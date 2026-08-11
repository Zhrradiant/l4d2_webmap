//go:build !windows

package main

// enableVirtualTerminal 在非 Windows 平台上，多数终端默认已支持 ANSI 颜色与 UTF-8，
// 直接返回 true。（若输出被重定向到文件，颜色转义序列可能被写入，但不影响功能。）
func enableVirtualTerminal() bool {
	return true
}
