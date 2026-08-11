//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// enableVirtualTerminal 为 Windows 控制台做两件事：
//  1. 将输入/输出代码页切换到 UTF-8(65001)，保证中文与框线字符正常显示；
//  2. 开启 ENABLE_VIRTUAL_TERMINAL_PROCESSING，使 ANSI 颜色/清屏转义序列生效。
//
// 全部通过标准库 syscall 调用 kernel32 完成，不引入任何第三方依赖。
// 返回 true 表示 ANSI 颜色可安全使用（例如已成功开启 VT，或本就已开启）。
func enableVirtualTerminal() bool {
	const enableVirtualTerminalProcessing = 0x0004
	const cpUTF8 = 65001

	kernel32 := syscall.NewLazyDLL("kernel32.dll")

	// 代码页切到 UTF-8（失败不致命，仅可能导致中文乱码）
	if p := kernel32.NewProc("SetConsoleOutputCP"); p.Find() == nil {
		_, _, _ = p.Call(uintptr(cpUTF8))
	}
	if p := kernel32.NewProc("SetConsoleCP"); p.Find() == nil {
		_, _, _ = p.Call(uintptr(cpUTF8))
	}

	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")

	handle := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	r, _, _ := getConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return false // 输出不是控制台（可能被重定向到文件/管道）
	}
	if mode&enableVirtualTerminalProcessing != 0 {
		return true // 已经开启
	}
	r, _, _ = setConsoleMode.Call(uintptr(handle), uintptr(mode|enableVirtualTerminalProcessing))
	return r != 0
}
