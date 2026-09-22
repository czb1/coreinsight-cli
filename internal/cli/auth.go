package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const loginPath = "/api/auth/login"

var errUnauthenticated = errors.New("unauthenticated")

type loginOpts struct {
	username      string
	password      string
	passwordStdin bool
	body          string
	bodyFile      string
}

func runAuth(rt *Runtime, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		authHelp()
		return nil
	}
	switch args[0] {
	case "login":
		return runAuthLogin(rt, args[1:])
	case "logout":
		return runAuthLogout(rt)
	case "status":
		return runAuthStatus(rt, args[1:])
	default:
		return fmt.Errorf("未知 auth 子命令: %s", args[0])
	}
}

func runAuthLogin(rt *Runtime, args []string) error {
	if hasHelp(args) {
		authLoginHelp()
		return nil
	}
	var o loginOpts
	fs := newFS("auth login")
	fs.StringVar(&o.username, "username", "", "域账号用户名")
	fs.StringVar(&o.username, "u", "", "域账号用户名")
	fs.StringVar(&o.password, "password", "", "密码")
	fs.StringVar(&o.password, "p", "", "密码")
	fs.BoolVar(&o.passwordStdin, "password-stdin", false, "从标准输入读取密码")
	fs.StringVar(&o.body, "body", "", "原始 JSON 请求体")
	fs.StringVar(&o.bodyFile, "body-file", "", "从文件读取 JSON 请求体")
	if err := fs.Parse(args); err != nil {
		return err
	}
	body, username, err := buildLoginBody(&o)
	if err != nil {
		return err
	}
	id := requestID()
	resp, payload, err := doLoginRequest(rt, body)
	if err != nil {
		failure(id, codeInternalError, "Request Failed", err.Error())
		return errHandled
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		failure(id, codeBackendError, resp.Status, payload)
		return errHandled
	}
	if code, msg, ok := businessCode(payload); ok && code != 0 {
		failure(id, codeBackendError, "Login Failed", map[string]interface{}{"code": code, "msg": msg})
		return errHandled
	}
	cookie, expires := extractCookieWithExpiry(resp)
	if cookie == "" {
		failure(id, codeInternalError, "No Session Cookie", "后端返回登录成功，但响应中没有 Set-Cookie，无法建立本地会话")
		return errHandled
	}
	sess := &Session{Cookie: cookie, Username: username, Server: rt.ChatServer, SavedAt: time.Now().Format(time.RFC3339)}
	if !expires.IsZero() {
		sess.ExpiresAt = expires.Format(time.RFC3339)
	}
	if err := saveSession(sess); err != nil {
		failure(id, codeInternalError, "Session Save Failed", err.Error())
		return errHandled
	}
	rt.Session = sess
	result := map[string]interface{}{"authenticated": true, "username": username, "server": rt.ChatServer, "cookie": maskedCookie(cookie), "session_file": sessionFile(), "saved_at": sess.SavedAt, "expires_at": nullable(sess.ExpiresAt)}
	if _, msg, ok := businessCode(payload); ok && msg != "" {
		result["msg"] = msg
	}
	success(id, result)
	return nil
}

func buildLoginBody(o *loginOpts) ([]byte, string, error) {
	raw := ""
	if o.bodyFile != "" {
		data, err := os.ReadFile(o.bodyFile)
		if err != nil {
			return nil, "", fmt.Errorf("读取 --body-file 失败: %w", err)
		}
		raw = string(data)
	} else if o.body != "" {
		raw = o.body
	}
	if raw != "" {
		if !json.Valid([]byte(raw)) {
			return nil, "", fmt.Errorf("请求体不是合法 JSON")
		}
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &m)
		name, _ := m["userName"].(string)
		return []byte(raw), name, nil
	}
	username := firstNonEmpty(o.username, os.Getenv("COREINSIGHT_AUTH_USERNAME"))
	if username == "" {
		if !stdinIsTerminal() {
			return nil, "", fmt.Errorf("缺少用户名：请使用 --username，或设置环境变量 COREINSIGHT_AUTH_USERNAME")
		}
		v, err := promptLine("域账号用户名: ")
		if err != nil {
			return nil, "", err
		}
		username = strings.TrimSpace(v)
	}
	if username == "" {
		return nil, "", fmt.Errorf("用户名不能为空")
	}
	password, err := resolvePassword(o)
	if err != nil {
		return nil, "", err
	}
	if password == "" {
		return nil, "", fmt.Errorf("密码不能为空")
	}
	body, err := json.Marshal(map[string]string{"userName": username, "passwd": password})
	if err != nil {
		return nil, "", err
	}
	return body, username, nil
}
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
func resolvePassword(o *loginOpts) (string, error) {
	if o.passwordStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("从标准输入读取密码失败: %w", err)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	if p := firstNonEmpty(o.password, os.Getenv("COREINSIGHT_AUTH_PASSWORD")); p != "" {
		return p, nil
	}
	if !stdinIsTerminal() {
		return "", errMissingPassword()
	}
	pw, err := promptPassword("密码（输入不回显）: ")
	if err != nil {
		return "", errMissingPassword()
	}
	return pw, nil
}
func errMissingPassword() error {
	return fmt.Errorf("缺少密码：请使用 --password-stdin 从标准输入传入，或设置环境变量 COREINSIGHT_AUTH_PASSWORD，或在交互式终端中直接运行 coreinsight-cli auth login")
}

