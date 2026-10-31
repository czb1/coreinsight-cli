package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func fastAuthPolling(t *testing.T) {
	t.Helper()
	interval, retries := pollInterval, pollMaxRetry
	pollInterval, pollMaxRetry = 5*time.Millisecond, 5
	t.Cleanup(func() { pollInterval, pollMaxRetry = interval, retries })
}

func TestTokenResponsesRejectErrors(t *testing.T) {
	for _, body := range []string{
		`{"code":401,"msg":"not logged in"}`,
		`{"code":500,"data":"looks-like-a-token"}`,
		`{"code":200,"success":false,"data":"looks-like-a-token"}`,
		`{"data":"looks-like-a-token"}`,
		`{}`, `{"message":"OK"}`, `{"code":null,"data":"token"}`,
		`{"code":200,"success":false}`, `{"success":false}`,
		`{"code":{},"data":"token"}`, `{"success":{},"data":"token"}`,
		`{"code":200,"data":{}}`, `{"code":200,"data":[]}`,
		`{"code":200,"data":42}`, `{"code":200,"data":false}`,
		`{"code":200,"data":"<nil>"}`, `{"code":200,"data":"null"}`,
		`{"code":200,"data":"line\nbreak"}`, `{"code":200,"data":"bad token"}`,
		`{"code":200`, `[]`, `42`, `false`, `<html>login</html>`,
		`Internal Server Error`, `Unauthorized`, `error`,
	} {
		t.Run(body, func(t *testing.T) {
			if token, err := extractToken([]byte(body)); err == nil || token != "" {
				t.Fatalf("invalid response accepted: token=%q err=%v", token, err)
			}
		})
	}
	for _, body := range []string{
		``, `null`, `""`, `{"code":200,"data":null}`, `{"code":"200","data":" "}`,
		`{"code":200}`, `{"code":200,"message":"OK"}`, `{"code":"200"}`,
		`{"code":0}`, `{"code":"0"}`, `{"success":true}`,
	} {
		if token, err := extractToken([]byte(body)); err != nil || token != "" {
			t.Fatalf("pending response %s: token=%q err=%v", body, token, err)
		}
	}
	for _, body := range []string{`"opaque-token"`, `{"code":0,"data":"opaque-token"}`, `{"success":true,"data":"opaque-token"}`} {
		if token, err := extractToken([]byte(body)); err != nil || token != "opaque-token" {
			t.Fatalf("success response: token=%q err=%v", token, err)
		}
	}
}

func TestInvalidLoginResponsesNeverSaveSession(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
	}{
		{"unauthorized", `{"code":401,"msg":"private-detail"}`, "application/json", 401},
		{"server error", `private-detail`, "text/plain", 500},
		{"business error", `{"code":500,"data":"private-detail"}`, "application/json", 200},
		{"null pending", `{"code":200,"data":null}`, "application/json", 200},
		{"missing data pending", `{"code":200,"message":"OK"}`, "application/json", 200},
		{"malformed JSON", `{"code":200,"data":"private-detail"`, "application/json", 200},
		{"wrong type", `{"code":200,"data":{}}`, "application/json", 200},
		{"HTML", `private-detail`, "text/html", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedHome(t)
			fastAuthPolling(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == devUCAuthorizePath {
					fmt.Fprint(w, `https://login.example.com/authorize`)
					return
				}
				w.Header().Set("Content-Type", tc.contentType)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			rt := &Runtime{MarketServer: srv.URL, Timeout: time.Second}
			output := captureAuthStdout(t, func() {
				if err := runAuthLogin(rt, []string{"--caller_id", "employee001", "--no-browser"}); err == nil {
					t.Error("login should fail without a valid token response")
				}
			})
			if strings.Contains(output, "private-detail") || strings.Contains(output, `"authenticated": true`) {
				t.Fatalf("failure leaked response or claimed success: %s", output)
			}
			if strings.HasSuffix(tc.name, "pending") && !strings.Contains(output, "Authorization Timeout") {
				t.Fatalf("pending response must wait until authorization times out: %s", output)
			}
			if _, err := os.Stat(sessionFile()); !os.IsNotExist(err) || rt.Session != nil {
				t.Fatalf("failed login must not save a session: stat=%v", err)
			}
		})
	}
}

