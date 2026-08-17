package query

import (
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

// freeTCPPort 监听一个 TCP 端口并立即关闭，返回刚释放的端口号（大概率空闲）。
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// TestProbe_ClosedPort 端口无人监听 → 应判 Dead（connection refused）。
func TestProbe_ClosedPort(t *testing.T) {
	port := freeTCPPort(t)
	res, err := Probe("127.0.0.1", port, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != Dead {
		t.Fatalf("closed port: want Dead, got %v", res)
	}
}

// TestProbe_OpenPort 端口在监听但不处理连接（模拟引擎休眠：进程活着、
// 端口绑定、应用静默）→ 应判 Alive（握手由内核完成）。
func TestProbe_OpenPort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close() // 只监听不 Accept，模拟休眠中的“沉默”服务器
	port := l.Addr().(*net.TCPAddr).Port
	res, err := Probe("127.0.0.1", port, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != Alive {
		t.Fatalf("listening port: want Alive, got %v", res)
	}
}

// TestProbe_InvalidAddr 地址非法 → 应判 Error（不得误判为 Dead）。
func TestProbe_InvalidAddr(t *testing.T) {
	res, _ := Probe("999.999.999.999", 27015, 300*time.Millisecond)
	if res != Error {
		t.Fatalf("invalid addr: want Error, got %v", res)
	}
}

// TestProbe_InvalidPort 端口越界 → Error。
func TestProbe_InvalidPort(t *testing.T) {
	res, _ := Probe("127.0.0.1", 70000, 100*time.Millisecond)
	if res != Error {
		t.Fatalf("invalid port: want Error, got %v", res)
	}
}

// TestProbe_Smoke 确保结果枚举值稳定（依赖顺序的消费方不会被意外破坏）。
func TestProbe_Smoke(t *testing.T) {
	if Dead != 1 || Error != 2 {
		t.Fatalf("Result 枚举顺序被破坏: Alive=%d Dead=%d Error=%d", Alive, Dead, Error)
	}
}

// errnoErr 构造一个带 syscall.Errno 的 Dial 风格错误链，用于确定性测试分类逻辑。
func errnoErr(n syscall.Errno) error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: &os.SyscallError{Syscall: "connect", Err: n}}
}

// TestProbe_isConnRefused 覆盖跨平台底层错误码分类（含 Windows WSA 与 Linux EHOSTUNREACH）。
func TestProbe_isConnRefused(t *testing.T) {
	dead := []syscall.Errno{
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		10061, // WSAECONNREFUSED
		10054, // WSAECONNRESET
		113,   // Linux EHOSTUNREACH
		101,   // Linux ENETUNREACH
		65,    // BSD EHOSTUNREACH
		51,    // BSD ENETUNREACH
		11065, // WSAEHOSTUNREACH
		11063, // WSAENETUNREACH
		10050, // WSAENETDOWN
	}
	for _, n := range dead {
		if !isConnRefused(errnoErr(n)) {
			t.Fatalf("errno %d: want refused/dead, got alive/error", n)
		}
	}
	// 无关错误码不得误判。
	// 注意：111 是 Linux 的 ECONNREFUSED（errors.Is 会命中），绝不能放进负例清单，
	// 否则该测试在 Linux 上必然失败；这里只放两个平台都中性的错误码。
	for _, n := range []syscall.Errno{0, 7, 22, 11001} {
		if isConnRefused(errnoErr(n)) {
			t.Fatalf("errno %d: want not-refused, got refused", n)
		}
	}
}

// TestProbe_TimeoutIsError 超时（SYN 无响应）→ Error，不得误判为 Dead。
func TestProbe_TimeoutIsError(t *testing.T) {
	// 用不可到达且必超时的地址无法稳定构造，这里直接验证 isConnRefused 对超时错误不命中：
	ne := &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
	if isConnRefused(ne) {
		t.Fatalf("timeout must not be classified as refused/dead")
	}
}
