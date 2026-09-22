package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func promptLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("读取输入失败: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
func promptPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if pw, ok := readPasswordNoEcho(); ok {
		fmt.Fprintln(os.Stderr)
		return pw, nil
	}
	fmt.Fprintln(os.Stderr, "\n[警告] 当前终端无法关闭回显，密码将明文显示。建议改用 --password-stdin。")
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("读取密码失败: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}
