// Package session 管理会话 token 与登录校验。
package session

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"webmap/internal/config"
	"webmap/internal/store"
)

// Session 一个已登录会话。
type Session struct {
	Token     string
	ServerKey string
	Player    string
	SteamID   string
	GameAdmin bool // 游戏服务器管理员标记（权限层模式 1 使用，来自插件推送）
	Expire    time.Time
}

// Manager 会话管理器。
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session // token -> session
	ttl      time.Duration
	done     chan struct{}
}

// New 创建会话管理器。
func New(cfg *config.Config) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		ttl:      cfg.SessionDuration(),
		done:     make(chan struct{}),
	}
}

// Login 校验验证码并创建会话。成功返回新会话。
func (m *Manager) Login(st *store.Store, serverKey string, code string) (*Session, error) {
	found, err := st.ConsumeCode(serverKey, code)
	if err != nil {
		return nil, err
	}

	token := newToken()
	sess := &Session{
		Token:     token,
		ServerKey: serverKey,
		Player:    found.Player,
		SteamID:   found.SteamID,
		GameAdmin: found.Admin,
		Expire:    time.Now().Add(m.ttl),
	}

	m.mu.Lock()
	m.sessions[token] = sess
	m.mu.Unlock()

	return sess, nil
}

// StartCleanup 启动后台清理 goroutine，定期清除过期会话。
func (m *Manager) StartCleanup(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.done:
				return
			case <-ticker.C:
				m.pruneExpired()
			}
		}
	}()
}

// Stop 停止后台清理 goroutine。
func (m *Manager) Stop() {
	close(m.done)
}

// pruneExpired 移除所有过期会话。
func (m *Manager) pruneExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for token, sess := range m.sessions {
		if now.After(sess.Expire) {
			delete(m.sessions, token)
		}
	}
}

// Validate 校验 token，返回会话。过期则删除。
func (m *Manager) Validate(token string) (*Session, bool) {
	m.mu.RLock()
	sess, ok := m.sessions[token]
	if !ok {
		m.mu.RUnlock()
		return nil, false
	}
	if !time.Now().After(sess.Expire) {
		m.mu.RUnlock()
		return sess, true
	}
	m.mu.RUnlock()

	// 过期路径：需要写锁删除
	m.mu.Lock()
	// 二次检查，防止在升级锁期间被其他 goroutine 刷新
	sess, ok = m.sessions[token]
	if ok && time.Now().After(sess.Expire) {
		delete(m.sessions, token)
	}
	m.mu.Unlock()
	return nil, false
}

// Logout 删除会话。
func (m *Manager) Logout(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

func newToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Repr 返回会话的可公开信息。
type Repr struct {
	Token     string `json:"token"`
	ServerKey string `json:"server_key"`
	Player    string `json:"player"`
	ExpiresAt int64  `json:"expires_at"`
}

// Repr 返回会话的可公开信息。
func (s *Session) Repr() Repr {
	return Repr{
		Token:     s.Token,
		ServerKey: s.ServerKey,
		Player:    s.Player,
		ExpiresAt: s.Expire.Unix(),
	}
}
