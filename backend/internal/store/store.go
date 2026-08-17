// Package store 以 JSON 文件持久化每台服务器的数据（地图、验证码、状态）。
//
// 每台服务器一个文件: <data_dir>/servers/<server_key>.json
// 读写加文件锁防止并发冲突。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const maxSubscribersPerKey = 64

// --- 数据结构 ---

// ChapterMap 单张地图。
type ChapterMap struct {
	Mission           string `json:"mission"`
	MissionDisplayEn  string `json:"mission_display_en"`
	MissionDisplayChi string `json:"mission_display_chi"`
	ChapterMap        string `json:"chapter_map"`
	ChapterEn         string `json:"chapter_en"`
	ChapterChi        string `json:"chapter_chi"`
	Official          bool   `json:"official"`
	IsFirst           bool   `json:"is_first"`
}

// Code 待验证的验证码。
type Code struct {
	Code    string `json:"code"`
	Player  string `json:"player"`
	SteamID string `json:"steamid"`
	Expire  int64  `json:"expire"`
	Admin   bool   `json:"admin"` // 是否为游戏服务器管理员（插件可选推送，权限层模式 1 使用）
}

// ServerData 单台服务器的完整数据。
type ServerData struct {
	ServerKey       string       `json:"server_key"`
	Host            string       `json:"host"`
	Port            int          `json:"port"`
	Name            string       `json:"name"`
	Gamemode        string       `json:"gamemode"`
	CurrentMap      string       `json:"current_map"`
	Maps            []ChapterMap `json:"maps"`
	Codes           []Code       `json:"codes"`
	UpdatedAt       int64        `json:"updated_at"`
	Online          bool         `json:"online"`
	OfflineSince    int64        `json:"offline_since,omitempty"` // 进入离线状态的时刻（Unix 秒）；在线时为 0
	MixmapAvailable bool         `json:"mixmap_available"`        // 插件探测 Mixmap 是否在线（可选增强）
}

// ServerSummary 服务器公开摘要（不含 codes/host/port）。
type ServerSummary struct {
	ServerKey      string `json:"server_key"`
	Name           string `json:"name"`
	Online         bool   `json:"online"`
	CurrentMap     string `json:"current_map"`
	CurrentMapName string `json:"current_map_name"` // 当前地图所属战役的展示名（预览列表用）
	Gamemode       string `json:"gamemode"`
}

// ServerSearchResult 统合搜索：单台服务器中命中的地图集合。
type ServerSearchResult struct {
	ServerKey  string
	Name       string
	Gamemode   string
	Online     bool
	CurrentMap string
	Matches    []ChapterMap // 命中的章节
}

// ErrNotFound 服务器不存在。
var ErrNotFound = errors.New("server not found")

// --- 存储引擎 ---

// Store JSON 文件存储引擎。
type Store struct {
	serversDir string

	// 内存缓存: server_key -> *ServerData
	mu    sync.RWMutex
	cache map[string]*ServerData

	// 验证码全局索引: code -> {serverKey, *Code}
	codeIndex map[string]codeIndexEntry

	// SSE 订阅通道
	evtMu sync.RWMutex
	subs  map[string]map[chan Event]struct{}
}

type codeIndexEntry struct {
	ServerKey string
	Code      Code
}

// Event SSE 推送事件。
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
}

// New 创建存储引擎。
func New(serversDir string) *Store {
	// 确保目录存在（持久化时不再重复创建）
	_ = os.MkdirAll(serversDir, 0o755)
	s := &Store{
		serversDir: serversDir,
		cache:      make(map[string]*ServerData),
		codeIndex:  make(map[string]codeIndexEntry),
		subs:       make(map[string]map[chan Event]struct{}),
	}
	_ = s.loadAll()
	return s
}

// loadAll 启动时从磁盘加载所有服务器 JSON，并建立 code 索引。
func (s *Store) loadAll() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entries, err := os.ReadDir(s.serversDir)
	if err != nil {
		return nil // 目录不存在不算错
	}
	now := time.Now().Unix()
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		key := name[:len(name)-len(".json")]
		path := filepath.Join(s.serversDir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var sd ServerData
		if err := json.Unmarshal(raw, &sd); err != nil {
			continue
		}
		// 启动时清理过期验证码，并重建索引
		var codes []Code
		for _, c := range sd.Codes {
			if c.Expire > now {
				codes = append(codes, c)
				s.codeIndex[c.Code] = codeIndexEntry{ServerKey: key, Code: c}
			}
		}
		sd.Codes = codes
		// 迁移兼容：旧文件无 offline_since 字段，离线服务器以 updated_at 作为离线起点兜底
		if !sd.Online && sd.OfflineSince == 0 {
			sd.OfflineSince = sd.UpdatedAt
		}
		s.cache[key] = &sd
	}
	return nil
}

