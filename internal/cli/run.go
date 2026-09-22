package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var errHandled = errors.New("handled")

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func newFS(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	return fs
}

func Run(args []string) int {
	rt, err := defaultRuntime()
	if err != nil {
		failure(requestID(), codeInternalError, "Runtime Error", err.Error())
		return 1
	}
	args, err = parseGlobals(rt, args)
	if err != nil {
		failure(requestID(), codeInvalidParams, "Invalid Params", err.Error())
		return 1
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		rootHelp()
		return 0
	}

	var runErr error
	switch args[0] {
	case "auth":
		runErr = runAuth(rt, args[1:])
	case "qa":
		if hasHelp(args[1:]) {
			qaHelp()
			return 0
		}
		runErr = runQA(rt, args[1:], false)
	case "retrieve":
		if hasHelp(args[1:]) {
			qaHelp()
			return 0
		}
		runErr = runQA(rt, args[1:], true)
	case "skill":
		runErr = runSkill(rt, args[1:])
	case "experience":
		runErr = runExperience(rt, args[1:])
	case "version":
		success(requestID(), map[string]interface{}{"name": "coreinsight-cli", "version": "0.1.0"})
		return 0
	default:
		runErr = fmt.Errorf("未知命令: %s", args[0])
	}
	if runErr == nil {
		return 0
	}
	if errors.Is(runErr, errHandled) {
		return 1
	}
	failure(requestID(), codeInvalidParams, "Invalid Params", runErr.Error())
	return 1
}

func parseGlobals(rt *Runtime, args []string) ([]string, error) {
	out := make([]string, 0, len(args))
	serverChanged := false
	chatChanged := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		take := func() (string, error) {
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s 缺少值", a)
			}
			i++
			return args[i], nil
		}
		switch a {
		case "--server":
			v, e := take()
			if e != nil {
				return nil, e
			}
			rt.CoreInsightServer = strings.TrimRight(v, "/")
			serverChanged = true
		case "--chat-server":
			v, e := take()
			if e != nil {
				return nil, e
			}
			rt.ChatServer = strings.TrimRight(v, "/")
			chatChanged = true
		case "--ai-community-server":
			v, e := take()
			if e != nil {
				return nil, e
			}
			rt.AICommunityServer = strings.TrimRight(v, "/")
		case "--core-harness-server":
			v, e := take()
			if e != nil {
				return nil, e
			}
			rt.CoreHarnessServer = strings.TrimRight(v, "/")
		case "--timeout":
			v, e := take()
			if e != nil {
				return nil, e
			}
			var n int
			if _, e = fmt.Sscanf(v, "%d", &n); e != nil || n <= 0 {
				return nil, fmt.Errorf("--timeout 必须是正整数秒")
			}
			rt.Timeout = time.Duration(n) * time.Second
		case "--debug":
			rt.Debug = true
		default:
			out = append(out, a)
		}
	}
	if serverChanged && !chatChanged && os.Getenv("COREINSIGHT_CHAT_SERVER") == "" {
		rt.ChatServer = rt.CoreInsightServer + "/chat"
	}
	return out, nil
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
		return runAuthStatus(rt)
	default:
		return fmt.Errorf("未知 auth 子命令: %s", args[0])
	}
}

func runAuthLogin(rt *Runtime, args []string) error {
	fs := newFS("auth login")
	var username, password string
	fs.StringVar(&username, "username", "", "用户名")
	fs.StringVar(&password, "password", "", "密码")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if username == "" {
		username = os.Getenv("COREINSIGHT_AUTH_USERNAME")
	}
	if password == "" {
		password = os.Getenv("COREINSIGHT_AUTH_PASSWORD")
	}
	if err := required("username", username); err != nil {
		return err
	}
	if err := required("password", password); err != nil {
		return err
	}
	body := map[string]string{"userName": username, "passwd": password}
	rawURL := rt.CoreInsightServer + "/api/auth/login"
	raw, _ := jsonMarshal(body)
	req, err := http.NewRequest(http.MethodPost, rawURL, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: rt.Timeout}).Do(req)
	if err != nil {
		return emitReqErr(err)
	}
	defer resp.Body.Close()
	respRaw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	payload := decodeJSON(respRaw)
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		return fmt.Errorf("登录响应没有 Set-Cookie，无法建立本地会话")
	}
	parts := make([]string, 0, len(cookies))
	var earliest time.Time
	now := time.Now()
	for _, c := range cookies {
		parts = append(parts, c.Name+"="+c.Value)
		var exp time.Time
		if c.MaxAge > 0 {
			exp = now.Add(time.Duration(c.MaxAge) * time.Second)
		} else if !c.Expires.IsZero() {
			exp = c.Expires
		}
		if !exp.IsZero() && (earliest.IsZero() || exp.Before(earliest)) {
			earliest = exp
		}
	}
	s := &Session{Cookie: strings.Join(parts, "; "), Username: username, Server: rt.CoreInsightServer, SavedAt: time.Now().Format(time.RFC3339)}
	if !earliest.IsZero() {
		s.ExpiresAt = earliest.Format(time.RFC3339)
	}
	if err := saveSession(s); err != nil {
		return err
	}
	rt.Session = s
	success(requestID(), map[string]interface{}{"authenticated": true, "username": username, "server": rt.CoreInsightServer, "cookie": maskedCookie(s.Cookie), "session_file": sessionFile(), "expires_at": nullable(s.ExpiresAt)})
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
func runAuthStatus(rt *Runtime) error {
	if rt.Session == nil {
		success(requestID(), map[string]interface{}{"authenticated": false, "session_file": sessionFile()})
		return nil
	}
	success(requestID(), map[string]interface{}{"authenticated": true, "username": rt.Session.Username, "server": rt.Session.Server, "cookie": maskedCookie(rt.Session.Cookie), "session_file": sessionFile(), "saved_at": rt.Session.SavedAt, "expires_at": nullable(rt.Session.ExpiresAt)})
	return nil
}

func jsonMarshal(v interface{}) ([]byte, error) { return json.Marshal(v) }
func decodeJSON(raw []byte) interface{} {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err == nil {
		return v
	}
	return string(raw)
}

func hasHelp(args []string) bool {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			return true
		}
	}
	return false
}
func nullable(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

func emitReqErr(err error) error {
	failure(requestID(), codeInternalError, "Request Failed", err.Error())
	return errHandled
}
func emitBackendErr(err error, payload interface{}) error {
	failure(requestID(), codeBackendError, err.Error(), payload)
	return errHandled
}

func rootHelp() {
	plainHelp(`coreinsight-cli - Core Insight AI 友好命令行工具

用法:
  coreinsight-cli [全局参数] <命令> [参数]

命令:
  auth login|logout|status   登录与本地会话
  qa                         知识问答
  retrieve                   知识库纯检索
  skill ...                  Skill 场景/检索/下载/上传
  experience ...             经验检索/上传
  version                    版本信息

全局参数:
  --server <url>              Core Insight 地址（默认 https://coreinsight.rnd.huawei.com）
  --chat-server <url>         问答地址（默认 <server>/chat）
  --ai-community-server <url> AI Community 地址
  --core-harness-server <url> Core Harness 地址
  --timeout <seconds>         请求超时
  --debug                     调试日志写 stderr

所有业务输出采用 JSON-RPC 2.0；stdout 适合脚本/Agent 消费。
`)
}
func authHelp() {
	plainHelp("用法:\n  coreinsight-cli auth login --username U --password P\n  coreinsight-cli auth logout\n  coreinsight-cli auth status\n")
}
