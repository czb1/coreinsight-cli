package cli

import (
	"flag"
	"fmt"
)

func runQA(rt *Runtime, args []string, retrieve bool) error {
	fs := flag.NewFlagSet("qa", flag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	var query, kb, products, departments, scenes, promptTemplate, userID string
	fs.StringVar(&query, "query", "", "原始问题")
	fs.StringVar(&kb, "kb_sns", "", "知识库 ID，逗号分隔")
	fs.StringVar(&products, "products", "", "产品名称，逗号分隔")
	fs.StringVar(&departments, "departments", "", "部门名称，逗号分隔")
	fs.StringVar(&scenes, "scenes", "", "场景名称，逗号分隔")
	fs.StringVar(&promptTemplate, "prompt_template", "", "prompt 模板名称，仅 qa 使用")
	fs.StringVar(&userID, "user_id", "", "用户 ID，默认使用登录用户名")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := required("query", query); err != nil {
		return err
	}
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

func qaHelp() {
	plainHelp(`用法:
  coreinsight-cli qa --query <问题> (--kb_sns <ids> | --products <names> | --departments <names> | --scenes <names>) [--prompt_template <name>]
  coreinsight-cli retrieve --query <问题> (--kb_sns <ids> | --products <names> | --departments <names> | --scenes <names>)

四类路由参数必须且只能选择一个，多个值使用逗号分隔。
`)
}

var _ = fmt.Sprintf
