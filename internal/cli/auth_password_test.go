package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func passwordTestStdin(t *testing.T, text string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() { os.Stdin = old; _ = f.Close() })
}

func capturePasswordStderr(t *testing.T, fn func()) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = f
	defer func() { os.Stderr = old; _ = f.Close() }()
	fn()
	raw, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestPasswordLoginAndBusinessRequest(t *testing.T) {
	for _, tc := range []struct {
		name, password, input string
		args                  []string
	}{
		{"flags", "secret-value-123", "", []string{"--username", "employee001", "--password", "secret-value-123"}},
		{"short flags", "secret-value-123", "", []string{"-u", "employee001", "-p", "secret-value-123"}},
		{"stdin", "  exact password  ", "  exact password  \r\n", []string{"--username", "employee001", "--password-stdin"}},
		{"environment", "environment-secret", "", []string{"--method", "password"}},
		{"username and environment password", "environment-secret", "", []string{"--username", "employee001"}},
		{"explicit method and caller", "environment-secret", "", []string{"--method", "password", "--username", "employee001", "--caller_id", "employee001"}},
		{"password is help flag", "--help", "", []string{"--username", "employee001", "--password", "--help"}},
		{"password is global flag", "--server", "", []string{"--username", "employee001", "--password", "--server"}},
		{"password is debug flag", "--debug", "", []string{"--username", "employee001", "--password", "--debug"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedHome(t)
			t.Setenv("COREINSIGHT_AUTH_USERNAME", "employee001")
			t.Setenv("COREINSIGHT_AUTH_PASSWORD", "environment-secret")
			passwordTestStdin(t, tc.input)
			oldBrowser := openBrowserFn
			openBrowserFn = func(string) error { t.Error("password login opened browser"); return nil }
			t.Cleanup(func() { openBrowserFn = oldBrowser })
			if err := saveSession(&Session{Token: "old-token", CallerID: "old-caller"}); err != nil {
				t.Fatal(err)
			}
			calls := make(chan string, 2)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls <- r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case passwordLoginPath:
					var body map[string]string
					if r.Method != http.MethodPost || json.NewDecoder(r.Body).Decode(&body) != nil || body["userName"] != "employee001" || body["passwd"] != tc.password {
						t.Error("password login request does not match the original API contract")
					}
					if r.Header.Get("Cookie") != "" || r.Header.Get("X-Auth-Token") != "" {
						t.Error("login forwarded old credentials")
					}
					http.SetCookie(w, &http.Cookie{Name: "sid", Value: "cookie-secret-123456", MaxAge: 120})
					fmt.Fprint(w, `{"code":0,"msg":"success"}`)
				case "/experience/search":
					var body map[string]interface{}
					if json.NewDecoder(r.Body).Decode(&body) != nil || body["caller_id"] != "employee001" || body["user_id"] != "target002" {
						t.Error("password login identity and search filter were not kept separate")
					}
					if r.Header.Get("Cookie") != "sid=cookie-secret-123456" || r.Header.Get("X-Auth-Token") != "" {
						t.Error("business request did not use the new cookie exclusively")
					}
					fmt.Fprint(w, `{"code":200,"data":[]}`)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			var output string
			logs := capturePasswordStderr(t, func() {
				output = captureAuthStdout(t, func() {
					args := append([]string{"--auth-server", srv.URL, "--debug", "auth", "login"}, tc.args...)
					if Run(args) != 0 {
						t.Error("password login failed")
					}
				})
			})
			if strings.Contains(output+logs, tc.password) || strings.Contains(output+logs, "cookie-secret-123456") || !strings.Contains(output, "password_login_response") {
				t.Fatal("login output leaked a credential or omitted the actual login source")
			}
			sess, err := loadSession()
			if err != nil || sess == nil || sess.Token != "" || sess.Cookie != "sid=cookie-secret-123456" || sess.CallerID != "employee001" || sess.Username != "employee001" {
				t.Fatalf("new cookie session was not persisted correctly: %v", err)
			}
			if expired, _ := sess.expired(); expired || sess.ExpiresAt == "" {
				t.Error("cookie lifetime was not preserved")
			}
			raw, _ := os.ReadFile(sessionFile())
			if strings.Contains(string(raw), tc.password) || strings.Contains(string(raw), "passwd") {
				t.Fatal("password persisted in session file")
			}
			captureAuthStdout(t, func() {
				if Run([]string{"--chat-server", srv.URL, "experience", "search", "--query", "test", "--user_id", "target002"}) != 0 {
					t.Error("business command failed after password login")
				}
			})
			if len(calls) != 2 {
				t.Fatalf("request count=%d, want password login and business request", len(calls))
			}
		})
	}
}

