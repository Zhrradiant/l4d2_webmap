// mixmap 网页侧预设存储：data/mixmap_presets/<server_key>/<name>.json
// 与 Mixmap 插件 configs/mixmap_presets/*.cfg 相互独立。
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MixmapPreset 网页图池预设。
type MixmapPreset struct {
	Name         string   `json:"name"`
	Gamemode     string   `json:"gamemode"`
	Maps         []string `json:"maps"`
	CreatedAt    int64    `json:"created_at"`
	OwnerSteamID string   `json:"owner_steam_id"` // 创建者 SteamID（旧数据可能为空=无主）
}

// MixmapPresetStore 按 server_key 分目录的预设文件存储。
type MixmapPresetStore struct {
	root string
	mu   sync.Mutex
}

// 预设名允许中文/字母/数字/空格/下划线/短横；禁止路径分隔与控制字符。
var presetNameSafe = regexp.MustCompile(`^[\p{L}\p{N} _\-]{1,64}$`)

// MaxPresetMaps 单份预设（及手动图池）的地图数量上限。
const MaxPresetMaps = 50

// ErrPresetNotFound 预设不存在。
var ErrPresetNotFound = errors.New("preset not found")

// ErrPresetInvalidName 预设名非法。
var ErrPresetInvalidName = errors.New("invalid preset name")

// NewMixmapPresetStore 创建预设存储。
func NewMixmapPresetStore(root string) *MixmapPresetStore {
	_ = os.MkdirAll(root, 0o755)
	return &MixmapPresetStore{root: root}
}

func sanitizePresetName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) {
		return "", ErrPresetInvalidName
	}
	// 显式拒绝路径穿越与扩展名注入
	if strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", ErrPresetInvalidName
	}
	if !presetNameSafe.MatchString(name) {
		return "", ErrPresetInvalidName
	}
	return name, nil
}

func (s *MixmapPresetStore) serverDir(serverKey string) string {
	// server_key 来自插件推送，再做一层路径净化
	key := filepath.Base(strings.TrimSpace(serverKey))
	return filepath.Join(s.root, key)
}

func (s *MixmapPresetStore) presetPath(serverKey, name string) string {
	return filepath.Join(s.serverDir(serverKey), name+".json")
}

// ListFiltered 列出某服务器下预设：支持名称/SteamID 子串搜索、自己创建的排最前、分页。
// q 为空表示不过滤；selfSteamID 非空时自己的预设排在前面；page 从 1 开始。
// 返回当前页条目与过滤后的总数。
func (s *MixmapPresetStore) ListFiltered(serverKey, q, selfSteamID string, page, pageSize int) ([]MixmapPreset, int, error) {
	all, err := s.listAll(serverKey)
	if err != nil {
		return nil, 0, err
	}

	// 搜索：名称 / SteamID 子串（不区分大小写）
	query := strings.ToLower(strings.TrimSpace(q))
	if query != "" {
		filtered := all[:0]
		for _, p := range all {
			if strings.Contains(strings.ToLower(p.Name), query) ||
				strings.Contains(strings.ToLower(p.OwnerSteamID), query) {
				filtered = append(filtered, p)
			}
		}
		all = filtered
	}

	// 排序：自己的排最前（相对顺序保持），其余按名称
	self := strings.TrimSpace(selfSteamID)
	if self != "" {
		var mine, others []MixmapPreset
		for _, p := range all {
			if p.OwnerSteamID == self {
				mine = append(mine, p)
			} else {
				others = append(others, p)
			}
		}
		sort.Slice(mine, func(i, j int) bool { return mine[i].Name < mine[j].Name })
		sort.Slice(others, func(i, j int) bool { return others[i].Name < others[j].Name })
		all = append(mine, others...)
	} else {
		sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	}

	total := len(all)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	start := (page - 1) * pageSize
	if start >= total {
		return []MixmapPreset{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

// CountByOwner 统计某用户在某服务器下创建的预设数量。
func (s *MixmapPresetStore) CountByOwner(serverKey, ownerSteamID string) int {
	all, err := s.listAll(serverKey)
	if err != nil {
		return 0
	}
	owner := strings.TrimSpace(ownerSteamID)
	if owner == "" {
		return 0
	}
	n := 0
	for _, p := range all {
		if p.OwnerSteamID == owner {
			n++
		}
	}
	return n
}

// Get 按名称获取单个预设（用于删除前的权限校验）。
func (s *MixmapPresetStore) Get(serverKey, name string) (*MixmapPreset, error) {
	name, err := sanitizePresetName(name)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	raw, err := os.ReadFile(s.presetPath(serverKey, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrPresetNotFound
		}
		return nil, err
	}
	var p MixmapPreset
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Name == "" {
		p.Name = name
	}
	return &p, nil
}

// listAll 列出某服务器下全部预设（按名称排序）。
func (s *MixmapPresetStore) listAll(serverKey string) ([]MixmapPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.serverDir(serverKey)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []MixmapPreset{}, nil
		}
		return nil, err
	}

	out := make([]MixmapPreset, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		var p MixmapPreset
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		if p.Name == "" {
			p.Name = strings.TrimSuffix(name, ".json")
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Save 保存/覆盖预设。
func (s *MixmapPresetStore) Save(serverKey string, p MixmapPreset) (*MixmapPreset, error) {
	name, err := sanitizePresetName(p.Name)
	if err != nil {
		return nil, err
	}
	if len(p.Maps) == 0 {
		return nil, fmt.Errorf("maps 不能为空")
	}
	// 清洗地图名：去空白、丢空串
	maps := make([]string, 0, len(p.Maps))
	for _, m := range p.Maps {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		// 地图名不允许空格（RCON 序列以空格分隔）
		if strings.ContainsAny(m, " \t\r\n\"'") {
			return nil, fmt.Errorf("非法地图名: %s", m)
		}
		maps = append(maps, m)
	}
	if len(maps) == 0 {
		return nil, fmt.Errorf("maps 不能为空")
	}
	if len(maps) > MaxPresetMaps {
		return nil, fmt.Errorf("预设地图不能超过 %d 张", MaxPresetMaps)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.serverDir(serverKey)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	p.Name = name
	p.Maps = maps
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().Unix()
	}

	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	path := s.presetPath(serverKey, name)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return nil, err
	}
	return &p, nil
}

// Delete 删除预设。
func (s *MixmapPresetStore) Delete(serverKey, name string) error {
	name, err := sanitizePresetName(name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.presetPath(serverKey, name)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrPresetNotFound
		}
		return err
	}
	return nil
}
