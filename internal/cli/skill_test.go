package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestSkillOfferingByRepo(t *testing.T) {
	var method, path, cookie string
	var body map[string]interface{}
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, cookie = r.Method, r.URL.Path, r.Header.Get("Cookie")
		decodeErr = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":200,"data":[{"offering_id":"P001","offering_cn_name":"产品A"}]}`))
	}))
	defer srv.Close()
	t.Setenv("COREINSIGHT_CORE_HARNESS_SERVER", srv.URL)
	rt := &Runtime{CoreHarnessServer: srv.URL, Timeout: 5 * time.Second, Session: &Session{Username: "tester", Cookie: "sid=test"}}

	// 用 --http_url 直接指定，避免依赖 git origin。
	err := runSkillOffering(rt, []string{"--http_url", "https://codehub-y.huawei.com/CSP/CSPCertSDK_C"})
	if err != nil {
		t.Fatal(err)
	}
	// 产品列表请求不携带会话 Cookie。
	if decodeErr != nil || method != "POST" || path != "/core-harness/api/v1/offering/list" || cookie != "" {
		t.Fatalf("request=%s %s cookie=%q decode=%v", method, path, cookie, decodeErr)
	}
	want := map[string]interface{}{"page": float64(1), "pageSize": float64(20), "http_url": "https://codehub-y.huawei.com/CSP/CSPCertSDK_C"}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body=%#v want=%#v", body, want)
	}
}

func TestSkillOfferingMissingArgs(t *testing.T) {
	rt := &Runtime{Timeout: 5 * time.Second}
	if err := runSkillOffering(rt, nil); err == nil {
		t.Fatal("expected error when neither --repo nor --http_url provided")
	}
}

func TestSkillProductScenes(t *testing.T) {
	var method, path, cookie string
	var body map[string]interface{}
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, cookie = r.Method, r.URL.Path, r.Header.Get("Cookie")
		decodeErr = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":200,"data":[{"scene_id":"s1","scene_name":"需求开发"}]}`))
	}))
	defer srv.Close()
	rt := &Runtime{ChatServer: srv.URL + "/chat", Timeout: 5 * time.Second, Session: &Session{Username: "tester", Cookie: "sid=test"}}

	err := runSkillProductScenes(rt, []string{"--dim_code", "P001"})
	if err != nil {
		t.Fatal(err)
	}
	if decodeErr != nil || method != "POST" || path != "/chat/experience/harness/scenes" || cookie != "sid=test" {
		t.Fatalf("request=%s %s cookie=%q decode=%v", method, path, cookie, decodeErr)
	}
	want := map[string]interface{}{"dimCode": "P001"}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body=%#v want=%#v", body, want)
	}
}

func TestSkillProductScenesMissingDimCode(t *testing.T) {
	rt := &Runtime{Timeout: 5 * time.Second}
	if err := runSkillProductScenes(rt, nil); err == nil {
		t.Fatal("expected error when --dim_code missing")
	}
}

func TestSkillSceneSkills(t *testing.T) {
	var method, path, cookie string
	var body map[string]interface{}
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, cookie = r.Method, r.URL.Path, r.Header.Get("Cookie")
		decodeErr = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":200,"data":[
			{"skillName":"ps-ci-package-build","skillDescription":"基于pipelineX构建与出包","versions":[
				{"version":"0.2.1","uploadDate":"2026-10-10 17:32:24","downloadUrl":"https://example.test/0.2.1.zip"},
				{"version":"0.2.2","uploadDate":"2026-10-12 14:25:21","downloadUrl":"https://example.test/0.2.2.zip","checksum":"kept"},
				{"version":"0.2.0","uploadDate":"2026-10-10 11:51:39","downloadUrl":"https://example.test/0.2.0.zip"}
			]},
			{"skillName":"mml-generator","skillDescription":"MML生成","versions":[
				{"version":"1.0.0","uploadDate":"2026-10-10 12:00:00","downloadUrl":"https://example.test/mml-1.0.0.zip"},
				{"version":"1.1.0","uploadDate":"2026-10-11 12:00:00","downloadUrl":"https://example.test/mml-1.1.0.zip"}
			]}
		]}`))
	}))
	defer srv.Close()
	rt := &Runtime{ChatServer: srv.URL + "/chat", Timeout: 5 * time.Second, Session: &Session{Username: "tester", Cookie: "sid=test"}}

	var err error
	output := captureAuthStdout(t, func() {
		err = runSkill(rt, []string{
			"scene-skills",
			"--first_scene", "需求开发",
			"--second_scene", "MML开发",
			"--dim_type", "产品级",
			"--dim_name", "UPCF",
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if decodeErr != nil || method != "POST" || path != "/chat/experience/harness/scene/skills" || cookie != "sid=test" {
		t.Fatalf("request=%s %s cookie=%q decode=%v", method, path, cookie, decodeErr)
	}
	want := map[string]interface{}{
		"firstScene":  "需求开发",
		"secondScene": "MML开发",
		"dimType":     "产品级",
		"dimName":     "UPCF",
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body=%#v want=%#v", body, want)
	}
	var response rpcResponse
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatal(err)
	}
	if response.JSONRPC != "2.0" || response.Error != nil {
		t.Fatalf("unexpected JSON-RPC response: %#v", response)
	}
	var wantResult interface{}
	if err := json.Unmarshal([]byte(`{
		"first_scene":"需求开发","second_scene":"MML开发","dim_type":"产品级","dim_name":"UPCF",
		"skills":[
			{"skillName":"ps-ci-package-build","skillDescription":"基于pipelineX构建与出包","versions":[
				{"version":"0.2.2","uploadDate":"2026-10-12 14:25:21","downloadUrl":"https://example.test/0.2.2.zip","checksum":"kept"}
			]},
			{"skillName":"mml-generator","skillDescription":"MML生成","versions":[
				{"version":"1.1.0","uploadDate":"2026-10-11 12:00:00","downloadUrl":"https://example.test/mml-1.1.0.zip"}
			]}
		]
	}`), &wantResult); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(response.Result, wantResult) {
		t.Fatalf("result=%#v want=%#v", response.Result, wantResult)
	}
}

func TestSkillSceneSkillsMissingRequired(t *testing.T) {
	rt := &Runtime{Timeout: 5 * time.Second}
	// 缺少 dim_name。
	if err := runSkillSceneSkills(rt, []string{
		"--first_scene", "研发",
		"--second_scene", "代码开发",
		"--dim_type", "product",
	}); err == nil {
		t.Fatal("expected error when --dim_name missing")
	}
}

func TestSkillSceneSearchRemoved(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("removed command sent a request: %s %s", r.Method, r.URL)
		_, _ = w.Write([]byte(`{"code":200,"data":[]}`))
	}))
	defer srv.Close()
	rt := &Runtime{ChatServer: srv.URL + "/chat", AICommunityServer: srv.URL, Timeout: 5 * time.Second}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"query", []string{"--query", "数据库"}},
		{"scene", []string{"--product", "UPCF", "--first_scene", "需求开发", "--second_scene", "MML开发"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := runSkill(rt, append([]string{"scene-search"}, tc.args...))
			if err == nil || err.Error() != "未知 skill 子命令: scene-search" {
				t.Fatalf("removed command should be rejected, got %v", err)
			}
		})
	}
}