func TestPasswordEnvironmentDoesNotChangeDefaultBrowser(t *testing.T) {
	isolatedHome(t)
	t.Setenv("COREINSIGHT_AUTH_USERNAME", "password-user")
	t.Setenv("COREINSIGHT_AUTH_PASSWORD", "do-not-use")
	if err := saveSession(&Session{Cookie: "sid=old-cookie", CallerID: "password-user"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case devUCAuthorizePath:
			fmt.Fprint(w, `https://login.example.com/authorize`)
		case devUCTokenPath:
			fmt.Fprint(w, `{"code":200,"data":"new-browser-token"}`)
		default:
			t.Error("default login must only use devUC")
		}
	}))
	defer srv.Close()
	oldBrowser := openBrowserFn
	opened := 0
	openBrowserFn = func(string) error { opened++; return nil }
	t.Cleanup(func() { openBrowserFn = oldBrowser })
	for _, extra := range [][]string{nil, {"--method", "browser"}} {
		captureAuthStdout(t, func() {
			args := append([]string{"--market-server", srv.URL, "auth", "login", "--caller_id", "browser-user"}, extra...)
			if Run(args) != 0 {
				t.Error("browser login failed")
			}
		})
	}
	s, err := loadSession()
	if err != nil || s == nil || s.Token != "new-browser-token" || s.Cookie != "" || s.CallerID != "browser-user" || opened != 2 {
		t.Fatal("default browser login inherited password settings or stale credentials")
	}
}

func TestPasswordLoginRejectsAmbiguousOrMissingInputs(t *testing.T) {
	isolatedHome(t)
	t.Setenv("COREINSIGHT_AUTH_USERNAME", "")
	t.Setenv("COREINSIGHT_AUTH_PASSWORD", "")
	passwordTestStdin(t, "")
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("invalid arguments made a network request") }))
	defer srv.Close()
	rt := &Runtime{AuthServer: srv.URL, MarketServer: srv.URL, Timeout: time.Second}
	for _, args := range [][]string{
		{"--method", "invalid"}, {"--method", "password"},
		{"--username", ""}, {"--username", "employee001"},
		{"--username", "employee001", "--password", ""},
		{"--username", "employee001", "--password-stdin"},
		{"--username", "employee001", "--password", "secret", "--password-stdin"},
		{"--username", "employee001", "--password", "secret", "--caller_id", "other-user"},
		{"--username", "employee001", "--password", "secret", "--no-browser"},
		{"--username", "employee001", "--password", "secret", "--timeout", "30"},
		{"--method", "browser", "--username", "employee001"},
	} {
		if err := runAuthLogin(rt, args); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatalf("invalid arguments accepted or leaked a password: %v", err)
		}
	}
	if _, err := os.Stat(sessionFile()); !os.IsNotExist(err) {
		t.Fatal("invalid input wrote a session")
	}
}

