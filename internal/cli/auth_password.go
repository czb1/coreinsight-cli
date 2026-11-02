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

// IDaaS password login uses its own endpoint and payload, independently of devUC.
const passwordLoginPath = "/rest/hwidcenter/login"

func runPasswordLogin(rt *Runtime, o *loginOpts, set map[string]bool) error {
	body, username, err := buildPasswordLoginBody(o, set)
	if err != nil {
		return err
	}
	if set["caller_id"] && strings.TrimSpace(o.callerID) != username {
		return fmt.Errorf("密码登录的 --caller_id 必须与登录工号一致；通常可省略，自动绑定登录账号")
	}
	if !validAuthorizeURL(rt.AuthServer) {
		return fmt.Errorf("账号密码登录地址无效，请检查 --auth-server 或 COREINSIGHT_AUTH_SERVER")
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

func buildPasswordLoginBody(o *loginOpts, set map[string]bool) ([]byte, string, error) {
	// Preserve the original --body-file > --body > credential-fields order.
	// Raw JSON is sent unchanged, so callers can supply their IDaaS app fields.
	if set["body-file"] || set["body"] {
		const maxBodyBytes = 64 << 10
		raw := []byte(o.body)
		if set["body-file"] {
			f, err := os.Open(o.bodyFile)
			if err != nil {
				return nil, "", fmt.Errorf("无法读取登录 --body-file")
			}
			defer f.Close()
			raw, err = io.ReadAll(io.LimitReader(f, maxBodyBytes+1))
			if err != nil {
				return nil, "", fmt.Errorf("无法读取登录 --body-file")
			}
		}
		if len(raw) > maxBodyBytes {
			return nil, "", fmt.Errorf("登录 JSON 请求体过大")
		}
		var payload map[string]interface{}
		if json.Unmarshal(raw, &payload) != nil || payload == nil {
			return nil, "", fmt.Errorf("登录请求体必须是合法 JSON 对象")
		}
		username, _ := payload["loginAccount"].(string)
		legacyName, _ := payload["userName"].(string)
		if username == "" {
			username = legacyName
		} else if legacyName != "" && legacyName != username {
			return nil, "", fmt.Errorf("登录请求体中的账号字段不一致")
		}
		username = strings.TrimSpace(username)
		if username == "" {
			return nil, "", fmt.Errorf("登录请求体缺少 loginAccount 工号")
		}
		if (set["username"] || set["u"]) && strings.TrimSpace(o.username) != username {
			return nil, "", fmt.Errorf("--username 必须与登录请求体的工号一致")
		}
		return raw, username, nil
	}

	username := strings.TrimSpace(o.username)
	if !set["username"] && !set["u"] {
		username = strings.TrimSpace(os.Getenv("COREINSIGHT_AUTH_USERNAME"))
		if username == "" {
			username = strings.TrimSpace(os.Getenv("OMRES_AUTH_USERNAME"))
		}
		if username == "" && stdinIsTerminal() {
			var err error
			username, err = promptUsername()
			if err != nil {
				return nil, "", err
			}
		}
	}
	if username == "" {
		return nil, "", fmt.Errorf("密码登录缺少工号：请使用 --username、COREINSIGHT_AUTH_USERNAME 或 OMRES_AUTH_USERNAME")
	}
	password, err := resolveLoginPassword(o, set)
	if err != nil {
		return nil, "", err
	}
	body, err := json.Marshal(map[string]string{
		"loginAccount":            username,
		"uid":                     username,
		"password":                password,
		"lang":                    "zh_CN",
		"cid":                     "",
		"appId":                   "com.huawei.so.iam.iam_heds",
		"encryptedPasswordSwitch": "off",
	})
	return body, username, err
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
			password = os.Getenv("OMRES_AUTH_PASSWORD")
		}
		if password == "" {
			if !stdinIsTerminal() {
				return "", fmt.Errorf("缺少密码：请使用 --password-stdin、COREINSIGHT_AUTH_PASSWORD / OMRES_AUTH_PASSWORD，或在终端执行 auth login --username <工号> 无回显输入")
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
	// IDaaS reports success with code=0; devUC's code=200 is a different contract.
	switch payload["code"] {
	case float64(0), "0":
	default:
		return "", time.Time{}, fmt.Errorf("账号密码登录接口未返回成功业务码 0")
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
