package cli

import (
	"fmt"
	"os"
)

func promptPassword() (string, error) {
	if !stdinIsTerminal() {
		return "", fmt.Errorf("当前输入不是交互终端，请使用 --password-stdin")
	}
	fmt.Fprint(os.Stderr, "密码（输入不回显）: ")
	password, ok := readPasswordNoEcho()
	fmt.Fprintln(os.Stderr)
	if !ok {
		return "", fmt.Errorf("无法安全读取密码，请改用 --password-stdin")
	}
	return password, nil
}
