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
	t.Setenv("HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "sid=test; Path=/")
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	rt := &Runtime{AuthServer: srv.URL, Timeout: 5 * time.Second}
	if err := runAuthLogin(rt, []string{"--username", "employee001", "--password", "test"}); err != nil {
		t.Fatal(err)
	}
	reloaded, err := defaultRuntime()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := reloaded.userID(""); err != nil || got != "employee001" {
		t.Fatalf("user ID=%q err=%v", got, err)
	}
	if got, err := reloaded.userID("other002"); err != nil || got != "other002" {
		t.Fatalf("override=%q err=%v", got, err)
	}
}
