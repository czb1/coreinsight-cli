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

const defaultAuthServer = "https://omtool.rnd.huawei.com"

type Runtime struct {
	CoreInsightServer string
	MemoryServer      string
	ChatServer        string
	AuthServer        string
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
		MemoryServer:      strings.TrimRight(envOr("COREINSIGHT_MEMORY_SERVER", ""), "/"),
		ChatServer:        strings.TrimRight(envOr("COREINSIGHT_CHAT_SERVER", core+"/chat"), "/"),
		AuthServer:        strings.TrimRight(envOr("COREINSIGHT_AUTH_SERVER", defaultAuthServer), "/"),
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

func (rt *Runtime) userID(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override), nil
	}
	if rt.Session != nil {
		if expired, _ := rt.Session.expired(); !expired && strings.TrimSpace(rt.Session.Username) != "" {
			return strings.TrimSpace(rt.Session.Username), nil
		}
	}
	return "", fmt.Errorf("缺少用户 ID：请先执行 coreinsight-cli auth login，或使用 --user_id 指定")
}

func (rt *Runtime) addSessionCookie(req *http.Request) {
	if rt.Session == nil || rt.Session.Cookie == "" {
		return
	}
	if expired, _ := rt.Session.expired(); expired {
		return
	}
	req.Header.Set("Cookie", rt.Session.Cookie)
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
		rt.addSessionCookie(req)
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
		rt.addSessionCookie(req)
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
