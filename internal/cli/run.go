package cli

import (
	"errors"
	"flag"
	"fmt"
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
	case "rag":
		if hasHelp(args[1:]) {
			ragHelp()
			return 0
		}
		if err := requireAuthenticated(rt, "rag"); err != nil {
			return 3
		}
		runErr = runRAG(rt, args[1:])
	case "skill":
		if !hasHelp(args[1:]) && len(args[1:]) > 0 {
			if err := requireAuthenticated(rt, "skill"); err != nil {
				return 3
			}
		}
		runErr = runSkill(rt, args[1:])
	case "experience":
		if !hasHelp(args[1:]) && len(args[1:]) > 0 {
			if err := requireAuthenticated(rt, "experience"); err != nil {
				return 3
			}
		}
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
	if errors.Is(runErr, errUnauthenticated) {
		return 3
	}
	if errors.Is(runErr, errHandled) {
		return 1
	}
	failure(requestID(), codeInvalidParams, "Invalid Params", runErr.Error())
	return 1
}

// requireAuthenticated 校验本地登录会话：所有业务命令必须先执行
// coreinsight-cli auth login，否则返回 errUnauthenticated（映射退出码 3）。
func requireAuthenticated(rt *Runtime, command string) error {
	if rt.Session == nil {
		failure(requestID(), codeUnauthenticated, "Not Authenticated", map[string]interface{}{
			"authenticated": false, "reason": "no_session", "command": command,
			"session_file": sessionFile(),
			"hint":         "请执行: coreinsight-cli auth login（浏览器授权登录）",
		})
		return errUnauthenticated
	}
	if expired, reason := rt.Session.expired(); expired {
		failure(requestID(), codeUnauthenticated, "Session Expired", map[string]interface{}{
			"authenticated": false, "reason": reason, "command": command,
			"username": rt.Session.Username, "saved_at": rt.Session.SavedAt,
			"expires_at": nullable(rt.Session.ExpiresAt), "session_file": sessionFile(),
			"hint": "会话已过期，请重新执行: coreinsight-cli auth login（浏览器授权登录）",
		})
		return errUnauthenticated
	}
	return nil
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
			// Keep auth --server override semantics compatible with czb1/cli:
			// auth login uses the same explicitly supplied server when --server is set.
			rt.AuthServer = strings.TrimRight(v, "/")
			serverChanged = true
		case "--chat-server":
			v, e := take()
			if e != nil {
				return nil, e
			}
			rt.ChatServer = strings.TrimRight(v, "/")
			chatChanged = true
		case "--auth-server":
			v, e := take()
			if e != nil {
				return nil, e
			}
			rt.AuthServer = strings.TrimRight(v, "/")
		case "--market-server":
			v, e := take()
			if e != nil {
				return nil, e
			}
			rt.MarketServer = strings.TrimRight(v, "/")
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
  rag                         知识问答 / 检索（--qa|--retrieve）
  skill ...                  Skill 场景/检索/下载/上传
  experience ...             经验检索/上传
  version                    版本信息

注: rag / skill / experience 业务命令必须先执行 auth login，否则返回
    code -32002（Not Authenticated），退出码 3；--help 不受此限制。

全局参数:
  --server <url>              Core Insight 地址；对 auth 也作为登录 server 覆盖
  --chat-server <url>         Chat 地址，供问答和经验等接口使用（默认 <server>/chat）
  --auth-server <url>         登录地址（默认 https://login.huawei.com/login1）
  --market-server <url>       市场服务地址（devUC 浏览器授权登录，默认 https://agent.huawei.com）
  --ai-community-server <url> AI Community 地址
  --core-harness-server <url> Core Harness 地址
  --timeout <seconds>         请求超时
  --debug                     调试日志写 stderr

所有业务输出采用 JSON-RPC 2.0；stdout 适合脚本/Agent 消费。
`)
}
