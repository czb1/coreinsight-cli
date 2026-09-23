package cli

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

func (rt *Runtime) memoryEndpoint() string {
	base := rt.MemoryServer
	if base == "" {
		base = rt.CoreInsightServer
	}
	return strings.TrimRight(base, "/") + "/memory/experience/doc"
}

// Do not retry writes or guess another endpoint after a failed request.
func apiRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	if len(via) > 0 && req.Method != via[0].Method {
		return http.ErrUseLastResponse
	}
	return nil
}

func requestError(req *http.Request, err error) error {
	kind := "请求失败"
	var dns *net.DNSError
	var op *net.OpError
	var network net.Error
	switch {
	case errors.As(err, &dns):
		kind = "DNS 解析失败，请检查内网 DNS/VPN"
	case errors.As(err, &op) && op.Op == "dial":
		kind = "连接失败，请检查服务地址、端口、代理和内网/VPN"
	case errors.As(err, &network) && network.Timeout():
		kind = "请求超时，可能发生在连接或等待响应阶段；请检查服务地址、代理和后端耗时"
	}
	return fmt.Errorf("%s %s: %s: %w", req.Method, diagnosticURL(req.URL), kind, err)
}

func diagnosticURL(u *url.URL) string {
	if u == nil {
		return ""
	}
	copy := *u
	copy.User = nil
	copy.RawQuery = ""
	copy.ForceQuery = false
	copy.Fragment = ""
	return copy.String()
}

func httpStatusError(resp *http.Response) error {
	request := ""
	if resp.Request != nil {
		request = resp.Request.Method + " " + diagnosticURL(resp.Request.URL) + ": "
	}
	detail := ""
	if resp.StatusCode == http.StatusMethodNotAllowed {
		detail = "; 请核对服务基址、网关前缀和接口路由，405 本身不能证明后端禁止上传"
		if allow := resp.Header.Get("Allow"); allow != "" {
			detail += "; Allow=" + allow
		}
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		detail = "; API 重定向未跟随，请直接配置最终接口的服务基址"
		if location, err := resp.Location(); err == nil {
			detail += "; Location=" + diagnosticURL(location)
		}
	}
	return fmt.Errorf("%sHTTP %s%s", request, resp.Status, detail)
}