func TestLoginUsesFreshAuthorizationAndDefaultBrowser(t *testing.T) {
	isolatedHome(t)
	fastAuthPolling(t)
	var mu sync.Mutex
	keys := []string{}
	authorized := map[string]bool{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		key := r.URL.Query().Get("fromSource")
		switch r.URL.Path {
		case devUCAuthorizePath:
			keys = append(keys, key)
			fmt.Fprint(w, srv.URL+"/browser?fromSource="+key)
		case "/browser":
			authorized[key] = true
		case devUCTokenPath:
			if !authorized[key] {
				fmt.Fprint(w, `{"code":200,"data":null}`)
			} else {
				fmt.Fprint(w, `{"code":200,"data":"new-token"}`)
			}
		}
	}))
	defer srv.Close()
	old := openBrowserFn
	opened := 0
	openBrowserFn = func(link string) error {
		opened++
		resp, err := srv.Client().Get(link)
		if err != nil {
			return err
		}
		return resp.Body.Close()
	}
	t.Cleanup(func() { openBrowserFn = old })
	rt := &Runtime{MarketServer: srv.URL, Timeout: time.Second}
	for i := 0; i < 2; i++ {
		if err := runAuthLogin(rt, []string{"--caller_id", "employee001"}); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if opened != 2 || len(keys) != 2 || len(keys[0]) != 64 || keys[0] == keys[1] || !authorized[keys[1]] {
		t.Fatalf("each login must launch a distinct browser flow: opened=%d keys=%v", opened, keys)
	}
}

func TestNoBrowserPendingDoesNotReuseOrOverwriteSavedSession(t *testing.T) {
	isolatedHome(t)
	fastAuthPolling(t)
	previous := &Session{Token: "previous-token", CallerID: "previous-caller", SavedAt: "2026-01-01T00:00:00Z"}
	if err := saveSession(previous); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(sessionFile())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == devUCAuthorizePath {
			fmt.Fprint(w, `https://login.example.com/auth`)
			return
		}
		fmt.Fprint(w, `{"code":200,"message":"OK"}`)
	}))
	defer srv.Close()
	old := openBrowserFn
	openBrowserFn = func(string) error { t.Error("--no-browser must not launch"); return nil }
	t.Cleanup(func() { openBrowserFn = old })
	rt := &Runtime{MarketServer: srv.URL, Timeout: time.Second, Session: previous}
	if err := runAuthLogin(rt, []string{"--caller_id", "employee001", "--no-browser"}); err == nil {
		t.Fatal("old local credentials must not complete a new login")
	}
	after, _ := os.ReadFile(sessionFile())
	if string(before) != string(after) || rt.Session != previous {
		t.Fatal("failed login overwrote the previous session")
	}
}

func TestInvalidAuthorizeResponsesAreRejected(t *testing.T) {
	for _, body := range []string{`{"code":401,"data":"https://login.example.com"}`, `{"code":200,"message":"OK"}`, `{"success":true}`, `{"code":200,"data":null}`, `{"code":200,"data":{}}`, `<html>failure</html>`, `javascript:alert(1)`, `https:///missing-host`, `https://user:pass@example.com`, `https://example.com/ space`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		_, err := fetchAuthorizeURL(&Runtime{Timeout: time.Second}, srv.URL, "test-key")
		srv.Close()
		if err == nil {
			t.Errorf("accepted invalid authorization URL response: %s", body)
		}
	}
}

