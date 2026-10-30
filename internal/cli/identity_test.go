package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestExperienceSearchIdentity(t *testing.T) {
	valid := &Session{Username: " employee001 ", Cookie: "sid=test"}
	expired := &Session{Username: "old-user", Cookie: "sid=old", ExpiresAt: time.Now().Add(-time.Hour).Format(time.RFC3339)}
	for _, tc := range []struct {
		name    string
		session *Session
		args    []string
		user    string
		caller  string
		wantErr bool
	}{
		{"logged in", valid, nil, "", "employee001", false},
		{"target user", valid, []string{"--user_id", "target002"}, "target002", "employee001", false},
		{"caller override", valid, []string{"--caller_id", " caller003 "}, "", "caller003", false},
		{"both overrides", valid, []string{"--user_id", " target002 ", "--caller_id", "caller003"}, "target002", "caller003", false},
		{"explicit without login", nil, []string{"--caller_id", "caller003"}, "", "caller003", false},
		{"no login", nil, nil, "", "", true},
		{"target is not caller", nil, []string{"--user_id", "target002"}, "", "", true},
		{"expired login", expired, nil, "", "", true},
		{"missing username", &Session{Cookie: "sid=test"}, nil, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body map[string]interface{}
			var decodeErr error
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				decodeErr = json.NewDecoder(r.Body).Decode(&body)
				_, _ = w.Write([]byte(`{"code":200,"data":[]}`))
			}))
			defer srv.Close()
			rt := &Runtime{ChatServer: srv.URL, Timeout: 5 * time.Second, Session: tc.session}
			err := runExperienceSearch(rt, append([]string{"--query", "test"}, tc.args...))
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "--caller_id") || calls != 0 {
					t.Fatalf("err=%v calls=%d", err, calls)
				}
				return
			}
			if err != nil || decodeErr != nil || calls != 1 || body["user_id"] != tc.user || body["caller_id"] != tc.caller {
				t.Fatalf("err=%v decode=%v calls=%d body=%v", err, decodeErr, calls, body)
			}
		})
	}
}

func TestLoginIdentityPersistsAcrossRuntime(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case devUCAuthorizePath:
			_, _ = w.Write([]byte(`https://login.example.com/auth?id=1`))
		case devUCTokenPath:
			_, _ = w.Write([]byte(`{"code":200,"data":"persist-token"}`))
		default:
			t.Errorf("unexpected path=%s", r.URL.Path)
		}
	}))
	defer srv.Close()

	oldInterval := pollInterval
	oldBrowser := openBrowserFn
	pollInterval = 5 * time.Millisecond
	openBrowserFn = func(string) error { return nil }
	defer func() {
		pollInterval = oldInterval
		openBrowserFn = oldBrowser
	}()

	rt := &Runtime{AuthServer: srv.URL, MarketServer: srv.URL, Timeout: 5 * time.Second}
	if err := runAuthLogin(rt, []string{}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := defaultRuntime()
	if err != nil {
		t.Fatal(err)
	}
	// devUC 浏览器登录不回传用户名，无显式 override 时 userID 应失败。
	if _, err := reloaded.userID(""); err == nil {
		t.Fatal("userID without override should fail (devUC login does not persist username)")
	}
	// 显式 override 仍可工作。
	if got, err := reloaded.userID("other002"); err != nil || got != "other002" {
		t.Fatalf("override=%q err=%v", got, err)
	}
	// token 已持久化，业务鉴权用 X-Auth-Token。
	if reloaded.Session == nil || reloaded.Session.Token != "persist-token" {
		t.Fatalf("token not persisted: %v", reloaded.Session)
	}
	// token 永久有效，不设过期时间。
	if reloaded.Session.ExpiresAt != "" {
		t.Fatalf("ExpiresAt should be empty for permanent token, got %q", reloaded.Session.ExpiresAt)
	}
}