// List 返回所有服务器的公开摘要。
func (s *Store) List() []ServerSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ServerSummary, 0, len(s.cache))
	for _, sd := range s.cache {
		out = append(out, ServerSummary{
			ServerKey:  sd.ServerKey,
			Name:       sd.Name,
			Online:     sd.Online,
			CurrentMap: sd.CurrentMap,
			Gamemode:   sd.Gamemode,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ServerKey < out[j].ServerKey
	})
	return out
}

// SearchMaps 跨全部服务器搜索包含关键字的地图（统合搜索）。
// 内存遍历 + 单次 RLock，匹配 mission / 显示名 / 章节 map / 章节名；按 server_key 排序。
func (s *Store) SearchMaps(q string) []ServerSearchResult {
	q = strings.ToLower(strings.TrimSpace(q))
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]ServerSearchResult, 0, len(s.cache))
	for _, sd := range s.cache {
		var matches []ChapterMap
		for _, m := range sd.Maps {
			if mapMatchesQuery(m, q) {
				matches = append(matches, m)
			}
		}
		if len(matches) == 0 {
			continue
		}
		out = append(out, ServerSearchResult{
			ServerKey:  sd.ServerKey,
			Name:       sd.Name,
			Gamemode:   sd.Gamemode,
			Online:     sd.Online,
			CurrentMap: sd.CurrentMap,
			Matches:    matches,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ServerKey < out[j].ServerKey
	})
	return out
}

