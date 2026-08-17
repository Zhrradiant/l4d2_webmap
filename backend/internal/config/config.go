// Package config 加载后端运行配置。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Config 后端配置。
type Config struct {
	Listen             string `json:"listen"`
	PushSecret         string `json:"push_secret"`
	DataDir            string `json:"data_dir"`
	WebDir             string `json:"web_dir"`
	SessionTTL         int    `json:"session_ttl"`          // 秒
	CodeTTL            int    `json:"code_ttl"`             // 秒
	RconTimeout        int    `json:"rcon_timeout"`         // 秒
	CleanupIntervalSec int    `json:"cleanup_interval_sec"` // 会话清理间隔（秒），默认 600
	OfflineAfterSec    int    `json:"offline_after_sec"`    // 推送超时（秒），未开启探测时超过则标记离线（兜底）；默认 180
	ProbeIntervalSec   int    `json:"probe_interval_sec"`   // 存活探测间隔（秒），0=关闭（回退推送超时判定）；默认 180
	ProbeTimeoutMs     int    `json:"probe_timeout_ms"`     // 单次探测超时（毫秒），默认 2000
	ProbeFailThreshold int    `json:"probe_fail_threshold"` // 连续探测失败多少次才判定离线（防偶发网络抖动），默认 2
	OfflineCleanupMin  int    `json:"offline_cleanup_min"`  // 连续离线多少分钟后自动清空该服务器文件与记录（0=关闭），默认 30
	OnlineMapURL       string `json:"online_map_url"`       // 远程在线地图 CSV URL，空表示仅使用本地数据
	OnlineMapRefreshH  int    `json:"online_map_refresh_h"` // 每日刷新时刻（0-23），默认 6
	BgMode             string `json:"bg_mode"`              // 背景模式: "default" / "custom" / "none"
	BgURL              string `json:"bg_url"`               // 自定义背景图链接（bg_mode=custom 时生效）
}

// Load 从 path 读取配置文件。
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败 %q: %w", path, err)
	}

	c := defaultConfig()
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("解析配置失败: %w", err)
	}

	c.applyDefaults()
	return c, nil
}

const DefaultOnlineMapURL = "https://zhrradiant-l4d2.cn-nb1.rains3.com/%E6%B1%82%E7%94%9F%E4%B9%8B%E8%B7%AF2-%E7%AC%AC%E4%B8%89%E6%96%B9%E5%9C%B0%E5%9B%BE%E5%88%97%E8%A1%A8-ZSM.csv"

// DefaultBgURL 默认背景图链接（bg_mode=default 时生效）。
const DefaultBgURL = "https://www.dmoe.cc/random.php"

func defaultConfig() *Config {
	return &Config{
		Listen:             ":11223",
		PushSecret:         "",
		DataDir:            "./data",
		WebDir:             "./web",
		SessionTTL:         7200,
		CodeTTL:            300,
		RconTimeout:        5,
		CleanupIntervalSec: 600,
		OfflineAfterSec:    180,
		ProbeIntervalSec:   180,
		ProbeTimeoutMs:     2000,
		ProbeFailThreshold: 2,
		OfflineCleanupMin:  30,
		OnlineMapURL:       DefaultOnlineMapURL,
		BgMode:             "default",
		BgURL:              DefaultBgURL,
	}
}

