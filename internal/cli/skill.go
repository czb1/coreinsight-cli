package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func runSkill(rt *Runtime, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		skillHelp()
		return nil
	}
	switch args[0] {
	case "scenes":
		return runSkillScenes(rt, args[1:])
	case "search", "search-smart":
		return runSkillSmart(rt, args[1:])
	case "scene-search":
		return runSkillSceneSearch(rt, args[1:])
	case "download":
		return runSkillDownload(rt, args[1:])
	case "upload":
		return runSkillUpload(rt, args[1:], false)
	case "verify":
		return runSkillUpload(rt, args[1:], true)
	default:
		return fmt.Errorf("未知 skill 子命令: %s", args[0])
	}
}

func runSkillScenes(rt *Runtime, args []string) error {
	fs := newFS("skill scenes")
	var repo string
	fs.StringVar(&repo, "repo", "", "代码仓目录")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("repo", repo); err != nil {
		return err
	}
	gitURL, err := repoHTTPURL(repo)
	if err != nil {
		return err
	}

	offerings := []map[string]interface{}{}
	for page := 1; ; page++ {
		body := map[string]interface{}{"page": page, "pageSize": 20, "http_url": gitURL}
		resp, payload, err := rt.doJSON("POST", rt.CoreHarnessServer+"/core-harness/api/v1/offering/list", body, false)
		if err != nil {
			return emitReqErr(err)
		}
		if err := backendOK(resp, payload); err != nil {
			return emitBackendErr(err, payload)
		}
		raw, ok := dataField(payload).([]interface{})
		if !ok {
			return fmt.Errorf("产品接口 data 不是数组")
		}
		for _, item := range raw {
			if m, ok := item.(map[string]interface{}); ok {
				offerings = append(offerings, m)
			}
		}
		if len(raw) < 20 {
			break
		}
	}

	result := make([]map[string]interface{}, 0, len(offerings))
	for _, offering := range offerings {
		code := scalarString(offering["offering_id"])
		name, _ := offering["offering_cn_name"].(string)
		resp, payload, err := rt.doJSON("POST", rt.ChatServer+"/experience/harness/scenes", map[string]interface{}{"dimCode": code}, true)
		if err != nil {
			return emitReqErr(err)
		}
		if err := backendOK(resp, payload); err != nil {
			return emitBackendErr(err, payload)
		}
		result = append(result, map[string]interface{}{
			"offering_id": code, "product": name, "scenes": dataField(payload),
		})
	}
	success(requestID(), map[string]interface{}{"repo": repo, "http_url": gitURL, "products": result})
	return nil
}

func runSkillSceneSearch(rt *Runtime, args []string) error {
	fs := newFS("skill scene-search")
	var query, product, first, second, sortBy, sortOrder string
	var page, pageSize int
	fs.StringVar(&query, "query", "", "检索关键字，优先于产品/场景参数")
	fs.StringVar(&product, "product", "", "产品名称")
	fs.StringVar(&first, "first_scene", "", "一级场景")
	fs.StringVar(&second, "second_scene", "", "二级场景")
	fs.IntVar(&page, "page", 1, "页码")
	fs.IntVar(&pageSize, "page_size", 6, "每页数量")
	fs.StringVar(&sortBy, "sort_by", "downloads", "排序字段")
	fs.StringVar(&sortOrder, "sort_order", "desc", "排序方向")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(query) == "" {
		// Preserve legacy scene arguments as smart-search keywords.
		var keywords []string
		for _, value := range []string{product, first, second} {
			if value = strings.TrimSpace(value); value != "" {
				keywords = append(keywords, value)
			}
		}
		query = strings.Join(keywords, " ")
	}
	return runSkillSmart(rt, []string{
		"--query", query,
		"--page", fmt.Sprint(page),
		"--page_size", fmt.Sprint(pageSize),
		"--sort_by", sortBy,
		"--sort_order", sortOrder,
	})
}

