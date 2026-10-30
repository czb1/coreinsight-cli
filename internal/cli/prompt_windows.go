//go:build windows

package cli

import (
	"bufio"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

const enableEchoInput = 0x0004

func stdinIsTerminal() bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(syscall.Handle(os.Stdin.Fd())), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}
func readPasswordNoEcho() (string, bool) {
	handle := syscall.Handle(os.Stdin.Fd())
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(uintptr(handle), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return "", false
	}
	if r, _, _ := procSetConsoleMode.Call(uintptr(handle), uintptr(mode&^enableEchoInput)); r == 0 {
		return "", false
	}
	defer procSetConsoleMode.Call(uintptr(handle), uintptr(mode))
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}