func TestPlainDevUCResponsesWithJSONContentType(t *testing.T) {
	// Preserve the existing bare-string API contract even with a JSON media type.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == devUCAuthorizePath {
			fmt.Fprint(w, "https://login.example.com/authorize")
			return
		}
		fmt.Fprint(w, "opaque-token")
	}))
	defer srv.Close()
	rt := &Runtime{Timeout: time.Second}
	if link, err := fetchAuthorizeURL(rt, srv.URL, "test"); err != nil || link != "https://login.example.com/authorize" {
		t.Fatalf("bare authorization URL rejected: %q %v", link, err)
	}
	if token, err := pollToken(rt, srv.URL, "test", 2); err != nil || token != "opaque-token" {
		t.Fatalf("bare token rejected: %q %v", token, err)
	}
}

func TestPollDeadlineInterruptsSlowResponse(t *testing.T) {
	fastAuthPolling(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	start := time.Now()
	_, err := pollToken(&Runtime{Timeout: time.Second}, srv.URL, "test", 2)
	if !errors.Is(err, errAuthorizationTimeout) || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("polling deadline not enforced: %v", err)
	}
}

func TestAuthTimeoutIsNotConsumedAsGlobal(t *testing.T) {
	rt := &Runtime{Timeout: time.Second}
	args, err := parseGlobals(rt, []string{"--timeout", "7", "auth", "login", "--timeout", "23", "--no-browser"})
	if err != nil || rt.Timeout != 7*time.Second || !reflect.DeepEqual(args, []string{"auth", "login", "--timeout", "23", "--no-browser"}) {
		t.Fatalf("args=%v timeout=%v err=%v", args, rt.Timeout, err)
	}
}

func TestStatusRejectsPreviouslyMisparsedCredentials(t *testing.T) {
	for _, token := range []string{"<nil>", `{"code":401}`, "Internal Server Error", "null", "<html>error</html>", " "} {
		rt := &Runtime{Session: &Session{Token: token}}
		if !errors.Is(runAuthStatus(rt, nil), errUnauthenticated) || !errors.Is(requireAuthenticated(rt, "skill"), errUnauthenticated) {
			t.Errorf("invalid stored token accepted: %q", token)
		}
	}
}

func TestStatusReportsLocalOnly(t *testing.T) {
	rt := &Runtime{Session: &Session{Token: "opaque-token", SavedAt: "2026-01-01T00:00:00Z"}}
	output := captureAuthStdout(t, func() {
		if err := runAuthStatus(rt, nil); err != nil {
			t.Fatal(err)
		}
	})
	var response struct {
		Result map[string]interface{} `json:"result"`
	}
	if err := json.Unmarshal([]byte(output), &response); err != nil {
		t.Fatal(err)
	}
	if response.Result["server_verified"] != false || response.Result["source"] != "session_file" || response.Result["credential_state"] != "stored" {
		t.Fatalf("status must explicitly report local-only evidence: %s", output)
	}
}

func TestBrowserCommandPropagatesLauncherFailure(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uses a fake Linux desktop launcher")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "xdg-open"), []byte("#!/bin/sh\nexit 7\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := openBrowser("https://example.com"); err == nil {
		t.Fatal("launcher nonzero exit must be reported")
	}
}

func TestWindowsBrowserCommandEscapesURLAndStopsOnError(t *testing.T) {
	link := "https://example.com/auth?name=o'hara&x=1"
	cmd := browserCommand(context.Background(), "windows", link)
	command := cmd.Args[len(cmd.Args)-1]
	if !strings.Contains(command, "$ErrorActionPreference = 'Stop'") || !strings.Contains(command, "o''hara&x=1") {
		t.Fatalf("unsafe or non-failing browser command: %v", cmd.Args)
	}
	if _, err := url.Parse(link); err != nil {
		t.Fatal(err)
	}
}

func captureAuthStdout(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	old := os.Stdout
	defer func() { os.Stdout = old }()
	os.Stdout = f
	fn()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
