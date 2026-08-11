// Package perm 站点权限层配置：data/permissions.json（与其他配置文件同目录，无配置界面，手搓修改）。
// 不存在时自动生成模板；修改后约 1 秒内热生效，无需重启。
package perm

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Config 权限层配置。
type Config struct {
	SuperAdminSteamIDs []string `json:"super_admin_steam_ids"` // 最高权限者 SteamID（Steam2 格式，与游戏内推送一致）
	PresetAccessMode   int      `json:"preset_access_mode"`    // 预设管理入口开放范围：0=仅 super_admin_steam_ids；1=额外允许游戏服务器管理员（需插件推送 admin 标记）
	MaxPresetsPerUser  int      `json:"max_presets_per_user"`  // 每用户可保存的预设数量上限
}

// defaultConfig 返回默认权限配置。
func defaultConfig() Config {
	return Config{
		SuperAdminSteamIDs: []string{},
		PresetAccessMode:   0,
		MaxPresetsPerUser:  5,
	}
}

// Store 权限配置存取：带 mtime 热加载缓存（checkGap 内复用内存副本）。
type Store struct {
	path string
	mu   sync.Mutex

	cfg      Config
	statTime time.Time
	checkGap time.Duration
}

// New 创建权限配置存取。文件不存在时生成默认模板；文件存在但解析失败时返回错误
// （调用方可降级为默认配置继续运行）。
// 路径由调用方提供（config.Config.PermPath → data/permissions.json）。
func New(path string) (*Store, error) {
	s := &Store{path: path, cfg: defaultConfig(), checkGap: time.Second}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if werr := writeTemplate(path); werr != nil {
			return s, fmt.Errorf("生成 %s 失败: %w", path, werr)
		}
		return s, nil
	}
	if err := s.reload(); err != nil {
		return s, err
	}
	return s, nil
}

// writeTemplate 生成带说明的默认模板。
func writeTemplate(path string) error {
	tpl := map[string]interface{}{
		"_说明": "站点权限层配置：手搓修改，改后约 1 秒内生效，无需重启。" +
			"super_admin_steam_ids 填 Steam2 格式（与游戏内推送一致），示例仅演示格式，请替换为自己的 SteamID；" +
			"preset_access_mode：0=仅上述 steamID 可管理预设，1=额外允许游戏服务器管理员（需插件推送 admin 标记）；" +
			"max_presets_per_user：每用户可保存的预设数量上限。",
		"super_admin_steam_ids": []string{"STEAM_1:0:1234567890", "STEAM_1:0:1234567891"},
		"preset_access_mode":    0,
		"max_presets_per_user":  5,
	}
	raw, err := json.MarshalIndent(tpl, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o644)
}

// reload 重新读取磁盘配置（不存在时回退默认值）。
func (s *Store) reload() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			s.cfg = defaultConfig()
			return nil
		}
		return err
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("解析 %s 失败: %w", s.path, err)
	}
	// 逐项回填默认值，避免手搓漏字段时零值生效
	d := defaultConfig()
	if c.MaxPresetsPerUser <= 0 {
		c.MaxPresetsPerUser = d.MaxPresetsPerUser
	}
	if c.PresetAccessMode != 1 {
		c.PresetAccessMode = 0
	}
	for i := range c.SuperAdminSteamIDs {
		c.SuperAdminSteamIDs[i] = strings.TrimSpace(c.SuperAdminSteamIDs[i])
	}
	s.cfg = c
	return nil
}

// Get 返回当前权限配置（带热加载缓存）。
func (s *Store) Get() Config {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if now.Sub(s.statTime) >= s.checkGap {
		// 仅当文件 mtime 变化时才真正重读，其余情况只更新时间戳
		if fi, err := os.Stat(s.path); err == nil && fi.ModTime().After(s.statTime) {
			_ = s.reload()
		}
		s.statTime = now
	}
	return s.cfg
}

// IsSuperAdmin 判断 steamID 是否为最高权限者。
func (s *Store) IsSuperAdmin(steamID string) bool {
	id := strings.TrimSpace(steamID)
	if id == "" {
		return false
	}
	for _, v := range s.Get().SuperAdminSteamIDs {
		if v != "" && v == id {
			return true
		}
	}
	return false
}

// CanManagePresets 判断用户是否有预设管理权限：
// 最高权限者始终允许；模式 1 时游戏服务器管理员（会话带 GameAdmin 标记）也允许。
func (s *Store) CanManagePresets(steamID string, gameAdmin bool) bool {
	if s.IsSuperAdmin(steamID) {
		return true
	}
	return s.Get().PresetAccessMode == 1 && gameAdmin
}
