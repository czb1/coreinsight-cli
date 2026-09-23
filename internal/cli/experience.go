package cli

import (
	"flag"
	"fmt"
	"strings"
)

func runExperience(rt *Runtime, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		experienceHelp()
		return nil
	}
	switch args[0] {
	case "search":
		return runExperienceSearch(rt, args[1:])
	case "upload":
		return runExperienceUpload(rt, args[1:])
	default:
		return fmt.Errorf("未知 experience 子命令: %s", args[0])
	}
}

func runExperienceSearch(rt *Runtime, args []string) error {
	fs := newFS("experience search")
	var query, caller, scene, source, userID string
	var page, pageSize int
	var showPersonal bool
	fs.StringVar(&query, "query", "", "查询文本，作为 title 搜索")
	fs.StringVar(&caller, "caller_id", "", "调用方工号，默认使用登录工号")
	fs.StringVar(&scene, "scene", "ALL", "场景名称")
	fs.StringVar(&source, "source", "web", "请求来源")
	fs.StringVar(&userID, "user_id", "", "目标用户 ID，默认空，不使用登录工号")
	fs.IntVar(&page, "page", 1, "页码")
	fs.IntVar(&pageSize, "page_size", 5, "每页数量")
	fs.BoolVar(&showPersonal, "show_personal", false, "是否展示个人经验")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("query", query); err != nil {
		return err
	}
	if page <= 0 || pageSize <= 0 {
		return fmt.Errorf("--page / --page_size 必须为正整数")
	}
	caller, err := rt.identityID(caller, "caller_id")
	if err != nil {
		return err
	}
	scene = strings.TrimSpace(scene)
	if scene == "" {
		scene = "ALL"
	}
	source = strings.TrimSpace(source)
	if source == "" {
		source = "web"
	}
	body := map[string]interface{}{
		"user_id":       strings.TrimSpace(userID),
		"caller_id":     caller,
		"show_personal": showPersonal,
		"page":          page,
		"page_size":     pageSize,
		"source":        source,
		"title":         query,
		"scene":         scene,
	}
	resp, payload, err := rt.doJSON("POST", rt.ChatServer+"/experience/search", body, true)
	if err != nil {
		return emitReqErr(err)
	}
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	success(requestID(), payload)
	return nil
}

func runExperienceUpload(rt *Runtime, args []string) error {
	fs := newFS("experience upload")
	var scene, sceneID, title, summary, experience, docID, rag, productLine, pdu, productID, version, feature, userID string
	fs.StringVar(&scene, "scene", "", "场景名称")
	fs.StringVar(&sceneID, "scene_id", "", "场景 ID")
	fs.StringVar(&title, "title", "", "标题")
	fs.StringVar(&summary, "summary", "", "摘要")
	fs.StringVar(&experience, "experience", "", "经验内容")
	fs.StringVar(&docID, "doc_id", "", "文档 ID")
	fs.StringVar(&rag, "rag_search_text", "", "补充检索文本")
	fs.StringVar(&productLine, "product_line_name", "", "产品线名称")
	fs.StringVar(&pdu, "pdu_name", "", "PDU 名称")
	fs.StringVar(&productID, "product_id", "", "产品 ID")
	fs.StringVar(&version, "version_name", "", "版本")
	fs.StringVar(&feature, "feature", "", "特性")
	fs.StringVar(&userID, "user_id", "", "用户 ID，默认使用登录用户名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for n, v := range map[string]string{"scene": scene, "scene_id": sceneID, "title": title, "summary": summary, "experience": experience} {
		if err := required(n, v); err != nil {
			return err
		}
	}
	uid, err := rt.userID(userID)
	if err != nil {
		return err
	}
	body := map[string]interface{}{"scene": scene, "scene_id": sceneID, "title": title, "summary": summary, "experience": experience, "user_id": uid}
	if docID != "" {
		body["doc_id"] = docID
	}
	if rag != "" {
		body["rag_search_text"] = rag
	}
	product := map[string]interface{}{}
	if productLine != "" {
		product["product_line_name"] = productLine
	}
	if pdu != "" {
		product["pdu_name"] = pdu
	}
	if productID != "" {
		product["product_id"] = productID
	}
	if version != "" {
		product["version_name"] = version
	}
	if feature != "" {
		product["feature"] = feature
	}
	if len(product) > 0 {
		body["product"] = product
	}
	resp, payload, err := rt.doJSON("POST", strings.TrimRight(rt.ChatServer, "/")+"/experience/experience_add", body, true)
	if err != nil {
		return emitReqErr(err)
	}
	if err := backendOK(resp, payload); err != nil {
		return emitBackendErr(err, payload)
	}
	success(requestID(), payload)
	return nil
}

func scalarOrArray(v []string) interface{} {
	if len(v) == 1 {
		return v[0]
	}
	return v
}
func flagWasSet(fs *flag.FlagSet, name string) bool {
	found := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func experienceHelp() {
	plainHelp(`用法:
  coreinsight-cli experience search --query <文本> [--user_id <工号>] [--caller_id <工号>] [--scene ALL] [--page 1] [--page_size 5]
  coreinsight-cli experience upload --scene <场景> --scene_id <id> --title <标题> --summary <摘要> --experience <内容> [产品字段]

检索仅 caller_id 默认使用登录工号；user_id 未指定时传空字符串。
上传的 user_id 默认使用登录工号。显式 ID 参数优先。
`)
}

var _ = strings.TrimSpace
