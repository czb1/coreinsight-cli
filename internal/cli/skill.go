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
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || hasHelp(args[1:]) {
		skillHelp()
		return nil
	}
	switch args[0] {
	case "scenes":
		return runSkillScenes(rt, args[1:])
	case "offering":
		return runSkillOffering(rt, args[1:])
	case "product-scenes":
		return runSkillProductScenes(rt, args[1:])
	case "scene-skills":
		return runSkillSceneSkills(rt, args[1:])
	case "search":
		return runSkillSmart(rt, args[1:])
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

// runSkillOffering 查询代码仓归属的产品列表。
// 调用 Core Harness POST /core-harness/api/v1/offering/list，
// body: {page, pageSize=20, http_url}；不携带会话 Cookie。
// 仓库 URL 可由 --repo 解析 origin，或由 --http_url 直接指定。
func runSkillOffering(rt *Runtime, args []string) error {
	fs := newFS("skill offering")
	var repo, httpURL string
	fs.StringVar(&repo, "repo", "", "代码仓目录，用于解析 origin HTTP URL")
	fs.StringVar(&httpURL, "http_url", "", "代码仓 HTTP URL，优先于 --repo")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(httpURL) == "" {
		if err := required("repo", repo); err != nil {
			return err
		}
		u, err := repoHTTPURL(repo)
		if err != nil {
			return err
		}
		httpURL = u
	} else {
		u, err := normalizeGitURL(httpURL)
		if err != nil {
			return err
		}
		httpURL = u
	}

	var offerings []map[string]interface{}
	for page := 1; ; page++ {
		body := map[string]interface{}{"page": page, "pageSize": 20, "http_url": httpURL}
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
	success(requestID(), map[string]interface{}{"http_url": httpURL, "products": offerings})
	return nil
}

// runSkillProductScenes 按 dimCode 查询产品的场景列表。
// 调用 Chat POST /experience/harness/scenes，body: {dimCode}；携带会话 Cookie。
func runSkillProductScenes(rt *Runtime, args []string) error {
	fs := newFS("skill product-scenes")
	var dimCode string
	fs.StringVar(&dimCode, "dim_code", "", "维度编码（产品 offering_id）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("dim_code", dimCode); err != nil {
		return err
	}
	resp, payload, err := rt.doJSON("POST", rt.ChatServer+"/experience/harness/scenes", map[string]interface{}{"dimCode": dimCode}, true)
	if err != nil {
		return emitReqErr(err)
	}
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	success(requestID(), map[string]interface{}{"dim_code": dimCode, "scenes": dataField(payload)})
	return nil
}

// runSkillSceneSkills 查询某场景下的 skill 列表。
// 调用 Chat POST /experience/harness/scene/skills，
// body: {firstScene, secondScene, dimType, dimName}；携带会话 Cookie。
func runSkillSceneSkills(rt *Runtime, args []string) error {
	fs := newFS("skill scene-skills")
	var firstScene, secondScene, dimType, dimName string
	fs.StringVar(&firstScene, "first_scene", "", "一级场景")
	fs.StringVar(&secondScene, "second_scene", "", "二级场景")
	fs.StringVar(&dimType, "dim_type", "", "维度类型")
	fs.StringVar(&dimName, "dim_name", "", "维度名称")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, p := range []struct{ name, val string }{
		{"first_scene", firstScene},
		{"second_scene", secondScene},
		{"dim_type", dimType},
		{"dim_name", dimName},
	} {
		if err := required(p.name, p.val); err != nil {
			return err
		}
	}
	body := map[string]interface{}{
		"firstScene":  firstScene,
		"secondScene": secondScene,
		"dimType":     dimType,
		"dimName":     dimName,
	}
	resp, payload, err := rt.doJSON("POST", rt.ChatServer+"/experience/harness/scene/skills", body, true)
	if err != nil {
		return emitReqErr(err)
	}
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	success(requestID(), map[string]interface{}{
		"first_scene":  firstScene,
		"second_scene": secondScene,
		"dim_type":     dimType,
		"dim_name":     dimName,
		"skills":       latestSceneSkillVersions(dataField(payload)),
	})
	return nil
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
	var skillID, version, output, caller string
	fs.StringVar(&skillID, "skill_id", "", "Skill ID")
	fs.StringVar(&version, "version", "", "Skill 版本")
	fs.StringVar(&output, "output", "./skills", "下载目录")
	fs.StringVar(&caller, "caller_id", "", "可选：校验登录调用者工号，不能覆盖会话身份")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("skill_id", skillID); err != nil {
		return err
	}
	uid, err := rt.callerID(caller)
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
	var file, business, caller string
	fs.StringVar(&file, "file", "", "Skill ZIP")
	fs.StringVar(&business, "business_dimension", "", "业务维度")
	fs.StringVar(&caller, "caller_id", "", "可选：校验登录调用者工号，不能覆盖会话身份")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("file", file); err != nil {
		return err
	}
	uid, err := rt.callerID(caller)
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
  coreinsight-cli skill offering --repo .
  coreinsight-cli skill offering --http_url <代码仓 HTTP URL>
  coreinsight-cli skill product-scenes --dim_code <产品 offering_id>
  coreinsight-cli skill scene-skills --first_scene <一级场景> --second_scene <二级场景> --dim_type <维度类型> --dim_name <维度名称>
  coreinsight-cli skill search --query <关键字> [--page 1] [--page_size 6] [--sort_by downloads] [--sort_order desc]
  coreinsight-cli skill download --skill_id <id> [--version <version>] [--output ./skills]
  coreinsight-cli skill upload --file <skill.zip> [--business_dimension <维度>]
  coreinsight-cli skill verify --file <skill.zip> [--business_dimension <维度>]

场景 Skill 查询使用 scene-skills，四个参数分别作为结构化字段发送到
    POST /experience/harness/scene/skills；产品级场景使用 --dim_type "产品级"。
每个场景 Skill 的 versions 仅保留最新一条：优先按 uploadDate，时间相同或均无有效时间时比较数字版本号。
下载/上传/校验自动从登录会话读取 caller_id，映射为市场接口既有 userId；不接受 --user_id。
可选 --caller_id 仅校验与会话一致，不能覆盖身份。
--version 可选；不传时由服务端选择版本，CLI 不硬编码版本或擅自补 latest。

注: skill offering 的产品列表请求不携带会话 Cookie，但仍需先执行
    coreinsight-cli auth login --caller_id <工号> 通过本地登录校验（与 skill scenes 一致）。
`)
}
