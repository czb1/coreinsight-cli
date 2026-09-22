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
	var query, field, caller, scene, sceneID, docID, userID string
	var topK, page, pageSize int
	var vectorWeight, bm25Weight, threshold float64
	var qualityOnly bool
	fs.StringVar(&query, "query", "", "查询文本")
	fs.StringVar(&field, "search_field", "", "title / summary / experience / rag_search_text")
	fs.StringVar(&caller, "caller_id", "", "调用方工号")
	fs.StringVar(&scene, "scene", "", "场景名称，多个逗号分隔")
	fs.StringVar(&sceneID, "scene_id", "", "场景 ID，多个逗号分隔")
	fs.IntVar(&topK, "top_k", 10, "候选数量")
	fs.IntVar(&page, "page", 1, "页码")
	fs.IntVar(&pageSize, "page_size", 10, "每页数量")
	fs.Float64Var(&vectorWeight, "vector_weight", 0.7, "向量权重")
	fs.Float64Var(&bm25Weight, "bm25_weight", 0.3, "BM25 权重")
	fs.Float64Var(&threshold, "score_threshold", 0, "分数阈值")
	fs.BoolVar(&qualityOnly, "quality_only", false, "仅检索质量通过的经验")
	fs.StringVar(&docID, "doc_id", "", "文档 ID")
	fs.StringVar(&userID, "user_id", "", "用户 ID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for n, v := range map[string]string{"query": query, "search_field": field, "caller_id": caller} {
		if err := required(n, v); err != nil {
			return err
		}
	}
	valid := map[string]bool{"title": true, "summary": true, "experience": true, "rag_search_text": true}
	if !valid[field] {
		return fmt.Errorf("--search_field 必须是 title / summary / experience / rag_search_text")
	}
	if topK <= 0 || page <= 0 || pageSize <= 0 {
		return fmt.Errorf("--top_k / --page / --page_size 必须为正整数")
	}
	if vectorWeight < 0 || bm25Weight < 0 || vectorWeight+bm25Weight == 0 {
		return fmt.Errorf("检索权重必须非负且不能同时为 0")
	}
	if flagWasSet(fs, "score_threshold") && (threshold < 0 || threshold > 1) {
		return fmt.Errorf("--score_threshold 必须在 0.0 到 1.0 之间")
	}
	body := map[string]interface{}{
		"query": query, "search_field": field, "caller_id": caller,
		"top_k": topK, "page": page, "page_size": pageSize,
		"weights":      map[string]float64{"vector": vectorWeight, "bm25": bm25Weight},
		"quality_only": qualityOnly,
	}
	if v := csvList(scene); len(v) > 0 {
		body["scene"] = scalarOrArray(v)
	}
	if v := csvList(sceneID); len(v) > 0 {
		body["scene_id"] = scalarOrArray(v)
	}
	if docID != "" {
		body["doc_id"] = docID
	}
	if userID != "" {
		body["user_id"] = userID
	}
	if flagWasSet(fs, "score_threshold") {
		body["score_threshold"] = threshold
	}
	resp, payload, err := rt.doJSON("POST", rt.CoreInsightServer+"/memory/experience/doc/search", body, true)
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
	resp, payload, err := rt.doJSON("POST", rt.CoreInsightServer+"/memory/experience/doc", body, true)
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
  coreinsight-cli experience search --query <文本> --search_field <字段> --caller_id <工号> [过滤/排序参数]
  coreinsight-cli experience upload --scene <场景> --scene_id <id> --title <标题> --summary <摘要> --experience <内容> [产品字段]
`)
}

var _ = strings.TrimSpace
