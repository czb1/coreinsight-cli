package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBuildLoginBodyFromEnv(t *testing.T) {
	t.Setenv("COREINSIGHT_AUTH_USERNAME", "alice")
	t.Setenv("COREINSIGHT_AUTH_PASSWORD", "secret")
	body, username, err := buildLoginBody(&loginOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if username != "alice" {
		t.Fatalf("username=%q", username)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["userName"] != "alice" || got["passwd"] != "secret" {
		t.Fatalf("unexpected body: %#v", got)
	}
}

func TestBuildLoginBodyRawJSONWins(t *testing.T) {
	o := &loginOpts{username: "flag-user", password: "flag-pass", body: `{"userName":"raw-user","passwd":"raw-pass"}`}
	body, username, err := buildLoginBody(o)
	if err != nil {
		t.Fatal(err)
	}
	if username != "raw-user" || string(body) != o.body {
		t.Fatalf("username=%q body=%s", username, body)
	}
}

func TestExtractCookieWithExpiry(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Add("Set-Cookie", "sid=abc123; Max-Age=60; Path=/")
	cookie, expires := extractCookieWithExpiry(resp)
	if cookie != "sid=abc123" {
		t.Fatalf("cookie=%q", cookie)
	}
	if expires.Before(time.Now().Add(50 * time.Second)) {
		t.Fatalf("unexpected expiry: %v", expires)
	}
}


func TestReferenceAuthServerDefault(t *testing.T) {
	if defaultAuthServer != "https://omtool.rnd.huawei.com" {
		t.Fatalf("defaultAuthServer=%q", defaultAuthServer)
	}
}

func TestLoginRequestMatchesReferenceFlow(t *testing.T) {
	var gotCookie string
	var gotMethod string
	var gotPath string
	var gotBody map[string]string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Add("Set-Cookie", "JSESSIONID=abc123; Path=/")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
	}))
	defer srv.Close()

	rt := &Runtime{
		AuthServer: srv.URL,
		Timeout:    5 * time.Second,
		Session:    &Session{Cookie: "JSESSIONID=old-session"},
	}
	body := []byte(`{"userName":"alice","passwd":"secret"}`)
	resp, payload, err := doLoginRequest(rt, body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if code, _, ok := businessCode(payload); !ok || code != 0 {
		t.Fatalf("unexpected payload: %#v", payload)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/auth/login" {
		t.Fatalf("request=%s %s", gotMethod, gotPath)
	}
	if gotCookie != "" {
		t.Fatalf("login request must not carry old cookie, got %q", gotCookie)
	}
	if gotBody["userName"] != "alice" || gotBody["passwd"] != "secret" {
		t.Fatalf("unexpected body: %#v", gotBody)
	}
}

func TestBuildLoginBodyFromOMRESEnv(t *testing.T) {
	t.Setenv("COREINSIGHT_AUTH_USERNAME", "")
	t.Setenv("COREINSIGHT_AUTH_PASSWORD", "")
	t.Setenv("OMRES_AUTH_USERNAME", "bob")
	t.Setenv("OMRES_AUTH_PASSWORD", "omres-secret")

	body, username, err := buildLoginBody(&loginOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if username != "bob" {
		t.Fatalf("username=%q", username)
	}
	var got map[string]string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["userName"] != "bob" || got["passwd"] != "omres-secret" {
		t.Fatalf("unexpected body: %#v", got)
	}
}
