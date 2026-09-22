package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func csvList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func routePayload(kb, products, departments, scenes string) (string, []string, error) {
	choices := []struct {
		key string
		raw string
	}{{"kb_sns", kb}, {"products", products}, {"departments", departments}, {"scenes", scenes}}
	selected := ""
	var values []string
	count := 0
	for _, c := range choices {
		if v := csvList(c.raw); len(v) > 0 {
			selected, values = c.key, v
			count++
		}
	}
	if count != 1 {
		return "", nil, fmt.Errorf("--kb_sns / --products / --departments / --scenes 必须且只能选择一个")
	}
	return selected, values, nil
}

var scpGitRe = regexp.MustCompile(`^([^@]+@)?([^:]+):(.+)$`)

func normalizeGitURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("Git remote URL 为空")
	}
	if m := scpGitRe.FindStringSubmatch(raw); m != nil && !strings.Contains(raw, "://") {
		raw = "https://" + m[2] + "/" + m[3]
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	switch strings.ToLower(u.Scheme) {
	case "ssh", "git":
		u.Scheme = "https"
	case "http", "https":
	default:
		return "", fmt.Errorf("不支持的 Git remote scheme: %s", u.Scheme)
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	if u.Scheme == "https" && (u.Port() == "22" || u.Port() == "29418") {
		u.Host = u.Hostname()
	}
	u.Path = strings.TrimSuffix(u.Path, ".git")
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func repoHTTPURL(repo string) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", abs, "remote", "get-url", "origin").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("读取 git origin 失败: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return normalizeGitURL(strings.TrimSpace(string(out)))
}

func required(name, v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("缺少必填参数 --%s", name)
	}
	return nil
}

func scalarString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	default:
		return fmt.Sprint(v)
	}
}
