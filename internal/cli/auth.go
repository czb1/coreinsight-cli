package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// devUC 浏览器授权登录接口路径，与 agentcenter CLI 完全一致：
//
//	GET <marketBaseURL>/mcpService/external/v1/auth/devUC/authorizeUrl?fromSource=<key>
//	GET <marketBaseURL>/mcpService/external/v1/auth/devUC/token?fromSource=<key>
const (
	devUCAuthorizePath = "/mcpService/external/v1/auth/devUC/authorizeUrl"
	devUCTokenPath     = "/mcpService/external/v1/auth/devUC/token"
)

// pollInterval / pollMaxRetry 声明为 var 便于测试覆盖加速轮询。
var (
	pollInterval  = 1 * time.Second
	pollMaxRetry  = 60
	fromSourceFn  = defaultFromSource
	openBrowserFn = openBrowser
)

var errUnauthenticated = errors.New("unauthenticated")
var errAuthorizationTimeout = errors.New("授权超时：未在时限内收到本次浏览器授权的 token")

type loginOpts struct {
	callerID      string
	noBrowser     bool
	timeoutSecs   int
	method        string
	username      string
	password      string
	passwordStdin bool
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
	var o loginOpts
	fs := newFS("auth login")
	fs.StringVar(&o.method, "method", "", "登录方式 browser/password；默认浏览器，显式账密参数选择密码登录")
	fs.StringVar(&o.username, "username", "", "账号密码登录的工号，同时绑定为 caller_id")
	fs.StringVar(&o.username, "u", "", "--username 的简写")
	fs.StringVar(&o.password, "password", "", "密码；推荐使用交互输入或 --password-stdin")
	fs.StringVar(&o.password, "p", "", "--password 的简写")
	fs.BoolVar(&o.passwordStdin, "password-stdin", false, "从标准输入读取密码")
	fs.StringVar(&o.callerID, "caller_id", "", "本次登录绑定的调用者工号，后续业务命令自动携带")
	fs.BoolVar(&o.noBrowser, "no-browser", false, "不自动打开浏览器，仅打印授权链接供手动复制")
	fs.IntVar(&o.timeoutSecs, "timeout", pollMaxRetry, "授权轮询超时秒数（默认 60）")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			authLoginHelp()
			return nil
		}
		return err
	}
	if o.timeoutSecs <= 0 {
		return fmt.Errorf("--timeout 必须为正整数秒")
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("auth login 不接受位置参数")
	}
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	credentials := set["username"] || set["u"] || set["password"] || set["p"] || o.passwordStdin
	switch o.method {
	case "", "browser", "password":
	default:
		return fmt.Errorf("--method 必须为 browser 或 password")
	}
	if o.method == "browser" && credentials {
		return fmt.Errorf("--method browser 不能与账号密码参数一起使用")
	}
	if o.method == "password" || credentials {
		if o.noBrowser || set["timeout"] {
			return fmt.Errorf("密码登录不使用 --no-browser 或登录后的 --timeout；HTTP 超时请放在 auth login 之前")
		}
		return runPasswordLogin(rt, &o, set)
	}

	o.callerID = strings.TrimSpace(o.callerID)
	if err := required("caller_id", o.callerID); err != nil {
		return fmt.Errorf("登录时需输入一次调用者工号：coreinsight-cli auth login --caller_id <工号>；后续调用自动读取会话")
	}

	id := requestID()
	market := rt.MarketServer
	if market == "" {
		failure(id, codeInternalError, "No Market Server", "未配置市场服务地址，请使用 --market-server 或环境变量 COREINSIGHT_MARKET_SERVER 指定")
		return errHandled
	}
	fromSource, err := fromSourceFn()
	if err != nil {
		failure(id, codeInternalError, "Authorization Initialization Failed", "无法生成本次授权标识")
		return errHandled
	}

	// 1. 请求授权 URL
	authURL, err := fetchAuthorizeURL(rt, market, fromSource)
	if err != nil {
		failure(id, codeInternalError, "Request Authorize URL Failed", err.Error())
		return errHandled
	}

	// 2. 打开浏览器（或仅打印链接）
	fmt.Fprintf(os.Stderr, "请在浏览器中完成授权。若未自动打开，请手动复制以下链接：\n  %s\n", authURL)
	browserLaunched := false
	if !o.noBrowser {
		if err := openBrowserFn(authURL); err != nil {
			fmt.Fprintf(os.Stderr, "[警告] 自动打开浏览器失败：%v，请手动复制上方链接。\n", err)
		} else {
			browserLaunched = true
		}
	}

	// 3. 轮询 token
	token, err := pollToken(rt, market, fromSource, o.timeoutSecs)
	if err != nil {
		message := "Authorization Failed"
		if errors.Is(err, errAuthorizationTimeout) {
			message = "Authorization Timeout"
		}
		failure(id, codeInternalError, message, err.Error())
		return errHandled
	}

	// 4. 后端未提供有效期；不据此推断 token 永久有效。
	sess := &Session{
		Token:    token,
		CallerID: o.callerID,
		Server:   rt.AuthServer,
		Market:   market,
		SavedAt:  time.Now().Format(time.RFC3339),
	}
	if err := saveSession(sess); err != nil {
		failure(id, codeInternalError, "Session Save Failed", err.Error())
		return errHandled
	}
	rt.Session = sess

	success(id, map[string]interface{}{
		"authenticated":              true,
		"credential_state":           "stored",
		"server_verified":            false,
		"source":                     "devuc_token_response",
		"method":                     "browser",
		"caller_id":                  sess.callerID(),
		"caller_id_bound":            true,
		"caller_id_source":           "login_input",
		"browser_open_requested":     !o.noBrowser,
		"browser_launcher_succeeded": browserLaunched,
		"hint":                       "已获取并保存本次授权返回的凭证；尚未验证业务接口。saved_at 是本地保存时间，不是用户点击授权的时间。",
		"server":                     rt.AuthServer,
		"market":                     market,
		"token":                      maskedToken(token),
		"session_file":               sessionFile(),
		"saved_at":                   sess.SavedAt,
		"expires_at":                 nullable(sess.ExpiresAt),
	})
	return nil
}