func TestPasswordFailurePreservesSessionAndRedactsResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType, cookie string
		status                          int
	}{
		{"http error", `private-response`, "text/plain", "sid=new", 401},
		{"business error", `{"code":401,"msg":"private-response"}`, "application/json", "sid=new", 200},
		{"conflicting success", `{"code":0,"success":false}`, "application/json", "sid=new", 200},
		{"missing success", `{}`, "application/json", "sid=new", 200},
		{"invalid success code", `{"code":null}`, "application/json", "sid=new", 200},
		{"invalid code type", `{"code":{}}`, "application/json", "sid=new", 200},
		{"null", `null`, "application/json", "sid=new", 200},
		{"html", `<html>private-response</html>`, "text/html", "sid=new", 200},
		{"malformed", `{"code":0`, "application/json", "sid=new", 200},
		{"no cookie", `{"code":0}`, "application/json", "", 200},
		{"deleted cookie", `{"code":0}`, "application/json", "sid=new; Max-Age=0", 200},
		{"expired cookie", `{"code":0}`, "application/json", "sid=new; Expires=Wed, 01 Jan 2020 00:00:00 GMT", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolatedHome(t)
			old := &Session{Token: "old-token", CallerID: "old-user"}
			if err := saveSession(old); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(sessionFile())
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				if tc.cookie != "" {
					w.Header().Set("Set-Cookie", tc.cookie)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			rt := &Runtime{AuthServer: srv.URL, Timeout: time.Second, Session: old}
			output := captureAuthStdout(t, func() {
				if err := runAuthLogin(rt, []string{"--username", "employee001", "--password", "secret"}); err == nil {
					t.Error("invalid password login response accepted")
				}
			})
			after, _ := os.ReadFile(sessionFile())
			if string(before) != string(after) || rt.Session != old || strings.Contains(output, "private-response") {
				t.Fatal("failed login changed the old session or leaked the response")
			}
		})
	}
}

func TestPasswordRequestsNeverFollowRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			isolatedHome(t)
			target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("password request followed redirect") }))
			defer target.Close()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, status) }))
			defer srv.Close()
			captureAuthStdout(t, func() {
				if Run([]string{"--auth-server", srv.URL, "auth", "login", "--username", "employee001", "--password", "secret"}) == 0 {
					t.Error("redirect was accepted as successful login")
				}
			})
		})
	}
}

func TestPasswordCookieExpiryAndReplacement(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	for _, cookie := range []string{"removed=old", "removed=gone; Max-Age=0", "sid=old", "sid=new; Max-Age=120", "other=ok; Max-Age=60", "expired=bad; Expires=Wed, 01 Jan 2020 00:00:00 GMT"} {
		resp.Header.Add("Set-Cookie", cookie)
	}
	cookie, expires := extractCookieWithExpiry(resp)
	if cookie != "sid=new; other=ok" || expires.Before(time.Now().Add(50*time.Second)) || expires.After(time.Now().Add(61*time.Second)) {
		t.Fatal("cookie replacement, deletion or earliest expiry was not respected")
	}
}

func TestPasswordDefaultServerAndOverrides(t *testing.T) {
	isolatedHome(t)
	t.Setenv("COREINSIGHT_AUTH_SERVER", "")
	rt, err := defaultRuntime()
	if err != nil || rt.AuthServer != "https://omtool.rnd.huawei.com" {
		t.Fatal("original password login server was not restored")
	}
	t.Setenv("COREINSIGHT_AUTH_SERVER", "https://auth.example.test/")
	rt, err = defaultRuntime()
	if err != nil || rt.AuthServer != "https://auth.example.test" {
		t.Fatal("password server environment override failed")
	}
	if _, err := parseGlobals(rt, []string{"--server", "https://server.example.test", "--auth-server", "https://explicit.example.test", "auth", "login"}); err != nil || rt.AuthServer != "https://explicit.example.test" {
		t.Fatal("explicit password server override failed")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(sessionFile()), "session.json")); !os.IsNotExist(err) {
		t.Fatal("runtime initialization unexpectedly created a session")
	}
}
