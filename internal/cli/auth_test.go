package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDefaultMarketServer(t *testing.T) {
	if defaultMarketServer != "https://agent.huawei.com" {
		t.Fatalf("defaultMarketServer=%q", defaultMarketServer)
	}
}

func TestFromSourceUnique(t *testing.T) {
	a, err := defaultFromSource()
	if err != nil {
		t.Fatal(err)
	}
	b, err := defaultFromSource()
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 64 || len(b) != 64 || a == b {
		t.Fatal("each login needs an independent 256-bit authorization identifier")
	}
}

func TestExtractTokenFromString(t *testing.T) {
	if got, err := extractToken([]byte(`abc-token-123`)); err != nil || got != "abc-token-123" {
		t.Fatalf("extractToken string=%q err=%v", got, err)
	}
}

func TestExtractTokenFromJSON(t *testing.T) {
	if got, err := extractToken([]byte(`{"code":200,"data":"tok-xyz"}`)); err != nil || got != "tok-xyz" {
		t.Fatalf("extractToken json=%q err=%v", got, err)
	}
	if got, err := extractToken([]byte(`{"code":200,"data":""}`)); err != nil || got != "" {
		t.Fatalf("extractToken empty data=%q err=%v", got, err)
	}
	if got, err := extractToken([]byte(``)); err != nil || got != "" {
		t.Fatalf("extractToken empty body=%q err=%v", got, err)
	}
}

func TestMaskedToken(t *testing.T) {
	if maskedToken("") != "" {
		t.Fatal("empty token should mask to empty")
	}
	if maskedToken("short") != "*****" {
		t.Fatalf("short token=%q", maskedToken("short"))
	}
	if got := maskedToken("abcdefghijklmnop"); !strings.HasPrefix(got, "abc") || !strings.HasSuffix(got, "nop") {
		t.Fatalf("long token=%q", got)
	}
}

func TestFetchAuthorizeURLPlainString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != devUCAuthorizePath {
			t.Errorf("path=%s", r.URL.Path)
		}
		if r.URL.Query().Get("fromSource") == "" {
			t.Error("fromSource missing")
		}
		_, _ = w.Write([]byte(`https://login.example.com/auth?x=1`))
	}))
	defer srv.Close()
	rt := &Runtime{MarketServer: srv.URL, Timeout: 5 * time.Second}
	url, err := fetchAuthorizeURL(rt, srv.URL, "fs-key")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://login.example.com/auth?x=1" {
		t.Fatalf("url=%q", url)
	}
}

func TestFetchAuthorizeURLJSONData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":200,"data":"https://login.example.com/auth?y=2"}`))
	}))
	defer srv.Close()
	rt := &Runtime{MarketServer: srv.URL, Timeout: 5 * time.Second}
	url, err := fetchAuthorizeURL(rt, srv.URL, "fs-key")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://login.example.com/auth?y=2" {
		t.Fatalf("url=%q", url)
	}
}

func TestPollTokenSucceeds(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			_, _ = w.Write([]byte(`{"code":200,"data":""}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"data":"real-token"}`))
	}))
	defer srv.Close()

	old := pollInterval
	pollInterval = 5 * time.Millisecond
	defer func() { pollInterval = old }()

	rt := &Runtime{MarketServer: srv.URL, Timeout: 5 * time.Second}
	token, err := pollToken(rt, srv.URL, "fs-key", 60)
	if err != nil {
		t.Fatal(err)
	}
	if token != "real-token" {
		t.Fatalf("token=%q", token)
	}
}

func TestPollTokenTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":""}`))
	}))
	defer srv.Close()

	old := pollInterval
	pollInterval = 2 * time.Millisecond
	defer func() { pollInterval = old }()

	rt := &Runtime{MarketServer: srv.URL, Timeout: 5 * time.Second}
	_, err := pollToken(rt, srv.URL, "fs-key", 3)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestRunAuthLoginEndToEnd(t *testing.T) {
	isolatedHome(t)

	var authorizeCalled, tokenCalled int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case devUCAuthorizePath:
			atomic.AddInt32(&authorizeCalled, 1)
			_, _ = w.Write([]byte(`https://login.example.com/auth?z=9`))
		case devUCTokenPath:
			atomic.AddInt32(&tokenCalled, 1)
			_, _ = w.Write([]byte(`{"code":200,"data":"e2e-token"}`))
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

	if got := Run([]string{"--market-server", srv.URL, "auth", "login", "--caller_id", "employee001"}); got != 0 {
		t.Fatalf("Run(auth login) = %d, want 0", got)
	}
	if atomic.LoadInt32(&authorizeCalled) == 0 || atomic.LoadInt32(&tokenCalled) == 0 {
		t.Fatal("authorize/token endpoints not called")
	}
	s, err := loadSession()
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Token != "e2e-token" {
		t.Fatalf("session token=%v", s)
	}
}

func TestRunAuthLoginNoBrowser(t *testing.T) {
	isolatedHome(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == devUCAuthorizePath {
			_, _ = w.Write([]byte(`https://login.example.com/auth?nb=1`))
			return
		}
		_, _ = w.Write([]byte(`{"code":200,"data":"nb-token"}`))
	}))
	defer srv.Close()

	oldInterval := pollInterval
	oldBrowser := openBrowserFn
	pollInterval = 5 * time.Millisecond
	browserOpened := false
	openBrowserFn = func(string) error { browserOpened = true; return nil }
	defer func() {
		pollInterval = oldInterval
		openBrowserFn = oldBrowser
	}()

	if got := Run([]string{"--market-server", srv.URL, "auth", "login", "--caller_id", "employee001", "--no-browser"}); got != 0 {
		t.Fatalf("Run = %d", got)
	}
	if browserOpened {
		t.Fatal("--no-browser must not open browser")
	}
}