func runSkillSmart(rt *Runtime, args []string) error {
	fs := newFS("skill search")
	var query, sortBy, sortOrder string
	var page, pageSize int
	fs.StringVar(&query, "query", "", "检索关键字")
	fs.IntVar(&page, "page", 1, "页码")
	fs.IntVar(&pageSize, "page_size", 6, "每页数量")
	fs.StringVar(&sortBy, "sort_by", "downloads", "排序字段")
	fs.StringVar(&sortOrder, "sort_order", "desc", "排序方向")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("query", query); err != nil {
		return err
	}
	if page <= 0 || pageSize <= 0 {
		return fmt.Errorf("--page / --page_size 必须为正整数")
	}
	q := url.Values{}
	q.Set("pageNum", fmt.Sprint(page))
	q.Set("pageSize", fmt.Sprint(pageSize))
	q.Set("sortBy", sortBy)
	q.Set("sortOrder", sortOrder)
	q.Set("keyword", query)
	q.Set("searchMode", "smart")
	resp, payload, err := rt.doJSON("GET", rt.AICommunityServer+"/aiapp-v2/api/skills?"+q.Encode(), nil, false)
	if err != nil {
		return emitReqErr(err)
	}
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	success(requestID(), payload)
	return nil
}

func runSkillDownload(rt *Runtime, args []string) error {
	fs := newFS("skill download")
	var skillID, version, output, userID string
	fs.StringVar(&skillID, "skill_id", "", "Skill ID")
	fs.StringVar(&version, "version", "", "Skill 版本")
	fs.StringVar(&output, "output", "./skills", "下载目录")
	fs.StringVar(&userID, "user_id", "", "用户 ID，默认使用登录用户名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("skill_id", skillID); err != nil {
		return err
	}
	uid, err := rt.userID(userID)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("userId", uid)
	if version != "" {
		q.Set("version", version)
	}
	endpoint := rt.AICommunityServer + "/aiapp-v2/api/skills/" + url.PathEscape(skillID) + "/download?" + q.Encode()
	resp, payload, err := rt.doJSON("POST", endpoint, nil, false)
	if err != nil {
		return emitReqErr(err)
	}
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	dl, ok := dataField(payload).(string)
	if !ok || strings.TrimSpace(dl) == "" {
		return fmt.Errorf("下载接口未返回 data URL")
	}

	req, err := http.NewRequest(http.MethodGet, dl, nil)
	if err != nil {
		return err
	}
	r, err := rt.httpClient().Do(req)
	if err != nil {
		return emitReqErr(err)
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return emitBackendErr(fmt.Errorf("下载失败: HTTP %s", r.Status), nil)
	}
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	name := downloadFileName(dl, r, skillID, version)
	path := filepath.Join(output, name)
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(f, r.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	abs, _ := filepath.Abs(path)
	success(requestID(), map[string]interface{}{"skill_id": skillID, "version": nullable(version), "path": abs, "bytes": n, "download_url": dl})
	return nil
}

func downloadFileName(rawURL string, resp *http.Response, skillID, version string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if v := filepath.Base(u.Query().Get("fileName")); v != "." && v != "" {
			return v
		}
		if v := filepath.Base(u.Path); strings.HasSuffix(strings.ToLower(v), ".zip") {
			return v
		}
	}
	base := skillID
	if version != "" {
		base += "-" + version
	}
	return base + ".zip"
}

func runSkillUpload(rt *Runtime, args []string, parse bool) error {
	fs := newFS("skill upload")
	var file, business, userID string
	fs.StringVar(&file, "file", "", "Skill ZIP")
	fs.StringVar(&business, "business_dimension", "", "业务维度")
	fs.StringVar(&userID, "user_id", "", "用户 ID，默认使用登录用户名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("file", file); err != nil {
		return err
	}
	uid, err := rt.userID(userID)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("userId", uid)
	if business != "" {
		q.Set("businessDimension", business)
	}
	path := "/aiapp-v2/api/skills/upload"
	if parse {
		path += "/parse"
	}
	resp, payload, err := rt.doMultipart(rt.AICommunityServer+path, q, "file", file, false)
	if err != nil {
		return emitReqErr(err)
	}
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	success(requestID(), payload)
	return nil
}

func skillHelp() {
	plainHelp(`用法:
  coreinsight-cli skill scenes --repo .
  coreinsight-cli skill search --query <关键字> [--page 1] [--page_size 6] [--sort_by downloads] [--sort_order desc]
  coreinsight-cli skill search-smart --query <关键字> [兼容别名]
  coreinsight-cli skill scene-search --query <关键字> [--page 1] [--page_size 6] [--sort_by downloads] [--sort_order desc]
  coreinsight-cli skill scene-search --product <产品> --first_scene <一级场景> --second_scene <二级场景>
  coreinsight-cli skill download --skill_id <id> [--version <version>] [--output ./skills]
  coreinsight-cli skill upload --file <skill.zip> [--business_dimension <维度>]
  coreinsight-cli skill verify --file <skill.zip> [--business_dimension <维度>]
`)
}
