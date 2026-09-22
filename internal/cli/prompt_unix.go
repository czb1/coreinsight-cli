//go:build !windows

package cli

import (
	"bufio"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	stdinStat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	nullFi, err := os.Stat(os.DevNull)
	if err != nil {
		return true
	}
	nullStat, ok := nullFi.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return stdinStat.Rdev != nullStat.Rdev
}
func readPasswordNoEcho() (string, bool) {
	if _, err := os.Stat("/dev/tty"); err != nil {
		return "", false
	}
	if err := sttyEcho(false); err != nil {
		return "", false
	}
	defer sttyEcho(true)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", false
	}
	return strings.TrimRight(line, "\r\n"), true
}
func sttyEcho(on bool) error {
	arg := "-echo"
	if on {
		arg = "echo"
	}
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
