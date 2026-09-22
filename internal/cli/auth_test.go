package cli

import (
	"encoding/json"
	"net/http"
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
