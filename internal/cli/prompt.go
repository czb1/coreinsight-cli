package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func promptUsername() (string, error) {
	fmt.Fprint(os.Stderr, "域账号用户名: ")
	username, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取用户名失败，请使用 --username")
	}
	return strings.TrimSpace(username), nil
}

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
