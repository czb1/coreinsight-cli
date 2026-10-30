package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testSkillZIP(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, err := w.Create("SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.WriteString(f, "---\nname: test-skill\ndescription: test\n---\n"); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSkillOperationsUseSessionCaller(t *testing.T) {
	archive := testSkillZIP(t)
	file := filepath.Join(t.TempDir(), "skill.zip")
	if err := os.WriteFile(file, archive, 0600); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"download", "upload", "verify"} {
		t.Run(op, func(t *testing.T) {
			calls := 0
			var srv *httptest.Server
			srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/archive.zip" {
					_, _ = w.Write(archive)
					return
				}
				calls++
				if r.Method != "POST" || r.URL.Query().Get("userId") != "employee001" {
					t.Errorf("request=%s %s", r.Method, r.URL)
				}
				if r.URL.Query().Has("version") {
					t.Error("version must not be filled by default")
				}
				if op == "download" {
					_ = json.NewEncoder(w).Encode(map[string]interface{}{"code": 200, "data": srv.URL + "/archive.zip"})
					return
				}
				if err := r.ParseMultipartForm(1 << 20); err != nil {
					t.Error(err)
					return
				}
				defer r.MultipartForm.RemoveAll()
				_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
			}))
			defer srv.Close()
			rt := &Runtime{AICommunityServer: srv.URL, Timeout: time.Second, Session: &Session{Token: "test-token", CallerID: "employee001"}}
			args := []string{op, "--file", file}
			if op == "download" {
				args = []string{op, "--skill_id", "test-id", "--output", t.TempDir()}
			}
			if err := runSkill(rt, args); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatalf("requests=%d", calls)
			}
			if err := runSkill(rt, append(args, "--caller_id", "someone-else")); err == nil {
				t.Error("caller override accepted")
			}
			if err := runSkill(rt, append(args, "--user_id", "someone-else")); err == nil {
				t.Error("search filter accepted as caller")
			}
			if calls != 1 {
				t.Fatal("invalid caller sent a request")
			}
		})
	}
}

func TestExperienceUploadUsesBoundCallerAsAuthor(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["user_id"] != "employee001" {
			t.Errorf("author=%v", body["user_id"])
		}
		_, _ = w.Write([]byte(`{"code":200,"data":"experience-id"}`))
	}))
	defer srv.Close()
	rt := &Runtime{ChatServer: srv.URL, Timeout: time.Second, Session: &Session{Token: "test-token", CallerID: "employee001"}}
	args := []string{"--scene", "test", "--scene_id", "s1", "--title", "title", "--summary", "summary", "--experience", "content"}
	if err := runExperienceUpload(rt, args); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--caller_id", "--user_id"} {
		if err := runExperienceUpload(rt, append(args, flag, "someone-else")); err == nil {
			t.Errorf("override accepted: %s", flag)
		}
	}
	if calls != 1 {
		t.Fatalf("requests=%d", calls)
	}
}
