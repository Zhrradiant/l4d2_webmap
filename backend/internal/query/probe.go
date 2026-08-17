// Package query 提供游戏服务器的轻量网络存活探测。
//
// 判定原则：不依赖游戏插件/SourceMod——引擎休眠（sv_hibernate_when_empty）
// 期间插件定时器全部停摆，但进程与端口始终绑定。Source 引擎的 RCON
// 监听 TCP 端口即游戏端口（后端 RCON 功能同样直连该端口），因此
// "TCP 端口可达" 是在休眠下依然可靠的存活信号，且跨平台（Windows/Linux）
// 都能区分"端口无监听(connection refused)"与"端口开放"。
package query

import (
	"errors"
	"net"
	"os"
	"strconv"
	"syscall"
	"time"
)

// Result 探测结果分类。
type Result int

const (
	// Alive 端口可达：TCP 连接建立成功，进程在监听（含引擎休眠中的服务器，
	// 握手由内核完成）。只说明端口在监听，不代表插件存活。
	Alive Result = iota
	// Dead 端口不可达（connection refused / RST / no route to host）：
	// 无进程监听，或目标 IP 已不存在（如容器重建后旧 IP 释放）。
	Dead
	// Error 探测自身出错（地址非法、DNS 失败、SYN 超时被防火墙丢弃等），
	// 不代表服务器状态，调用方不应据此改状态。
	Error
)

// 连接被拒相关底层错误码（数值比对，跨平台无编译风险）：
//   - Linux: EHOSTUNREACH=113 / ENETUNREACH=101（目标 IP 不存在时 ARP 失败返回,
//     典型场景: Docker 容器已重建、旧 IP 已释放——这是僵尸记录判定离线的关键信号）
//   - BSD/macOS: EHOSTUNREACH=65 / ENETUNREACH=51
//   - Windows(WSA): WSAECONNREFUSED=10061 / WSAECONNRESET=10054 /
//     WSAEHOSTUNREACH=11065 / WSAENETUNREACH=11063 / WSAENETDOWN=10050
const (
	wsaECONNREFUSED   = 10061
	wsaECONNRESET     = 10054
	errnoEHOSTUNREACH = 113
	errnoENETUNREACH  = 101
	bsdEHOSTUNREACH   = 65
	bsdENETUNREACH    = 51
	wsaEHOSTUNREACH   = 11065
	wsaENETUNREACH    = 11063
	wsaENETDOWN       = 10050
)

// isConnRefused 跨平台判定"连接被拒/端口无监听/主机不可达"。
//   - errors.Is(ECONNREFUSED/ECONNRESET) 在多数平台直接命中；
//   - 底层裸 Errno 数值比对兜底（Windows 的 WSA 码、Linux 的 EHOSTUNREACH 等）。
func isConnRefused(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var se *os.SyscallError
	if errors.As(err, &se) {
		if ee, ok := se.Err.(syscall.Errno); ok {
			switch uintptr(ee) {
			case wsaECONNREFUSED, wsaECONNRESET,
				errnoEHOSTUNREACH, errnoENETUNREACH,
				bsdEHOSTUNREACH, bsdENETUNREACH,
				wsaEHOSTUNREACH, wsaENETUNREACH, wsaENETDOWN:
				return true
			}
		}
	}
	return false
}

// Probe 探测 host:port 的存活状态。
//
// 判定逻辑：
//   - TCP 连接建立成功 → Alive
//   - connection refused / no route to host（无进程监听；目标 IP 已不存在，如容器重建）→ Dead
//   - 连接超时（SYN 无响应，可能网络不通/防火墙丢弃）→ Error，不判定
//   - 其他错误（DNS 等）→ Error
func Probe(host string, port int, timeout time.Duration) (Result, error) {
	if port <= 0 || port > 65535 {
		return Error, errors.New("invalid port")
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			// SYN 无响应：可能是防火墙丢弃或网络不通，不据此判定离线
			return Error, err
		}
		if isConnRefused(err) {
			return Dead, nil
		}
		return Error, err
	}
	_ = conn.Close()
	return Alive, nil
}
