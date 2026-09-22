package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const sessionFileName = "session.json"

type Session struct {
	Cookie    string `json:"cookie"`
	Username  string `json:"username,omitempty"`
	Server    string `json:"server,omitempty"`
	SavedAt   string `json:"saved_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

func sessionDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".coreinsight-cli"), nil
}
func sessionFile() string {
	d, err := sessionDir()
	if err != nil {
		return "~/.coreinsight-cli/" + sessionFileName
	}
	return filepath.Join(d, sessionFileName)
}
func saveSession(s *Session) error {
	d, err := sessionDir()
	if err != nil {
		return fmt.Errorf("无法确定用户主目录: %w", err)
	}
	if err := os.MkdirAll(d, 0o700); err != nil {
		return fmt.Errorf("创建会话目录失败: %w", err)
	}
	if s.SavedAt == "" {
		s.SavedAt = time.Now().Format(time.RFC3339)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, sessionFileName), raw, 0o600)
}
func loadSession() (*Session, error) {
	p := sessionFile()
	raw, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("会话文件损坏: %w", err)
	}
	if s.Cookie == "" {
		return nil, nil
	}
	return &s, nil
}
func clearSession() (bool, error) {
	p := sessionFile()
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
func (s *Session) expired() (bool, string) {
	if s == nil {
		return true, "no_session"
	}
	if s.ExpiresAt == "" {
		return false, ""
	}
	t, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil {
		return false, ""
	}
	if time.Now().After(t) {
		return true, "cookie_expired"
	}
	return false, ""
}
func (s *Session) ageSeconds() int64 {
	if s == nil {
		return -1
	}
	t, err := time.Parse(time.RFC3339, s.SavedAt)
	if err != nil {
		return -1
	}
	return int64(time.Since(t).Seconds())
}
func maskedCookie(cookie string) string {
	var parts []string
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			parts = append(parts, "***")
			continue
		}
		v := kv[1]
		if len(v) <= 8 {
			v = strings.Repeat("*", len(v))
		} else {
			v = v[:3] + "******" + v[len(v)-3:]
		}
		parts = append(parts, kv[0]+"="+v)
	}
	return strings.Join(parts, "; ")
}
