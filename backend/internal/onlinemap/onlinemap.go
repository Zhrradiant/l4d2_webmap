// Package onlinemap 从远程 CSV 获取在线地图元数据，提供缓存与定时刷新。
//
// 数据来源：l4d2_server_status 项目导出的第三方地图列表 CSV。
// 字段顺序：中文名, 地图大厅展示名, 地图文件识别名, 换图代码, 下载链接, 浏览图
package onlinemap

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// OnlineMapEntry 单条在线地图元数据（对应 CSV 一行）。
type OnlineMapEntry struct {
	ChineseName string `json:"chinese_name"` // 中文名（仅参考）
	DisplayName string `json:"display_name"` // 地图大厅展示名
	Identifier  string `json:"identifier"`   // 地图文件识别名 —— 匹配键
	ImageURL    string `json:"image_url"`    // 浏览图 URL
}

// Store 在线地图元数据缓存。
// 线程安全，支持从远程拉取 + 本地文件兜底 + 定时刷新。
type Store struct {
	mu        sync.RWMutex
	entries   []OnlineMapEntry           // 有序列表
	byIdent   map[string]*OnlineMapEntry // identifier (小写) → entry
	url       string
	cachePath string
	updatedAt time.Time
}

// New 创建在线地图存储实例。
// url: 远程 CSV 地址，空字符串表示不启用。
// cachePath: 本地缓存 JSON 文件路径，如 "data/online_maps.json"。
func New(url, cachePath string) *Store {
	return &Store{
		url:       url,
		cachePath: cachePath,
		byIdent:   map[string]*OnlineMapEntry{},
	}
}

// FetchAndParse 下载远程 CSV、解析并更新内存缓存，同时写入本地文件。
// 返回拉取到的条目数。若拉取失败则尝试回退加载本地缓存。
func (s *Store) FetchAndParse() (int, error) {
	if s.url == "" {
		// 未配置 URL，尝试从本地缓存加载
		return s.loadFromLocal()
	}

	entries, err := s.downloadAndParse(s.url)
	if err != nil {
		log.Printf("[onlinemap] 远程拉取失败 (%v)，回退本地缓存", err)
		n, localErr := s.loadFromLocal()
		if localErr != nil {
			return 0, fmt.Errorf("远程拉取失败且本地缓存不可用: %w (远程: %v)", localErr, err)
		}
		return n, nil
	}

	s.mu.Lock()
	s.entries = entries
	s.rebuildIndex()
	s.updatedAt = time.Now()
	s.mu.Unlock()

	// 异步写入本地缓存
	go func() {
		if err := s.saveToLocal(entries); err != nil {
			log.Printf("[onlinemap] 写入本地缓存失败: %v", err)
		}
	}()

	log.Printf("[onlinemap] 成功拉取 %d 条在线地图数据", len(entries))
	return len(entries), nil
}

// Refresh 强制刷新（由定时器调用）。失败时静默保留旧数据。
func (s *Store) Refresh() {
	if s.url == "" {
		return
	}
	entries, err := s.downloadAndParse(s.url)
	if err != nil {
		log.Printf("[onlinemap] 定时刷新失败: %v", err)
		return
	}
	s.mu.Lock()
	s.entries = entries
	s.rebuildIndex()
	s.updatedAt = time.Now()
	s.mu.Unlock()

	go func() {
		if err := s.saveToLocal(entries); err != nil {
			log.Printf("[onlinemap] 写入本地缓存失败: %v", err)
		}
	}()

	log.Printf("[onlinemap] 定时刷新完成，共 %d 条", len(entries))
}

// StartScheduler 启动每日定时刷新（每天 refreshHour 点）。
// 调用前请确保已先调用 FetchAndParse 完成首次加载。
func (s *Store) StartScheduler(refreshHour int) {
	if s.url == "" {
		return
	}
	go func() {
		// 计算到下一个 refreshHour 的等待时间
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), refreshHour, 0, 0, 0, now.Location())
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}
		dur := next.Sub(now)
		log.Printf("[onlinemap] 下次刷新时间: %s（%s后）", next.Format("2006-01-02 15:04"), dur.Round(time.Second))

		// 等待到第一个刷新点
		time.Sleep(dur)

		// 之后每 24 小时执行一次
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()

		s.Refresh()

		for range t.C {
			s.Refresh()
		}
	}()
}

