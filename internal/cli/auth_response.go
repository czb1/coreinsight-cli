package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// Errors deliberately omit response bodies: they can contain credentials or SSO URLs.
func readAuthResponse(resp *http.Response) ([]byte, error) {
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("授权接口返回 HTTP %d", resp.StatusCode)
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if mediaType == "text/html" || mediaType == "application/xhtml+xml" {
		return nil, fmt.Errorf("授权接口返回了 HTML 页面")
	}
	const maxResponseBytes = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取授权接口响应失败")
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("授权接口响应过大")
	}
	// Validate the actual payload below. Preserve the existing bare-string
	// URL/token contract independently of the declared media type.
	return raw, nil
}

// Accept the legacy plain-string format and explicit successful JSON envelopes.
// Empty/null responses represent pending authorization. Never stringify JSON errors
// or non-string data into credentials.
func authResponseString(raw []byte) (string, error) {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return "", nil
	}
	var value interface{}
	if err := json.Unmarshal([]byte(body), &value); err != nil {
		if strings.ContainsAny(body[:1], "{[\"") {
			return "", fmt.Errorf("授权接口返回了无效 JSON")
		}
		return body, nil
	}
	switch v := value.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(v), nil
	case map[string]interface{}:
		if err := checkAuthSuccess(v); err != nil {
			return "", err
		}
		data, exists := v["data"]
		if !exists {
			return "", fmt.Errorf("授权接口 JSON 缺少 data")
		}
		if data == nil {
			return "", nil
		}
		if text, ok := data.(string); ok {
			return strings.TrimSpace(text), nil
		}
		return "", fmt.Errorf("授权接口 data 必须为字符串或 null")
	default:
		return "", fmt.Errorf("授权接口响应必须为字符串或成功 JSON 对象")
	}
}

// Both login methods require an explicit success marker. Do not echo backend
// messages: an error response can contain the password or session credentials.
func checkAuthSuccess(payload map[string]interface{}) error {
	hasSuccess := false
	if code, exists := payload["code"]; exists {
		switch code {
		case float64(0), float64(200), "0", "200":
			hasSuccess = true
		default:
			return fmt.Errorf("授权接口返回了非成功业务码")
		}
	}
	if success, exists := payload["success"]; exists {
		if success != true {
			return fmt.Errorf("授权接口未报告成功")
		}
		hasSuccess = true
	}
	if !hasSuccess {
		return fmt.Errorf("授权接口 JSON 缺少明确的成功状态")
	}
	return nil
}

// Bearer-token syntax; do not impose JWT structure on opaque devUC tokens.
var bearerTokenPattern = regexp.MustCompile(`^[A-Za-z0-9._~+/-]+=*$`)

func validBearerToken(token string) bool {
	if !bearerTokenPattern.MatchString(token) {
		return false
	}
	switch strings.ToLower(token) {
	case "null", "nil", "undefined", "true", "false", "error", "unauthorized", "forbidden", "pending", "authorization_pending", "access_denied":
		return false
	}
	return true
}

func extractToken(raw []byte) (string, error) {
	token, err := authResponseString(raw)
	if err != nil || token == "" {
		return "", err
	}
	if !validBearerToken(token) {
		return "", fmt.Errorf("授权接口未返回格式有效的 token")
	}
	return token, nil
}

func validAuthorizeURL(value string) bool {
	if strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	u, err := url.Parse(value)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil
}
