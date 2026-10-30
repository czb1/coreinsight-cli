package cli

import (
	"flag"
	"fmt"
	"strings"
)

func runExperience(rt *Runtime, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || hasHelp(args[1:]) {
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
	var query, caller, scene, source, userID, departments, products, searchField string
	var page, pageSize int
	fs.StringVar(&query, "query", "", "查询文本")
	fs.StringVar(&searchField, "search_field", "title", "检索字段，可选 title / rag_search_text，默认 title")
	fs.StringVar(&caller, "caller_id", "", "调用方工号（浏览器登录不保存工号，需显式指定）")
	fs.StringVar(&scene, "scene", "ALL", "场景名称")
	fs.StringVar(&source, "source", "web", "请求来源")
	fs.StringVar(&userID, "user_id", "", "按贡献者工号过滤的经验，未指定则检索所有用户的经验")
	fs.StringVar(&departments, "departments", "", "部门名称，逗号分隔")
	fs.StringVar(&products, "products", "", "产品名称，逗号分隔")
	fs.IntVar(&page, "page", 1, "页码")
	fs.IntVar(&pageSize, "page_size", 5, "每页数量")
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
	searchField = strings.TrimSpace(searchField)
	if searchField == "" {
		searchField = "title"
	}
	body := map[string]interface{}{
		"user_id":      strings.TrimSpace(userID),
		"caller_id":    caller,
		"page":         page,
		"page_size":    pageSize,
		"source":       source,
		"title":        query,
		"search_field": searchField,
		"scene":        scene,
	}
	if departments != "" {
		body["dept"] = scalarOrArray(csvList(departments))
	}
	if products != "" {
		body["products"] = scalarOrArray(csvList(products))
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
	fs.StringVar(&userID, "user_id", "", "用户 ID（浏览器登录不保存工号，需显式指定）")
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
  coreinsight-cli experience search --query <文本> [--search_field title|rag_search_text] [--user_id <工号>] [--caller_id <工号>] [--scene ALL] [--departments <部门,逗号分隔>] [--products <产品,逗号分隔>] [--page 1] [--page_size 5]
  coreinsight-cli experience upload --scene <场景> --scene_id <id> --title <标题> --summary <摘要> --experience <内容> [产品字段]

（需先执行 coreinsight-cli auth login 建立会话）

检索时 caller_id 需使用 --caller_id 显式指定（浏览器登录不保存工号）；user_id 指定时仅检索该贡献者的经验，未指定则检索所有用户的经验。
--search_field 指定检索字段，默认 title（按标题检索），可指定 rag_search_text（按补充检索文本检索）。
--departments 按部门过滤（后端映射 product.pdu_name），--products 按产品名称过滤（映射 product.product_name），
两者可同时叠加过滤，多个值使用逗号分隔。
上传的 user_id 需使用 --user_id 显式指定（浏览器登录不保存工号）。显式 ID 参数优先。
`)
}

var _ = strings.TrimSpace
