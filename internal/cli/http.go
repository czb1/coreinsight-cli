package cli

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const defaultAuthServer = "https://login.huawei.com/login1"

// defaultMarketServer 是 agentcenter devUC 浏览器授权登录所在的市场域，
// 授权页 URL 申请和 token 轮询都挂在这个 baseURL 下。
const defaultMarketServer = "https://agent.huawei.com"

type Runtime struct {
	CoreInsightServer string
	ChatServer        string
	AuthServer        string
	MarketServer      string
	AICommunityServer string
	CoreHarnessServer string
	Timeout           time.Duration
	Debug             bool
	Session           *Session
}

func defaultRuntime() (*Runtime, error) {
	timeout := 120 * time.Second
	if v := os.Getenv("COREINSIGHT_TIMEOUT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("COREINSIGHT_TIMEOUT 必须是正整数秒")
		}
		timeout = time.Duration(n) * time.Second
	}
	s, err := loadSession()
	if err != nil {
		return nil, err
	}
	core := strings.TrimRight(envOr("COREINSIGHT_SERVER", "https://coreinsight.rnd.huawei.com"), "/")
	return &Runtime{
		CoreInsightServer: core,
		ChatServer:        strings.TrimRight(envOr("COREINSIGHT_CHAT_SERVER", core+"/chat"), "/"),
		AuthServer:        strings.TrimRight(envOr("COREINSIGHT_AUTH_SERVER", defaultAuthServer), "/"),
		MarketServer:      strings.TrimRight(envOr("COREINSIGHT_MARKET_SERVER", defaultMarketServer), "/"),
		AICommunityServer: strings.TrimRight(envOr("COREINSIGHT_AI_COMMUNITY_SERVER", "https://aicommunity.coreai.rnd.huawei.com"), "/"),
		CoreHarnessServer: strings.TrimRight(envOr("COREINSIGHT_CORE_HARNESS_SERVER", "http://coreharness.spec.rnd.huawei.com"), "/"),
		Timeout:           timeout,
		Debug:             false,
		Session:           s,
	}, nil
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func (rt *Runtime) httpClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	// Internal CoreInsight/AI Community endpoints may use certificates that are not
	// trusted by the local machine. Per CLI requirements, disable TLS verification
	// for every outbound HTTPS request made by this client.
	transport.TLSClientConfig.InsecureSkipVerify = true
	return &http.Client{Timeout: rt.Timeout, Transport: transport}
}

func (rt *Runtime) callerID(expected string) (string, error) {
	if expired, _ := rt.Session.expired(); expired {
		return "", fmt.Errorf("调用者身份需要有效登录会话，请执行 coreinsight-cli auth login --caller_id <工号>")
	}
	id := rt.Session.callerID()
	if id == "" {
		return "", fmt.Errorf("当前会话未绑定调用者工号，请执行 coreinsight-cli auth login --caller_id <工号>；--user_id 仅用于检索过滤")
	}
	if expected = strings.TrimSpace(expected); expected != "" && expected != id {
		return "", fmt.Errorf("--caller_id 与登录会话不一致；业务命令不能覆盖调用者身份，请重新登录绑定目标工号")
	}
	return id, nil
}

// addSessionAuth 给业务请求注入鉴权凭证：
// 优先用 devUC 浏览器登录拿到的 Token（X-Auth-Token 头，与 agentcenter/aimarket-sdk 一致）；
// 旧版账密登录产生的 Cookie 会话文件仍兼容，回退到 Cookie 头。
func (rt *Runtime) addSessionAuth(req *http.Request) {
	if rt.Session == nil {
		return
	}
	if expired, _ := rt.Session.expired(); expired {
		return
	}
	if rt.Session.Token != "" {
		req.Header.Set("X-Auth-Token", rt.Session.Token)
		return
	}
	if rt.Session.Cookie != "" {
		req.Header.Set("Cookie", rt.Session.Cookie)
	}
}

func (rt *Runtime) doJSON(method, fullURL string, body interface{}, withSession bool) (*http.Response, interface{}, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(raw)
		if rt.Debug {
			fmt.Fprintf(os.Stderr, "[debug] body: %s\n", raw)
		}
	}
	req, err := http.NewRequest(strings.ToUpper(method), fullURL, reader)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if withSession {
		rt.addSessionAuth(req)
	}
	if rt.Debug {
		fmt.Fprintf(os.Stderr, "[debug] %s %s\n", req.Method, fullURL)
	}
	// A 301/302/303 must not silently turn an API POST into a GET.
	// Keep the login client's reference behavior unchanged.
	client := rt.httpClient()
	client.CheckRedirect = apiRedirectPolicy
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, requestError(req, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, nil, requestError(req, err)
	}
	var payload interface{}
	if len(bytes.TrimSpace(raw)) == 0 {
		payload = nil
	} else if err := json.Unmarshal(raw, &payload); err != nil {
		payload = string(raw)
	}
	return resp, payload, nil
}

func (rt *Runtime) doMultipart(fullURL string, query url.Values, fileField, filePath string, withSession bool) (*http.Response, interface{}, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(fileField, filepath.Base(filePath))
	if err != nil {
		return nil, nil, err
	}
	if _, err := io.Copy(part, f); err != nil {
		return nil, nil, err
	}
	if err := w.Close(); err != nil {
		return nil, nil, err
	}

	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodPost, fullURL, &buf)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if withSession {
		rt.addSessionAuth(req)
	}
	if rt.Debug {
		fmt.Fprintf(os.Stderr, "[debug] POST %s (multipart file=%s)\n", fullURL, filePath)
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
	if err := json.Unmarshal(raw, &payload); err != nil {
		payload = string(raw)
	}
	return resp, payload, nil
}

func backendOK(resp *http.Response, payload interface{}) error {
	if resp == nil {
		return fmt.Errorf("无 HTTP 响应")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return httpStatusError(resp)
	}
	if m, ok := payload.(map[string]interface{}); ok {
		if success, exists := m["success"].(bool); exists && !success {
			return fmt.Errorf("后端 success=false")
		}
		if raw, exists := m["code"]; exists {
			code := 0
			switch v := raw.(type) {
			case float64:
				code = int(v)
			case string:
				code, _ = strconv.Atoi(v)
			}
			if code != 0 && code != 200 {
				if msg, _ := m["msg"].(string); msg != "" {
					return fmt.Errorf("后端错误 %d: %s", code, msg)
				}
				return fmt.Errorf("后端错误 code=%d", code)
			}
		}
		if meta, ok := m["meta"].(map[string]interface{}); ok {
			if okv, exists := meta["success"].(bool); exists && !okv {
				msg, _ := meta["message"].(string)
				if msg == "" {
					msg = "meta.success=false"
				}
				return fmt.Errorf("后端错误: %s", msg)
			}
		}
	}
	return nil
}

func dataField(payload interface{}) interface{} {
	if m, ok := payload.(map[string]interface{}); ok {
		return m["data"]
	}
	return nil
}