func doLoginRequest(rt *Runtime, body []byte) (*http.Response, interface{}, error) {
	req, err := http.NewRequest(http.MethodPost, rt.ChatServer+loginPath, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if rt.Debug {
		fmt.Fprintf(os.Stderr, "[debug] POST %s%s\n", rt.ChatServer, loginPath)
	}
	resp, err := rt.httpClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, err
	}
	var payload interface{}
	if len(bytes.TrimSpace(raw)) == 0 {
		payload = nil
	} else if err := json.Unmarshal(raw, &payload); err != nil {
		payload = string(raw)
	}
	return resp, payload, nil
}
func extractCookieWithExpiry(resp *http.Response) (string, time.Time) {
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return "", time.Time{}
	}
	var parts []string
	var earliest time.Time
	now := time.Now()
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
		var exp time.Time
		switch {
		case c.MaxAge > 0:
			exp = now.Add(time.Duration(c.MaxAge) * time.Second)
		case !c.Expires.IsZero():
			exp = c.Expires
		}
		if !exp.IsZero() && (earliest.IsZero() || exp.Before(earliest)) {
			earliest = exp
		}
	}
	return strings.Join(parts, "; "), earliest
}
func businessCode(payload interface{}) (float64, string, bool) {
	m, ok := payload.(map[string]interface{})
	if !ok {
		return 0, "", false
	}
	msg, _ := m["msg"].(string)
	if msg == "" {
		msg, _ = m["message"].(string)
	}
	raw, ok := m["code"]
	if !ok {
		return 0, msg, false
	}
	switch v := raw.(type) {
	case float64:
		return v, msg, true
	case string:
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n, msg, true
		}
	}
	return 0, msg, false
}

func runAuthStatus(rt *Runtime, args []string) error {
	if hasHelp(args) {
		authStatusHelp()
		return nil
	}
	if len(args) > 0 {
		return fmt.Errorf("auth status 不接受位置参数")
	}
	id := requestID()
	if rt.Session == nil {
		failure(id, codeUnauthenticated, "Not Authenticated", map[string]interface{}{"authenticated": false, "reason": "no_session", "session_file": sessionFile(), "hint": "请执行: coreinsight-cli auth login --username <域账号>"})
		return errUnauthenticated
	}
	if expired, reason := rt.Session.expired(); expired {
		failure(id, codeUnauthenticated, "Session Expired", map[string]interface{}{"authenticated": false, "reason": reason, "username": rt.Session.Username, "saved_at": rt.Session.SavedAt, "expires_at": nullable(rt.Session.ExpiresAt), "session_file": sessionFile(), "hint": "会话已过期，请重新执行: coreinsight-cli auth login --username <域账号>"})
		return errUnauthenticated
	}
	success(id, map[string]interface{}{"authenticated": true, "source": "session_file", "username": nullable(rt.Session.Username), "server": rt.ChatServer, "cookie": maskedCookie(rt.Session.Cookie), "session_file": sessionFile(), "saved_at": rt.Session.SavedAt, "expires_at": nullable(rt.Session.ExpiresAt), "age_seconds": rt.Session.ageSeconds()})
	return nil
}
func runAuthLogout(rt *Runtime) error {
	removed, err := clearSession()
	if err != nil {
		return err
	}
	rt.Session = nil
	success(requestID(), map[string]interface{}{"authenticated": false, "cleared": removed, "session_file": sessionFile()})
	return nil
}

func authHelp() {
	plainHelp("用法:\n  coreinsight-cli auth login [登录参数]\n  coreinsight-cli auth status\n  coreinsight-cli auth logout\n")
}
func authLoginHelp() {
	plainHelp(`用法:
  coreinsight-cli auth login --username U --password P
  coreinsight-cli auth login --username U --password-stdin < pass.txt
  coreinsight-cli auth login --body '{"userName":"U","passwd":"P"}'

凭证解析:
  1. --body-file / --body（原始 JSON，兼容参考 CLI）
  2. --username/-u；未指定时读取 COREINSIGHT_AUTH_USERNAME；交互终端最后提示输入
  3. --password-stdin 优先；否则 --password/-p、COREINSIGHT_AUTH_PASSWORD、交互式无回显输入
`)
}
func authStatusHelp() {
	plainHelp("用法:\n  coreinsight-cli auth status\n\n退出码: 0=已认证，3=未认证/会话过期。\n")
}
