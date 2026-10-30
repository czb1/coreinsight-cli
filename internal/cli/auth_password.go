package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Restore the original password API contract, independently of devUC.
const passwordLoginPath = "/api/auth/login"

func runPasswordLogin(rt *Runtime, o *loginOpts, set map[string]bool) error {
	username := strings.TrimSpace(o.username)
	if !set["username"] && !set["u"] {
		username = strings.TrimSpace(os.Getenv("COREINSIGHT_AUTH_USERNAME"))
	}
	if username == "" {
		return fmt.Errorf("密码登录缺少工号：请使用 --username 或 COREINSIGHT_AUTH_USERNAME")
	}
	if set["caller_id"] && strings.TrimSpace(o.callerID) != username {
		return fmt.Errorf("密码登录的 --caller_id 必须与 --username 工号一致；通常可省略，自动绑定登录账号")
	}
	password, err := resolveLoginPassword(o, set)
	if err != nil {
		return err
	}
	if !validAuthorizeURL(rt.AuthServer) {
		return fmt.Errorf("账号密码登录地址无效，请检查 --auth-server 或 COREINSIGHT_AUTH_SERVER")
	}
	body, err := json.Marshal(map[string]string{"userName": username, "passwd": password})
	if err != nil {
		return err
	}
	cookie, expires, err := requestPasswordLogin(rt, body)
	if err != nil {
		failure(requestID(), codeBackendError, "Password Login Failed", err.Error())
		return errHandled
	}
	sess := &Session{
		Cookie: cookie, Username: username, CallerID: username,
		Server: rt.AuthServer, SavedAt: time.Now().Format(time.RFC3339),
	}
	if !expires.IsZero() {
		sess.ExpiresAt = expires.Format(time.RFC3339)
	}
	if err := saveSession(sess); err != nil {
		failure(requestID(), codeInternalError, "Session Save Failed", err.Error())
		return errHandled
	}
	// Replace the previous session only after validation and persistence succeed.
	rt.Session = sess
	success(requestID(), map[string]interface{}{
		"authenticated": true, "credential_state": "stored", "server_verified": false,
		"source": "password_login_response", "method": "password", "username": username,
		"caller_id": username, "caller_id_bound": true, "caller_id_source": "login_username",
		"browser_open_requested": false, "browser_launcher_succeeded": false,
		"server": rt.AuthServer, "cookie": maskedCookie(cookie), "session_file": sessionFile(),
		"saved_at": sess.SavedAt, "expires_at": nullable(sess.ExpiresAt),
		"hint": "账号密码接口已返回成功和 Cookie，已绑定登录工号；尚未验证业务接口。",
	})
	return nil
}

func resolveLoginPassword(o *loginOpts, set map[string]bool) (string, error) {
	explicit := set["password"] || set["p"]
	if o.passwordStdin && explicit {
		return "", fmt.Errorf("--password 与 --password-stdin 不能同时使用")
	}
	var password string
	switch {
	case o.passwordStdin:
		const maxPasswordBytes = 64 << 10
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, maxPasswordBytes+1))
		if err != nil || len(raw) > maxPasswordBytes {
			return "", fmt.Errorf("读取密码失败或输入过长")
		}
		password = strings.TrimRight(string(raw), "\r\n")
	case explicit:
		password = o.password
	default:
		password = os.Getenv("COREINSIGHT_AUTH_PASSWORD")
		if password == "" {
			if !stdinIsTerminal() {
				return "", fmt.Errorf("缺少密码：请使用 --password-stdin、COREINSIGHT_AUTH_PASSWORD，或在终端执行 auth login --username <工号> 无回显输入")
			}
			var err error
			password, err = promptPassword()
			if err != nil {
				return "", err
			}
		}
	}
	if password == "" {
		return "", fmt.Errorf("密码不能为空")
	}
	return password, nil
}

func requestPasswordLogin(rt *Runtime, body []byte) (string, time.Time, error) {
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(rt.AuthServer, "/")+passwordLoginPath, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("无法创建账号密码登录请求")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if rt.Debug {
		fmt.Fprintln(os.Stderr, "[debug] 请求账号密码登录接口（凭证与响应不输出）")
	}
	client := rt.httpClient()
	defer client.CloseIdleConnections()
	// Never redirect a password-bearing request or mistake an SSO HTML page for success.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("账号密码登录请求失败，请检查登录地址、网络和超时设置")
	}
	defer resp.Body.Close()
	raw, err := readAuthResponse(resp)
	if err != nil {
		return "", time.Time{}, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return "", time.Time{}, fmt.Errorf("账号密码登录接口未返回有效 JSON 对象")
	}
	if err := checkAuthSuccess(payload); err != nil {
		return "", time.Time{}, err
	}
	cookie, expires := extractCookieWithExpiry(resp)
	if cookie == "" {
		return "", time.Time{}, fmt.Errorf("登录响应中没有可用的 Set-Cookie，未保存会话")
	}
	return cookie, expires, nil
}

func extractCookieWithExpiry(resp *http.Response) (string, time.Time) {
	// Keep the final value for each cookie name, including deletion cookies.
	values := make(map[string]*http.Cookie)
	var names []string
	for _, c := range resp.Cookies() {
		if _, exists := values[c.Name]; !exists {
			names = append(names, c.Name)
		}
		values[c.Name] = c
	}
	now := time.Now()
	var parts []string
	var earliest time.Time
	for _, name := range names {
		c := values[name]
		if c.Value == "" || c.MaxAge < 0 {
			continue
		}
		expires := c.Expires
		if c.MaxAge > 0 {
			expires = now.Add(time.Duration(c.MaxAge) * time.Second)
		}
		if !expires.IsZero() {
			if !expires.After(now) {
				continue
			}
			if earliest.IsZero() || expires.Before(earliest) {
				earliest = expires
			}
		}
		parts = append(parts, (&http.Cookie{Name: c.Name, Value: c.Value}).String())
	}
	return strings.Join(parts, "; "), earliest
}