// mapMatchesQuery 单张地图是否命中关键字（大小写不敏感 contains）。
func mapMatchesQuery(m ChapterMap, q string) bool {
	if q == "" {
		return false
	}
	fields := []string{m.Mission, m.MissionDisplayEn, m.MissionDisplayChi, m.ChapterMap, m.ChapterEn, m.ChapterChi}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// Get 返回服务器完整数据。
func (s *Store) Get(key string) (*ServerData, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sd, ok := s.cache[key]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneServerData(sd), nil
}

// GetPublicState 返回不含 codes 的状态（供已登录玩家查看地图）。
func (s *Store) GetPublicState(key string) (*ServerData, error) {
	sd, err := s.Get(key)
	if err != nil {
		return nil, err
	}
	sd.Codes = nil // 不暴露验证码池
	sd.Host = ""   // 不暴露 host
	sd.Port = 0    // 不暴露 port
	return sd, nil
}

// UpsertServer 更新或创建服务器数据（来自 /api/push）。
func (s *Store) UpsertServer(in *ServerData) error {
	s.mu.Lock()

	sd, ok := s.cache[in.ServerKey]
	if !ok {
		sd = &ServerData{ServerKey: in.ServerKey}
		s.cache[in.ServerKey] = sd
	}

	sd.Host = in.Host
	sd.Port = in.Port
	sd.Name = in.Name
	sd.Gamemode = in.Gamemode
	sd.CurrentMap = in.CurrentMap
	sd.Maps = in.Maps
	sd.MixmapAvailable = in.MixmapAvailable
	sd.UpdatedAt = time.Now().Unix()
	sd.Online = true
	sd.OfflineSince = 0 // 推送即存活，重置离线计时

	// 在锁内取快照，供锁外 broadcast 使用
	currentMap := sd.CurrentMap
	mapCount := len(sd.Maps)

	if err := s.persistLocked(in.ServerKey); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()

	s.broadcast(in.ServerKey, Event{Type: "map_data", Data: map[string]interface{}{
		"current_map": currentMap,
		"map_count":   mapCount,
	}})
	return nil
}

// AppendCode 添加验证码（来自 /api/code）。
func (s *Store) AppendCode(key string, code Code) error {
	s.mu.Lock()
	sd, ok := s.cache[key]
	if !ok {
		s.mu.Unlock()
		return ErrNotFound
	}
	// 去重: 同一玩家旧码删除（同时清理索引）
	var codes []Code
	for _, c := range sd.Codes {
		if c.Player == code.Player {
			delete(s.codeIndex, c.Code)
			continue
		}
		codes = append(codes, c)
	}
	codes = append(codes, code)
	sd.Codes = codes
	s.codeIndex[code.Code] = codeIndexEntry{ServerKey: key, Code: code}

	if err := s.persistLocked(key); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return nil
}

// ConsumeCode 校验并消费验证码（一次性）。返回匹配的 Code。
func (s *Store) ConsumeCode(key string, code string) (*Code, error) {
	// 快速路径：通过 code 索引 O(1) 查找
	s.mu.RLock()
	entry, ok := s.codeIndex[code]
	s.mu.RUnlock()
	if !ok || entry.ServerKey != key {
		return nil, errors.New("验证码无效或已过期")
	}

	s.mu.Lock()
	// 二次校验（防止索引被并发修改）
	entry, ok = s.codeIndex[code]
	if !ok || entry.ServerKey != key {
		s.mu.Unlock()
		return nil, errors.New("验证码无效或已过期")
	}

	sd, ok := s.cache[key]
	if !ok {
		s.mu.Unlock()
		return nil, ErrNotFound
	}

	now := time.Now().Unix()
	var found *Code
	var remaining []Code
	for i := range sd.Codes {
		c := sd.Codes[i]
		if c.Expire <= now {
			// 过期码也从索引移除
			delete(s.codeIndex, c.Code)
			continue
		}
		if c.Code == code && found == nil {
			cCopy := c
			found = &cCopy
			delete(s.codeIndex, c.Code)
			continue
		}
		remaining = append(remaining, c)
	}

	if found == nil {
		s.mu.Unlock()
		return nil, errors.New("验证码无效或已过期")
	}

	sd.Codes = remaining
	_ = s.persistLocked(key)
	s.mu.Unlock()
	return found, nil
}

// MarkOffline 标记离线（记录离线起点，供连续离线自动清理使用）。
func (s *Store) MarkOffline(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sd, ok := s.cache[key]; ok && sd.Online {
		sd.Online = false
		sd.OfflineSince = time.Now().Unix()
		_ = s.persistLocked(key)
	}
}

// ProbeTarget 存活探测目标（含 host/port，供外部网络探测使用）。
type ProbeTarget struct {
	Key  string
	Host string
	Port int
}

// ProbeTargets 返回所有服务器的探测目标（含僵尸记录，由探测结果决定去留）。
func (s *Store) ProbeTargets() []ProbeTarget {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ProbeTarget, 0, len(s.cache))
	for _, sd := range s.cache {
		out = append(out, ProbeTarget{Key: sd.ServerKey, Host: sd.Host, Port: sd.Port})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// ProbeOnline 探测确认在线：仅在有变化（状态翻转）时更新时间并写盘，
// 避免每轮探测都产生磁盘写入。恢复在线时重置离线计时。
func (s *Store) ProbeOnline(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sd, ok := s.cache[key]
	if !ok {
		return
	}
	if !sd.Online {
		sd.Online = true
		sd.OfflineSince = 0
		sd.UpdatedAt = time.Now().Unix()
		_ = s.persistLocked(key)
	}
}

// StartSweep 启动后台推送超时兜底扫描（仅在未开启端口探测时使用）：
// 周期检查所有服务器，超过 threshold 未收到推送的在线服务器自动标记离线
// （复用 MarkOffline 语义）。扫描每 30 秒执行一次。
func (s *Store) StartSweep(threshold time.Duration) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.sweepStale(threshold)
		}
	}()
}

func (s *Store) sweepStale(threshold time.Duration) {
	now := time.Now().Unix()
	limit := int64(threshold.Seconds())
	if limit <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, sd := range s.cache {
		if !sd.Online {
			continue
		}
		if now-sd.UpdatedAt > limit {
			sd.Online = false
			sd.OfflineSince = now
			_ = s.persistLocked(key)
		}
	}
}

// CleanupExpired 清理连续离线超过 threshold 的服务器：删除 JSON 文件、
// 内存记录与验证码索引。返回被清理的 key 列表。threshold <= 0 时不做任何事。
func (s *Store) CleanupExpired(threshold time.Duration) []string {
	limit := int64(threshold.Seconds())
	if limit <= 0 {
		return nil
	}
	now := time.Now().Unix()

	s.mu.Lock()
	defer s.mu.Unlock()

	var deleted []string
	for key, sd := range s.cache {
		// 在线或从未离线（无离线起点）的不动
		if sd.Online || sd.OfflineSince <= 0 {
			continue
		}
		if now-sd.OfflineSince < limit {
			continue
		}
		// 清验证码索引
		for _, c := range sd.Codes {
			delete(s.codeIndex, c.Code)
		}
		delete(s.cache, key)
		path := filepath.Join(s.serversDir, key+".json")
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("[store] 清理离线服务器文件失败 %s: %v", path, err)
		}
		deleted = append(deleted, key)
	}
	sort.Strings(deleted)
	return deleted
}

