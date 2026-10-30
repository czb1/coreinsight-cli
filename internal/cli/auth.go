package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// devUC 浏览器授权登录接口路径，与 agentcenter CLI 完全一致：
//   GET <marketBaseURL>/mcpService/external/v1/auth/devUC/authorizeUrl?fromSource=<key>
//   GET <marketBaseURL>/mcpService/external/v1/auth/devUC/token?fromSource=<key>
const (
	devUCAuthorizePath = "/mcpService/external/v1/auth/devUC/authorizeUrl"
	devUCTokenPath     = "/mcpService/external/v1/auth/devUC/token"
	// fromSourceSalt 是机器绑定标识的 salt，使用 coreinsight-cli 自有 salt
	// 避免与 agentcenter 会话冲突。
	fromSourceSalt = "coreinsight-cli"
)

// pollInterval / pollMaxRetry 声明为 var 便于测试覆盖加速轮询。
var (
	pollInterval  = 1 * time.Second
	pollMaxRetry  = 60
	fromSourceFn  = defaultFromSource
	openBrowserFn = openBrowser
)

var errUnauthenticated = errors.New("unauthenticated")

type loginOpts struct {
	noBrowser    bool
	timeoutSecs  int
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
	fs.BoolVar(&o.noBrowser, "no-browser", false, "不自动打开浏览器，仅打印授权链接供手动复制")
	fs.IntVar(&o.timeoutSecs, "timeout", 0, "授权轮询超时秒数（默认 60）")
	if err := fs.Parse(args); err != nil {
		return err
	}

	id := requestID()
	market := rt.MarketServer
	if market == "" {
		failure(id, codeInternalError, "No Market Server", "未配置市场服务地址，请使用 --market-server 或环境变量 COREINSIGHT_MARKET_SERVER 指定")
		return errHandled
	}
	fromSource := fromSourceFn()

	// 1. 请求授权 URL
	authURL, err := fetchAuthorizeURL(rt, market, fromSource)
	if err != nil {
		failure(id, codeInternalError, "Request Authorize URL Failed", err.Error())
		return errHandled
	}

	// 2. 打开浏览器（或仅打印链接）
	fmt.Fprintf(os.Stderr, "请在浏览器中完成授权。若未自动打开，请手动复制以下链接：\n  %s\n", authURL)
	if !o.noBrowser {
		if err := openBrowserFn(authURL); err != nil {
			fmt.Fprintf(os.Stderr, "[警告] 自动打开浏览器失败：%v，请手动复制上方链接。\n", err)
		}
	}

	// 3. 轮询 token
	maxRetry := pollMaxRetry
	if o.timeoutSecs > 0 {
		maxRetry = o.timeoutSecs
	}
	token, err := pollToken(rt, market, fromSource, maxRetry)
	if err != nil {
		failure(id, codeInternalError, "Authorization Timeout", err.Error())
		return errHandled
	}

	// 4. 保存会话（token 永久有效，不设过期时间）
	sess := &Session{
		Token:   token,
		Server:  rt.AuthServer,
		Market:  market,
		SavedAt: time.Now().Format(time.RFC3339),
	}
	if err := saveSession(sess); err != nil {
		failure(id, codeInternalError, "Session Save Failed", err.Error())
		return errHandled
	}
	rt.Session = sess

	success(id, map[string]interface{}{
		"authenticated": true,
		"server":        rt.AuthServer,
		"market":        market,
		"token":         maskedToken(token),
		"session_file":  sessionFile(),
		"saved_at":      sess.SavedAt,
		"expires_at":    nullable(sess.ExpiresAt),
	})
	return nil
}

// fetchAuthorizeURL 向市场服务请求 devUC 授权页 URL。
// 响应可能是纯字符串，也可能是 {"code":200,"data":"<url>"} 形式，二者均兼容。
func fetchAuthorizeURL(rt *Runtime, market, fromSource string) (string, error) {
	fullURL := market + devUCAuthorizePath + "?fromSource=" + fromSource
	if rt.Debug {
		fmt.Fprintf(os.Stderr, "[debug] GET %s\n", fullURL)
	}
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := rt.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	body := strings.TrimSpace(string(raw))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("授权页请求失败: HTTP %d: %s", resp.StatusCode, body)
	}
	// 优先按 JSON 解析提取 data 字段；失败则视作纯字符串 URL。
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(body), &m); err == nil {
		if d, ok := m["data"]; ok {
			switch v := d.(type) {
			case string:
				if v != "" {
					return v, nil
				}
			default:
				if s := scalarString(d); s != "" {
					return s, nil
				}
			}
		}
	}
	if body != "" {
		return body, nil
	}
	return "", fmt.Errorf("授权页响应为空")
}