// GetAll 返回全部在线地图条目（只读副本）。
func (s *Store) GetAll() []OnlineMapEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]OnlineMapEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// UpdatedAt 返回最后成功更新时间。
func (s *Store) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

// FindByIdentifier 按地图文件识别名（大小写不敏感）查找。
// 说明：插件上报的 Mission 即游戏 mission txt 的 Name 字段，与 CSV「地图文件识别名」同源，故此即匹配主键。
func (s *Store) FindByIdentifier(ident string) *OnlineMapEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byIdent[strings.ToLower(ident)]
}

// Len 返回条目数。
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// --- 内部方法 ---

func (s *Store) downloadAndParse(url string) ([]OnlineMapEntry, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB 上限
	if err != nil {
		return nil, fmt.Errorf("读取响应体: %w", err)
	}

	return parseCSV(string(body))
}

// parseCSV 解析 CSV 文本为条目列表。
// CSV 格式: 中文名, 地图大厅展示名, 地图文件识别名, 换图代码, 下载链接, 浏览图
func parseCSV(raw string) ([]OnlineMapEntry, error) {
	r := csv.NewReader(strings.NewReader(raw))
	r.LazyQuotes = true
	r.FieldsPerRecord = -1 // 允许字段数不同

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV 解析失败: %w", err)
	}

	if len(records) < 3 {
		return nil, fmt.Errorf("CSV 数据过短，仅 %d 行", len(records))
	}

	// 跳过前两行标题（第1行: 列名, 第2行: 空）
	// 第3行开始的分组注释行以 ">>>>>" 开头，跳过
	entries := make([]OnlineMapEntry, 0, len(records)-2)
	for _, rec := range records[2:] {
		if len(rec) < 4 {
			continue
		}
		// 跳过注释/分组行
		col0 := strings.TrimSpace(rec[0])
		if strings.HasPrefix(col0, ">>>") || col0 == "" {
			continue
		}

		entry := OnlineMapEntry{
			ChineseName: col(rec, 0),
			DisplayName: col(rec, 1),
			Identifier:  col(rec, 2),
			ImageURL:    col(rec, 5),
		}

		entries = append(entries, entry)
	}

	return entries, nil
}

// col 安全返回 rec 的第 i 列并去除首尾空白；索引越界时返回空串。
// 用于容忍 CSV 行尾可选列（下载链接、浏览图）缺失，避免越界 panic。
func col(rec []string, i int) string {
	if i >= 0 && i < len(rec) {
		return strings.TrimSpace(rec[i])
	}
	return ""
}

// rebuildIndex 重建查找索引（调用方需持有写锁）。
func (s *Store) rebuildIndex() {
	s.byIdent = make(map[string]*OnlineMapEntry, len(s.entries))
	for i := range s.entries {
		e := &s.entries[i]
		// identifier 为空的行无法作为匹配键，跳过（避免空串污染索引）。
		if e.Identifier == "" {
			continue
		}
		// identifier（=游戏 mission Name）战役级唯一；重名时先到先得，
		// 保留 CSV 中先出现的战役，与桌面端 zhrradiant-srvmap 的 buildMapLookupRecord 行为一致。
		k := strings.ToLower(e.Identifier)
		if _, ok := s.byIdent[k]; !ok {
			s.byIdent[k] = e
		}
	}
}

func (s *Store) loadFromLocal() (int, error) {
	raw, err := os.ReadFile(s.cachePath)
	if err != nil {
		return 0, fmt.Errorf("读取本地缓存 %s: %w", s.cachePath, err)
	}

	var entries []OnlineMapEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return 0, fmt.Errorf("解析本地缓存: %w", err)
	}

	s.mu.Lock()
	s.entries = entries
	s.rebuildIndex()
	s.mu.Unlock()

	log.Printf("[onlinemap] 从本地缓存加载 %d 条在线地图数据", len(entries))
	return len(entries), nil
}

func (s *Store) saveToLocal(entries []OnlineMapEntry) error {
	if s.cachePath == "" {
		return nil
	}
	dir := filepath.Dir(s.cachePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	// 原子写：先写临时文件再 rename，避免写入中途被读到半个文件。
	tmp := s.cachePath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.cachePath); err != nil {
		os.Remove(tmp) // 清理失败的临时文件
		return err
	}
	return nil
}