// fetchAuthorizeURL 向市场服务请求 devUC 授权页 URL。
// 响应可能是纯字符串，也可能是 {"code":200,"data":"<url>"} 形式，二者均兼容。
func fetchAuthorizeURL(rt *Runtime, market, fromSource string) (string, error) {
	fullURL := market + devUCAuthorizePath + "?fromSource=" + fromSource
	if rt.Debug {
		fmt.Fprintf(os.Stderr, "[debug] 请求 devUC 授权链接\n")
	}
	req, err := http.NewRequest(http.MethodGet, fullURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := rt.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("授权页请求失败，请检查市场地址、网络和超时设置")
	}
	defer resp.Body.Close()
	raw, err := readAuthResponse(resp)
	if err != nil {
		return "", err
	}
	value, err := authResponseString(raw)
	if err != nil {
		return "", err
	}
	if !validAuthorizeURL(value) {
		return "", fmt.Errorf("授权页响应不是有效的 HTTP(S) URL")
	}
	return value, nil
}

// pollToken 轮询 devUC token 接口，直到拿到非空 token 或超时。
func pollToken(rt *Runtime, market, fromSource string, maxRetry int) (string, error) {
	fullURL := market + devUCTokenPath + "?fromSource=" + fromSource
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(maxRetry)*pollInterval)
	defer cancel()
	for i := 0; i < maxRetry; i++ {
		if rt.Debug {
			fmt.Fprintf(os.Stderr, "[debug] poll token attempt %d/%d\n", i+1, maxRetry)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := rt.httpClient().Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return "", errAuthorizationTimeout
			}
			return "", fmt.Errorf("token 请求失败，请检查市场地址、网络和超时设置")
		}
		raw, err := readAuthResponse(resp)
		_ = resp.Body.Close()
		if err != nil {
			if ctx.Err() != nil {
				return "", errAuthorizationTimeout
			}
			return "", err
		}
		token, err := extractToken(raw)
		if err != nil {
			return "", err
		}
		if token != "" {
			return token, nil
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", errAuthorizationTimeout
		case <-timer.C:
		}
	}
	return "", errAuthorizationTimeout
}

// 每次显式登录创建独立授权事务，不用机器固定标识找回历史 token。
func defaultFromSource() (string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(nonce[:]), nil
}

// openBrowser 跨平台打开默认浏览器。
// 等待启动命令结束以捕获失败；成功不等于窗口可见或用户已确认授权。
func openBrowser(url string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return browserCommand(ctx, runtime.GOOS, url).Run()
}

