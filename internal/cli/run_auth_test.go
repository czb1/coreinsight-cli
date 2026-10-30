package cli

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func isolatedHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
}

func TestRequireAuthenticated(t *testing.T) {
	rt := &Runtime{}
	if !errors.Is(requireAuthenticated(rt, "rag"), errUnauthenticated) {
		t.Fatalf("nil session must be rejected")
	}
	rt.Session = &Session{Username: "tester", Cookie: "sid=test", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}
	if err := requireAuthenticated(rt, "rag"); err != nil {
		t.Fatalf("valid session must pass: %v", err)
	}
	rt.Session.ExpiresAt = time.Now().Add(-1 * time.Minute).Format(time.RFC3339)
	if !errors.Is(requireAuthenticated(rt, "rag"), errUnauthenticated) {
		t.Fatalf("expired session must be rejected")
	}
}

func TestRunBusinessCommandsRequireLogin(t *testing.T) {
	isolatedHome(t)
	t.Setenv("COREINSIGHT_SERVER", "http://unused.invalid")
	t.Setenv("COREINSIGHT_CHAT_SERVER", "")
	t.Setenv("COREINSIGHT_TIMEOUT", "5")

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"rag", []string{"rag", "--qa", "--query", "hi", "--kb_sns", "1"}, 3},
		{"rag help", []string{"rag", "--help"}, 0},
		{"skill search", []string{"skill", "search", "--query", "x"}, 3},
		{"skill scenes", []string{"skill", "scenes", "--repo", "."}, 3},
		{"skill download", []string{"skill", "download", "--skill_id", "s1"}, 3},
		{"skill upload", []string{"skill", "upload", "--file", "x.zip"}, 3},
		{"skill verify", []string{"skill", "verify", "--file", "x.zip"}, 3},
		{"skill help", []string{"skill", "--help"}, 0},
		{"skill download help", []string{"skill", "download", "--help"}, 0},
		{"skill search help", []string{"skill", "search", "-h"}, 0},
		{"experience upload help", []string{"experience", "upload", "--help"}, 0},
		{"experience search", []string{"experience", "search", "--query", "x"}, 3},
		{"experience upload", []string{"experience", "upload", "--scene", "s", "--scene_id", "1", "--title", "t", "--summary", "s", "--experience", "e"}, 3},
		{"experience help", []string{"experience", "--help"}, 0},
		{"version", []string{"version"}, 0},
		{"auth status", []string{"auth", "status"}, 3},
		{"unknown", []string{"no-such-command"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Run(tc.args); got != tc.want {
				t.Fatalf("Run(%v) = %d, want %d", tc.args, got, tc.want)
			}
		})
	}
}

func TestRunBusinessCommandWithSession(t *testing.T) {
	isolatedHome(t)
	t.Setenv("COREINSIGHT_CHAT_SERVER", "")
	t.Setenv("COREINSIGHT_TIMEOUT", "5")

	var gotCookie string
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCookie = r.Header.Get("Cookie")
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
	}))
	defer srv.Close()
	t.Setenv("COREINSIGHT_SERVER", srv.URL+"/")

	if err := saveSession(&Session{Username: "tester", Cookie: "sid=test", ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if got := Run([]string{"rag", "--qa", "--query", "hi", "--kb_sns", "1"}); got != 0 {
		t.Fatalf("Run(rag) = %d, want 0", got)
	}
	if gotPath != "/chat/app/support/app/chat" || gotCookie != "sid=test" {
		t.Fatalf("path=%q cookie=%q", gotPath, gotCookie)
	}
}
