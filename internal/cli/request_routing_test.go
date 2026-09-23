package cli

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReportedQARoutes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		retrieve bool
		path     string
		body     map[string]interface{}
	}{
		{"kb", []string{"--kb_sns", "123,456"}, false, "/chat/app/support/app/chat", map[string]interface{}{"user_id": "tester", "content": "问题", "stream_flag": false, "kb_sns": []interface{}{"123", "456"}}},
		{"products", []string{"--products", "UPCF,UDG"}, true, "/chat/app/support/app/retrieve", map[string]interface{}{"user_id": "tester", "question": "问题", "products": []interface{}{"UPCF", "UDG"}}},
		{"department", []string{"--departments", "分组核心网产品部", "--prompt_template", "SDD"}, false, "/chat/app/support/app/chat", map[string]interface{}{"user_id": "tester", "content": "问题", "stream_flag": false, "departments": []interface{}{"分组核心网产品部"}, "prompt_template": "SDD"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var method, path, cookie string
			var body map[string]interface{}
			var decodeErr error
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method, path, cookie = r.Method, r.URL.Path, r.Header.Get("Cookie")
				decodeErr = json.NewDecoder(r.Body).Decode(&body)
				_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
			}))
			defer srv.Close()
			t.Setenv("COREINSIGHT_SERVER", srv.URL+"/")
			t.Setenv("COREINSIGHT_CHAT_SERVER", "")
			t.Setenv("COREINSIGHT_TIMEOUT", "5")
			rt, err := defaultRuntime()
			if err != nil {
				t.Fatal(err)
			}
			rt.Session = &Session{Username: "tester", Cookie: "sid=test"}
			if err := runQA(rt, append([]string{"--query", "问题"}, tc.args...), tc.retrieve); err != nil {
				t.Fatal(err)
			}
			if decodeErr != nil || method != "POST" || path != tc.path || cookie != "sid=test" || !reflect.DeepEqual(body, tc.body) {
				t.Fatalf("request=%s %s cookie=%q body=%#v decode=%v", method, path, cookie, body, decodeErr)
			}
		})
	}
}

func TestExperienceUploadUsesMemoryServer(t *testing.T) {
	var method, path string
	var body map[string]interface{}
	var decodeErr error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		decodeErr = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"code":200,"data":{"version":2,"is_overwrite":true}}`))
	}))
	defer srv.Close()
	rt := &Runtime{CoreInsightServer: "http://unused.invalid", MemoryServer: srv.URL + "/gateway/", Timeout: 5 * time.Second, Session: &Session{Username: "tester"}}
	err := runExperienceUpload(rt, []string{"--scene", "test", "--scene_id", "scene-001", "--title", "标题", "--summary", "摘要", "--experience", "内容", "--doc_id", "existing", "--product_id", "PID-123"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"scene": "test", "scene_id": "scene-001", "title": "标题", "summary": "摘要", "experience": "内容", "user_id": "tester", "doc_id": "existing", "product": map[string]interface{}{"product_id": "PID-123"}}
	if decodeErr != nil || method != "POST" || path != "/gateway/memory/experience/doc" || !reflect.DeepEqual(body, want) {
		t.Fatalf("request=%s %s body=%#v decode=%v", method, path, body, decodeErr)
	}
}

func TestAPIRedirectDoesNotRewritePOST(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			var finalMethod string
			var finalBody map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path == "/start" {
					http.Redirect(w, r, "/final", status)
					return
				}
				finalMethod = r.Method
				_ = json.NewDecoder(r.Body).Decode(&finalBody)
				_, _ = w.Write([]byte(`{"code":200}`))
			}))
			defer srv.Close()
			rt := &Runtime{Timeout: 5 * time.Second}
			resp, payload, err := rt.doJSON("POST", srv.URL+"/start", map[string]string{"title": "keep"}, false)
			if err != nil {
				t.Fatal(err)
			}
			if status == 307 || status == 308 {
				if calls != 2 || finalMethod != "POST" || finalBody["title"] != "keep" || backendOK(resp, payload) != nil {
					t.Fatalf("redirect lost POST: calls=%d method=%s body=%v", calls, finalMethod, finalBody)
				}
			} else if calls != 1 || resp.StatusCode != status || backendOK(resp, payload) == nil {
				t.Fatalf("unexpected method-changing redirect: calls=%d status=%d", calls, resp.StatusCode)
			}
		})
	}
}

func TestMemoryServerConfiguration(t *testing.T) {
	t.Setenv("COREINSIGHT_SERVER", "https://core.example/")
	t.Setenv("COREINSIGHT_MEMORY_SERVER", "https://memory.example/gateway/")
	t.Setenv("COREINSIGHT_CHAT_SERVER", "https://chat.example/")
	t.Setenv("COREINSIGHT_TIMEOUT", "5")
	rt, err := defaultRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if rt.memoryEndpoint() != "https://memory.example/gateway/memory/experience/doc" {
		t.Fatal(rt.memoryEndpoint())
	}
	_, err = parseGlobals(rt, []string{"--memory-server", "https://override.example/api/", "--server", "https://new-core.example"})
	if err != nil {
		t.Fatal(err)
	}
	if rt.memoryEndpoint() != "https://override.example/api/memory/experience/doc" || rt.ChatServer != "https://chat.example" {
		t.Fatalf("runtime=%#v", rt)
	}
	rt.MemoryServer = ""
	if rt.memoryEndpoint() != "https://new-core.example/memory/experience/doc" {
		t.Fatal(rt.memoryEndpoint())
	}
}

func TestRequestDiagnostics(t *testing.T) {
	u, _ := url.Parse("https://user:secret@example.com/memory/experience/doc?token=secret")
	req := &http.Request{Method: "POST", URL: u}
	resp := &http.Response{StatusCode: 405, Status: "405 Method Not Allowed", Request: req, Header: http.Header{"Allow": []string{"GET"}}}
	msg := httpStatusError(resp).Error()
	if !strings.Contains(msg, "POST https://example.com/memory/experience/doc") || !strings.Contains(msg, "Allow=GET") || strings.Contains(msg, "secret") {
		t.Fatal(msg)
	}
	err := requestError(req, &net.DNSError{Err: "no such host", Name: "example.com"})
	if !strings.Contains(err.Error(), "DNS") {
		t.Fatal(err)
	}
}