// pollToken 轮询 devUC token 接口，直到拿到非空 token 或超时。
func pollToken(rt *Runtime, market, fromSource string, maxRetry int) (string, error) {
	fullURL := market + devUCTokenPath + "?fromSource=" + fromSource
	for i := 0; i < maxRetry; i++ {
		if rt.Debug {
			fmt.Fprintf(os.Stderr, "[debug] poll token attempt %d/%d\n", i+1, maxRetry)
		}
		req, err := http.NewRequest(http.MethodGet, fullURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := rt.httpClient().Do(req)
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if token := extractToken(raw); token != "" {
			return token, nil
		}
		time.Sleep(pollInterval)
	}
	return "", fmt.Errorf("授权超时：请在 %d 秒内完成浏览器授权", int(time.Duration(maxRetry)*pollInterval/time.Second))
}

// extractToken 从 token 轮询响应中提取 token 字符串。
// 兼容纯字符串和 {"data":"<token>"} 两种返回形态。
func extractToken(raw []byte) string {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(body), &m); err == nil {
		if d, ok := m["data"]; ok {
			switch v := d.(type) {
			case string:
				return strings.TrimSpace(v)
			default:
				return strings.TrimSpace(scalarString(d))
			}
		}
	}
	return strings.TrimSpace(body)
}

// defaultFromSource 生成机器绑定的 fromSource 标识：
// SHA256(username + hostname + salt)，同一机器稳定。
func defaultFromSource() string {
	user := os.Getenv("USERNAME")
	if user == "" {
		user = os.Getenv("USER")
	}
	host, _ := os.Hostname()
	h := sha256.Sum256([]byte(user + host + fromSourceSalt))
	return hex.EncodeToString(h[:])
}

// openBrowser 跨平台打开默认浏览器。
// Windows 上用 powershell Start-Process 启动浏览器；相比 cmd /c start，
// Start-Process 创建的进程能正确绑定到用户交互桌面会话，避免在无桌面
// 会话的子进程环境（如 Agent Bash 沙箱）中浏览器窗口不弹出。
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("powershell", "-NoProfile", "-Command",
			"Start-Process '"+strings.ReplaceAll(url, "'", "''")+"'")
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
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
		failure(id, codeUnauthenticated, "Not Authenticated", map[string]interface{}{"authenticated": false, "reason": "no_session", "session_file": sessionFile(), "hint": "请执行: coreinsight-cli auth login（浏览器授权登录）"})
		return errUnauthenticated
	}
	if expired, reason := rt.Session.expired(); expired {
		failure(id, codeUnauthenticated, "Session Expired", map[string]interface{}{"authenticated": false, "reason": reason, "username": rt.Session.Username, "saved_at": rt.Session.SavedAt, "expires_at": nullable(rt.Session.ExpiresAt), "session_file": sessionFile(), "hint": "会话已过期，请重新执行: coreinsight-cli auth login（浏览器授权登录）"})
		return errUnauthenticated
	}
	success(id, map[string]interface{}{
		"authenticated": true,
		"source":        "session_file",
		"username":      nullable(rt.Session.Username),
		"server":        rt.AuthServer,
		"market":        nullable(rt.Session.Market),
		"token":         maskedToken(rt.Session.Token),
		"cookie":        maskedCookie(rt.Session.Cookie),
		"session_file":  sessionFile(),
		"saved_at":      rt.Session.SavedAt,
		"expires_at":    nullable(rt.Session.ExpiresAt),
		"age_seconds":   rt.Session.ageSeconds(),
	})
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

func maskedToken(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 8 {
		return strings.Repeat("*", len(token))
	}
	return token[:3] + "******" + token[len(token)-3:]
}

func authHelp() {
	plainHelp("用法:\n  coreinsight-cli auth login [登录参数]\n  coreinsight-cli auth status\n  coreinsight-cli auth logout\n")
}
func authLoginHelp() {
	plainHelp(`用法:
  coreinsight-cli auth login
  coreinsight-cli auth login --no-browser
  coreinsight-cli auth login --timeout 120

浏览器授权登录（复用 agentcenter devUC 流程）:
  1. CLI 向市场服务请求授权页 URL
  2. 自动打开默认浏览器（--no-browser 则仅打印链接供手动复制）
  3. 用户在浏览器完成授权，CLI 轮询后端获取 token
  4. token 本地保存（永久有效），业务请求通过 X-Auth-Token 头发送

参数:
  --no-browser        不自动打开浏览器，仅打印授权链接
  --timeout <seconds> 授权轮询超时秒数（默认 60）
  --market-server <url> 覆盖市场服务地址（默认 https://agent.huawei.com，可用环境变量 COREINSIGHT_MARKET_SERVER）

注: devUC token 是市场域凭证，由后端直接作为业务鉴权，无需额外 Cookie。
`)
}
func authStatusHelp() {
	plainHelp("用法:\n  coreinsight-cli auth status\n\n退出码: 0=已认证，3=未认证/会话过期。\n")
}