func (c *Config) applyDefaults() {
	if c.Listen == "" {
		c.Listen = ":11223"
	} else {
		c.Listen = normalizeListenAddr(c.Listen)
	}
	if c.DataDir == "" {
		c.DataDir = "./data"
	}
	if c.WebDir == "" {
		c.WebDir = "./web"
	}
	if c.SessionTTL <= 0 {
		c.SessionTTL = 7200
	}
	if c.CodeTTL <= 0 {
		c.CodeTTL = 300
	}
	if c.RconTimeout <= 0 {
		c.RconTimeout = 5
	}
	if c.CleanupIntervalSec <= 0 {
		c.CleanupIntervalSec = 600
	}
	if c.OfflineAfterSec <= 0 {
		c.OfflineAfterSec = 180
	}
	if c.ProbeIntervalSec < 0 {
		c.ProbeIntervalSec = 0 // 非法负值按"关闭探测"处理
	}
	if c.ProbeTimeoutMs <= 0 {
		c.ProbeTimeoutMs = 2000
	}
	if c.ProbeFailThreshold <= 0 {
		c.ProbeFailThreshold = 2
	}
	if c.OfflineCleanupMin < 0 {
		c.OfflineCleanupMin = 30
	}
	if c.OnlineMapRefreshH <= 0 || c.OnlineMapRefreshH > 23 {
		c.OnlineMapRefreshH = 6
	}
	if c.BgMode == "" {
		c.BgMode = "default"
	}
	if c.BgURL == "" && c.BgMode == "default" {
		c.BgURL = DefaultBgURL
	}
}

// SessionDuration 会话有效期。
func (c *Config) SessionDuration() time.Duration {
	return time.Duration(c.SessionTTL) * time.Second
}

// CodeDuration 验证码有效期。
func (c *Config) CodeDuration() time.Duration {
	return time.Duration(c.CodeTTL) * time.Second
}

// RconDuration RCON 超时。
func (c *Config) RconDuration() time.Duration {
	return time.Duration(c.RconTimeout) * time.Second
}

// CleanupDuration 会话清理间隔。
func (c *Config) CleanupDuration() time.Duration {
	return time.Duration(c.CleanupIntervalSec) * time.Second
}

// OfflineAfterDuration 推送超时兜底阈值（未开启探测时超过视为离线）。
func (c *Config) OfflineAfterDuration() time.Duration {
	return time.Duration(c.OfflineAfterSec) * time.Second
}

// ProbeInterval 存活探测间隔（0 表示关闭探测）。
func (c *Config) ProbeInterval() time.Duration {
	return time.Duration(c.ProbeIntervalSec) * time.Second
}

// ProbeTimeout 单次探测超时。
func (c *Config) ProbeTimeout() time.Duration {
	return time.Duration(c.ProbeTimeoutMs) * time.Millisecond
}

// OfflineCleanupDuration 连续离线自动清理阈值（0 表示关闭）。
func (c *Config) OfflineCleanupDuration() time.Duration {
	return time.Duration(c.OfflineCleanupMin) * time.Minute
}

// ServersDir 服务器 JSON 存储目录。
func (c *Config) ServersDir() string {
	return filepath.Join(c.DataDir, "servers")
}

// OnlineMapCachePath 在线地图本地缓存文件路径。
func (c *Config) OnlineMapCachePath() string {
	return filepath.Join(c.DataDir, "online_maps.json")
}

// RconPath rcon.json 路径。
func (c *Config) RconPath() string {
	return filepath.Join(c.DataDir, "rcon.json")
}

// PermPath 站点权限层配置路径：data/permissions.json（与 config.json / rcon.json 同目录）。
func (c *Config) PermPath() string {
	return filepath.Join(c.DataDir, "permissions.json")
}

// MixmapPresetsDir mixmap 网页预设根目录：data/mixmap_presets/<server_key>/
func (c *Config) MixmapPresetsDir() string {
	return filepath.Join(c.DataDir, "mixmap_presets")
}

// EnsureDirs 创建所需目录。
func (c *Config) EnsureDirs() error {
	if err := os.MkdirAll(c.ServersDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(c.MixmapPresetsDir(), 0o755); err != nil {
		return err
	}
	return nil
}

// normalizeListenAddr 规范化监听地址：纯端口号（如 "8080"）自动补冒号前缀 ":8080"。
func normalizeListenAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return addr
	}
	if !strings.Contains(addr, ":") {
		if _, err := strconv.Atoi(addr); err == nil {
			return ":" + addr
		}
	}
	return addr
}
