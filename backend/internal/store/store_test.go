package store

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestStore 在临时目录下创建 Store，返回 Store 与 servers 目录路径。
func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	serversDir := filepath.Join(dir, "servers")
	return New(serversDir), serversDir
}

// writeServerFile 直接写一份服务器 JSON 到磁盘（绕过 API，用于构造旧数据/迁移场景）。
func writeServerFile(t *testing.T, serversDir, key string, online bool, offlineSince, updatedAt int64) {
	t.Helper()
	raw := fmt.Sprintf(
		`{"server_key":%q,"online":%t,"offline_since":%d,"updated_at":%d}`,
		key, online, offlineSince, updatedAt)
	path := filepath.Join(serversDir, key+".json")
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fileExists 判断服务器文件是否存在。
func fileExists(serversDir, key string) bool {
	_, err := os.Stat(filepath.Join(serversDir, key+".json"))
	return err == nil
}

// TestMarkOfflineRecordsOfflineSince 离线时记录离线起点，恢复在线时清零。
func TestMarkOfflineRecordsOfflineSince(t *testing.T) {
	st, _ := newTestStore(t)
	if err := st.UpsertServer(&ServerData{ServerKey: "s1", Online: false}); err != nil {
		t.Fatal(err)
	}
	sd, err := st.Get("s1")
	if err != nil {
		t.Fatal(err)
	}
	if !sd.Online || sd.OfflineSince != 0 {
		t.Fatalf("push 后应在线且 offline_since=0, got online=%v offline_since=%d", sd.Online, sd.OfflineSince)
	}

	st.MarkOffline("s1")
	sd, _ = st.Get("s1")
	if sd.Online || sd.OfflineSince <= 0 {
		t.Fatalf("MarkOffline 后应离线且记录离线起点, got online=%v offline_since=%d", sd.Online, sd.OfflineSince)
	}

	st.ProbeOnline("s1")
	sd, _ = st.Get("s1")
	if !sd.Online || sd.OfflineSince != 0 {
		t.Fatalf("恢复在线后应 online 且 offline_since=0, got online=%v offline_since=%d", sd.Online, sd.OfflineSince)
	}
}

// TestCleanupExpired 删除连续离线超阈值且落到缓存的服务器文件；在线/未超时/无离线起点的保留。
func TestCleanupExpired(t *testing.T) {
	st, serversDir := newTestStore(t)
	old := time.Now().Unix() - 60*60 // 1 小时前

	// 1) 连续离线 1 小时 → 应被清理（走直接从磁盘加载，验证落盘文件也已删除）
	writeServerFile(t, serversDir, "zombie", false, old, old)
	// 2) 连续离线仅 1 分钟 → 保留
	writeServerFile(t, serversDir, "fresh", false, time.Now().Unix()-60, time.Now().Unix()-60)
	// 3) 在线 → 保留
	writeServerFile(t, serversDir, "alive", true, 0, time.Now().Unix())
	// 4) 离线但无 offline_since（旧格式迁移：加载时应以 updated_at 兜底）→ 视为旧离线，应被清理
	writeServerFile(t, serversDir, "oldfmt", false, 0, old)

	// 重新加载（New 会 loadAll）
	st = New(serversDir)

	deleted := st.CleanupExpired(30 * time.Minute)
	if len(deleted) != 2 {
		t.Fatalf("期望清理 2 台（zombie + oldfmt），实际清理 %v", deleted)
	}

	// 文件层面
	if fileExists(serversDir, "zombie") {
		t.Fatal("zombie 文件应已被删除")
	}
	if fileExists(serversDir, "oldfmt") {
		t.Fatal("oldfmt 文件应已被删除")
	}
	for _, keep := range []string{"fresh", "alive"} {
		if !fileExists(serversDir, keep) {
			t.Fatalf("%s 文件不应被删除", keep)
		}
		if _, err := st.Get(keep); err != nil {
			t.Fatalf("%s 应仍在缓存中: %v", keep, err)
		}
	}
	// 缓存侧
	if _, err := st.Get("zombie"); err == nil {
		t.Fatal("zombie 应已从缓存移除")
	}
}

// TestCleanupExpiredDisabled threshold<=0 时不做任何清理。
func TestCleanupExpiredDisabled(t *testing.T) {
	st, serversDir := newTestStore(t)
	old := time.Now().Unix() - 60*60
	writeServerFile(t, serversDir, "zombie", false, old, old)
	st = New(serversDir)

	if got := st.CleanupExpired(0); len(got) != 0 {
		t.Fatalf("threshold<=0 不应清理，实际 %v", got)
	}
	if !fileExists(serversDir, "zombie") {
		t.Fatal("关闭清理时文件不应被删")
	}
}

// TestStartOfflineCleanup 验证清理循环实际生效（用毫秒级间隔触发）。
func TestStartOfflineCleanup(t *testing.T) {
	st, serversDir := newTestStore(t)
	old := time.Now().Unix() - 60*60
	writeServerFile(t, serversDir, "zombie", false, old, old)
	writeServerFile(t, serversDir, "alive", true, 0, time.Now().Unix())
	st = New(serversDir)

	st.StartOfflineCleanup(50*time.Millisecond, 30*time.Minute)

	// 轮询等待清理线程删掉 zombie（缓存 + 磁盘）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, zombieErr := st.Get("zombie")
		if zombieErr != nil && !fileExists(serversDir, "zombie") {
			if fileExists(serversDir, "alive") {
				return // zombie 已清、alive 保留 → 通过
			}
			t.Fatal("alive 不应被清理")
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("2 秒内 zombie 未被清理")
}
