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
		_, _ = w.Write([]byte(`{"code":200,"data":[{"skill_id":"sk1","skill_name":"MML生成"}]}`))
	}))
	defer srv.Close()
	rt := &Runtime{ChatServer: srv.URL + "/chat", Timeout: 5 * time.Second, Session: &Session{Username: "tester", Cookie: "sid=test"}}

	err := runSkillSceneSkills(rt, []string{
		"--first_scene", "研发",
		"--second_scene", "代码开发",
		"--dim_type", "product",
		"--dim_name", "测试产品",
	})
	if err != nil {
		t.Fatal(err)
	}
	if decodeErr != nil || method != "POST" || path != "/chat/experience/harness/scene/skills" || cookie != "sid=test" {
		t.Fatalf("request=%s %s cookie=%q decode=%v", method, path, cookie, decodeErr)
	}
	want := map[string]interface{}{
		"firstScene":  "研发",
		"secondScene": "代码开发",
		"dimType":     "product",
		"dimName":     "测试产品",
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("body=%#v want=%#v", body, want)
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