// StartOfflineCleanup 启动连续离线自动清理：每 interval 扫描一次，
// 超过 threshold 连续离线的服务器清空其文件与记录（threshold <= 0 表示关闭）。
// interval 由调用方传入（生产建议 60s，便于测试缩短）。
func (s *Store) StartOfflineCleanup(interval, threshold time.Duration) {
	if threshold <= 0 || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if keys := s.CleanupExpired(threshold); len(keys) > 0 {
				log.Printf("[store] 已清空连续离线超时的服务器记录: %v", keys)
			}
		}
	}()
}

// --- 持久化 ---

func (s *Store) persistLocked(key string) error {
	sd, ok := s.cache[key]
	if !ok {
		return ErrNotFound
	}
	path := filepath.Join(s.serversDir, key+".json")
	// 目录已在 New() 中创建，此处不再重复 MkdirAll
	raw, err := json.MarshalIndent(sd, "", "  ")
	if err != nil {
		return err
	}
	// 原子写入: 先写临时文件再 rename
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cloneServerData(sd *ServerData) *ServerData {
	cp := *sd
	if sd.Maps != nil {
		cp.Maps = append([]ChapterMap(nil), sd.Maps...)
	}
	if sd.Codes != nil {
		cp.Codes = append([]Code(nil), sd.Codes...)
	}
	return &cp
}

// --- SSE 订阅 ---

// Subscribe 订阅服务器事件。返回接收通道和取消函数。
// 每个 key 最多 maxSubscribersPerKey 个订阅者。
func (s *Store) Subscribe(key string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	s.evtMu.Lock()
	if s.subs[key] == nil {
		s.subs[key] = make(map[chan Event]struct{})
	}
	if len(s.subs[key]) >= maxSubscribersPerKey {
		s.evtMu.Unlock()
		return nil, nil
	}
	s.subs[key][ch] = struct{}{}
	s.evtMu.Unlock()

	cancel := func() {
		s.evtMu.Lock()
		delete(s.subs[key], ch)
		if len(s.subs[key]) == 0 {
			delete(s.subs, key)
		}
		s.evtMu.Unlock()
		close(ch)
	}
	return ch, cancel
}

func (s *Store) broadcast(key string, evt Event) {
	s.evtMu.RLock()
	defer s.evtMu.RUnlock()
	for ch := range s.subs[key] {
		select {
		case ch <- evt:
		default:
			log.Printf("[store] broadcast dropped event %q for key %q: subscriber too slow", evt.Type, key)
		}
	}
}

// BroadcastVoteResult 广播投票结果。
func (s *Store) BroadcastVoteResult(key string, result string, mission string, mapName string) {
	s.broadcast(key, Event{Type: "vote_result", Data: map[string]interface{}{
		"result":  result,
		"mission": mission,
		"map":     mapName,
	}})
}

// EnrichedMap 本地地图与在线元数据合并后的条目。
type EnrichedMap struct {
	// 本地数据
	Mission           string `json:"mission"`
	MissionDisplayEn  string `json:"mission_display_en"`
	MissionDisplayChi string `json:"mission_display_chi"`
	ChapterMap        string `json:"chapter_map"`
	ChapterEn         string `json:"chapter_en"`
	ChapterChi        string `json:"chapter_chi"`
	Official          bool   `json:"official"`
	IsFirst           bool   `json:"is_first"`
	// 在线匹配数据（nil 表示未匹配）
	Online     *OnlineMapRef `json:"online,omitempty"`
	MatchLevel string        `json:"match_level"` // "exact" | "none"
}

// OnlineMapRef 在线地图引用（精简版，避免循环引用）。
type OnlineMapRef struct {
	ChineseName string `json:"chinese_name"`
	DisplayName string `json:"display_name"`
	Identifier  string `json:"identifier"`
	ImageURL    string `json:"image_url"`
}

// FormatPort 格式化端口为字符串（用于 per_port 密码查找）。
func FormatPort(port int) string {
	return fmt.Sprintf("%d", port)
}

// LookupCode 通过验证码 O(1) 查找所属 serverKey 和码信息。不消费验证码。
func (s *Store) LookupCode(code string) (string, *Code, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.codeIndex[code]
	if !ok {
		return "", nil, false
	}
	cp := entry.Code
	return entry.ServerKey, &cp, true
}
