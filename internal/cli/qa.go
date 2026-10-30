package cli

import (
	"flag"
	"fmt"
)

func runRAG(rt *Runtime, args []string) error {
	fs := flag.NewFlagSet("rag", flag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	var query, kb, products, departments, scenes, promptTemplate, userID string
	var qaMode, retrieveMode bool
	fs.StringVar(&query, "query", "", "原始问题")
	fs.StringVar(&kb, "kb_sns", "", "知识库 ID，逗号分隔")
	fs.StringVar(&products, "products", "", "产品名称，逗号分隔")
	fs.StringVar(&departments, "departments", "", "部门名称，逗号分隔")
	fs.StringVar(&scenes, "scenes", "", "场景名称，逗号分隔")
	fs.StringVar(&promptTemplate, "prompt_template", "", "prompt 模板名称，仅问答模式使用")
	fs.StringVar(&userID, "user_id", "", "用户 ID（浏览器登录不保存工号，需显式指定）")
	fs.BoolVar(&qaMode, "qa", false, "问答模式（与 --retrieve 二选一，必选其一）")
	fs.BoolVar(&retrieveMode, "retrieve", false, "纯检索模式（与 --qa 二选一，必选其一）")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("query", query); err != nil {
		return err
	}
	if qaMode && retrieveMode {
		return fmt.Errorf("--qa 与 --retrieve 不能同时使用")
	}
	if !qaMode && !retrieveMode {
		return fmt.Errorf("请指定模式：--qa（问答）或 --retrieve（纯检索）")
	}
	retrieve := retrieveMode
	key, vals, err := routePayload(kb, products, departments, scenes)
	if err != nil {
		return err
	}
	uid, err := rt.userID(userID)
	if err != nil {
		return err
	}
	body := map[string]interface{}{"user_id": uid, key: vals}
	path := "/app/support/app/chat"
	if retrieve {
		body["question"] = query
		path = "/app/support/app/retrieve"
	} else {
		body["content"] = query
		body["stream_flag"] = false
		if promptTemplate != "" {
			body["prompt_template"] = promptTemplate
		}
	}
	id := requestID()
	resp, payload, err := rt.doJSON("POST", rt.ChatServer+path, body, true)
	if err != nil {
		failure(id, codeInternalError, "Request Failed", err.Error())
		return errHandled
	}
	if err := backendOK(resp, payload); err != nil {
		failure(id, codeBackendError, err.Error(), payload)
		return errHandled
	}
	success(id, payload)
	return nil
}

func ragHelp() {
	plainHelp(`用法:
  coreinsight-cli rag --qa|--retrieve --query <问题> (--kb_sns <ids> | --products <names> | --departments <names> | --scenes <names>) [--prompt_template <name>]

（需先执行 coreinsight-cli auth login 建立会话）

模式（必选其一）:
  --qa        问答模式，请求 /app/support/app/chat
  --retrieve  纯检索模式，请求 /app/support/app/retrieve
              --qa 与 --retrieve 互斥，同时指定报错

四类路由参数必须且只能选择一个，多个值使用逗号分隔。
`)
}