func browserCommand(ctx context.Context, platform, url string) *exec.Cmd {
	var cmd *exec.Cmd
	switch platform {
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
			"$ErrorActionPreference = 'Stop'; Start-Process -FilePath '"+strings.ReplaceAll(url, "'", "''")+"'")
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd
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
		failure(id, codeUnauthenticated, "Not Authenticated", map[string]interface{}{"authenticated": false, "reason": "no_session", "session_file": sessionFile(), "hint": "请执行: coreinsight-cli auth login --caller_id <工号>（浏览器授权登录）"})
		return errUnauthenticated
	}
	if expired, reason := rt.Session.expired(); expired {
		failure(id, codeUnauthenticated, "Session Invalid", map[string]interface{}{"authenticated": false, "reason": reason, "username": rt.Session.Username, "saved_at": rt.Session.SavedAt, "expires_at": nullable(rt.Session.ExpiresAt), "session_file": sessionFile(), "hint": "本地会话无效或已过期，请重新执行: coreinsight-cli auth login --caller_id <工号>（浏览器授权登录）"})
		return errUnauthenticated
	}
	success(id, map[string]interface{}{
		"authenticated":    true,
		"credential_state": "stored",
		"server_verified":  false,
		"hint":             "仅检查本地凭证；尚未验证服务端授权。saved_at 是本地保存时间，不是浏览器授权时间。",
		"source":           "session_file",
		"username":         nullable(rt.Session.Username),
		"caller_id":        nullable(rt.Session.callerID()),
		"caller_id_bound":  rt.Session.callerID() != "",
		"server":           rt.AuthServer,
		"market":           nullable(rt.Session.Market),
		"token":            maskedToken(rt.Session.Token),
		"cookie":           maskedCookie(rt.Session.Cookie),
		"session_file":     sessionFile(),
		"saved_at":         rt.Session.SavedAt,
		"expires_at":       nullable(rt.Session.ExpiresAt),
		"age_seconds":      rt.Session.ageSeconds(),
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
	plainHelp("用法:\n  coreinsight-cli auth login --caller_id <工号> [登录参数]  默认浏览器\n  coreinsight-cli auth login --username <工号> [密码参数]  账号密码登录\n  coreinsight-cli auth status\n  coreinsight-cli auth logout\n")
}
func authLoginHelp() {
	plainHelp(`用法:
  coreinsight-cli auth login --caller_id <工号>
  coreinsight-cli auth login --caller_id <工号> --no-browser
  coreinsight-cli auth login --caller_id <工号> --timeout 120
  coreinsight-cli auth login --username <工号>
  coreinsight-cli auth login --username <工号> --password-stdin
  coreinsight-cli auth login --username <工号> --password <密码>
  coreinsight-cli auth login --method password

默认使用浏览器；显式 --username/-u、--password/-p 或 --password-stdin
选择账号密码登录，也可使用 --method password。环境变量本身不切换默认方式。
--method browser 与账密参数不能混用；--no-browser 只用于浏览器授权。

浏览器授权登录（复用 agentcenter devUC 流程）:
  1. 每次生成新的随机授权标识，向市场服务请求授权页 URL
  2. 自动打开默认浏览器（--no-browser 则仅打印链接供手动复制）
  3. 用户在浏览器完成授权，CLI 轮询后端获取 token
  4. 校验成功响应后，将 token 和本次输入的 caller_id 一起保存到会话

参数:
  --caller_id <工号>  浏览器登录必填；密码登录自动使用 username，若传入则必须一致
  --no-browser        不自动打开浏览器，仅打印授权链接
  --timeout <seconds> 授权轮询超时秒数（默认 60）
  --market-server <url> 覆盖市场服务地址（默认 https://agent.huawei.com，可用环境变量 COREINSIGHT_MARKET_SERVER）

账号密码登录（POST https://omtool.rnd.huawei.com/api/auth/login）:
  --username/-u <工号>  登录账号；未传时读取 COREINSIGHT_AUTH_USERNAME
  --password/-p <密码>  直接传入密码（可能留在命令历史或进程参数，推荐以下方式）
  --password-stdin     从标准输入读取密码，不能与 --password 同时使用
  未指定密码时读取 COREINSIGHT_AUTH_PASSWORD，再尝试终端无回显输入。
  --method password    显式选择密码登录，可配合上述环境变量用于脚本
  --auth-server <url>  覆盖密码登录服务，可用 COREINSIGHT_AUTH_SERVER；不影响 devUC
  请求体为 userName/passwd；校验成功状态及 Cookie 后保存，会话不存储密码。
  HTTP 超时放在命令前，例如: coreinsight-cli --timeout 30 auth login --username <工号>

注: --no-browser 仅关闭自动打开，仍需完成本次授权。已有浏览器 SSO
    可能自动完成授权。后端未返回有效期不代表 token 永久有效。
    auth status 只检查本地凭证，不验证服务端。logout 只清除本地凭证。
    浏览器 caller_id 由本次输入，并非服务端返回的已验证身份；请填当前 SSO 账号工号。
    密码登录将 username 绑定为 caller_id；业务请求使用 Cookie，不继承旧 token。
    user_id 是检索过滤字段，不作为登录身份；业务命令不能覆盖会话 caller_id。
`)
}
func authStatusHelp() {
	plainHelp("用法:\n  coreinsight-cli auth status\n\n退出码: 0=本地存在格式有效且未标记过期的凭证，3=本地凭证缺失/无效/过期。\n不会请求服务端验证；server_verified 固定为 false。\ncaller_id_bound=false 表示旧会话未绑定工号；执行需要调用者身份的任务前，请 auth login --caller_id <工号>。\n")
}
