// Package rconcfg 管理 rcon.json 配置（RCON 密码）。
//
// 支持两种模式:
//   - unified:   所有端口共用一个密码
//   - per_port:  按端口查密码
package rconcfg

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Config rcon.json 配置。
type Config struct {
	Mode             string            `json:"mode"`              // "unified" | "per_port"
	UnifiedPassword  string            `json:"unified_password"`  // mode=unified 时使用
	PerPort          map[string]string `json:"per_port"`          // mode=per_port 时使用
	HostOverride     map[string]string `json:"host_override"`     // 可选: 按 server_key 覆盖 host
}

// Manager RCON 配置管理器。
type Manager struct {
	mu   sync.RWMutex
	cfg  *Config
	path string
}

// New 创建管理器并加载文件。
func New(path string) (*Manager, error) {
	m := &Manager{path: path, cfg: &Config{Mode: "unified"}}
	if err := m.Load(); err != nil {
		return nil, err
	}
	return m, nil
}

// Load 重新从磁盘加载。
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	raw, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在时使用空默认值
		}
		return fmt.Errorf("读取 rcon.json 失败: %w", err)
	}

	c := &Config{Mode: "unified"}
	if err := json.Unmarshal(raw, c); err != nil {
		return fmt.Errorf("解析 rcon.json 失败: %w", err)
	}
	if c.Mode == "" {
		c.Mode = "unified"
	}
	m.cfg = c
	return nil
}

// GetPassword 根据 server_key 和 port 获取 RCON 密码。
func (m *Manager) GetPassword(serverKey string, port int) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	switch m.cfg.Mode {
	case "per_port":
		portStr := fmt.Sprintf("%d", port)
		if pwd, ok := m.cfg.PerPort[portStr]; ok {
			return pwd, nil
		}
		return "", fmt.Errorf("per_port 模式: 端口 %d 未配置密码", port)
	case "unified":
		if m.cfg.UnifiedPassword != "" {
			return m.cfg.UnifiedPassword, nil
		}
		return "", fmt.Errorf("unified 模式: 未配置 unified_password")
	default:
		return "", fmt.Errorf("未知 rcon 模式: %s", m.cfg.Mode)
	}
}

// GetHost 根据 server_key 获取 host（可被 host_override 覆盖）。
func (m *Manager) GetHost(serverKey string, defaultHost string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if h, ok := m.cfg.HostOverride[serverKey]; ok && h != "" {
		return h
	}
	return defaultHost
}

// Snapshot 返回配置的只读副本（不含密码明文给前端，但供 init/CLI 用）。
func (m *Manager) Snapshot() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := *m.cfg
	return cp
}

// Save 写入磁盘（供 init 命令用）。
func (m *Manager) Save(c *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.path, raw, 0o600); err != nil {
		return err
	}
	m.cfg = c
	return nil
}
