package cli

import (
	"runtime/debug"
	"testing"
)

func TestBuildVersionSources(t *testing.T) {
	info := &debug.BuildInfo{GoVersion: "go-test", Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
		{Key: "vcs.revision", Value: "1234567890abcdef"},
		{Key: "vcs.time", Value: "2026-10-30T00:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}}
	got := resolveBuildVersion(info, "", "", "")
	if got.Version != "dev+1234567890ab.dirty" || got.Commit != "1234567890abcdef" || !got.Modified || got.GoVersion != "go-test" {
		t.Fatalf("source build: %+v", got)
	}
	if got.BuildTime != "" || got.CommitTime != "2026-10-30T00:00:00Z" {
		t.Fatalf("commit time is not build time: %+v", got)
	}
	info.Main.Version = "v2.3.4"
	if got := resolveBuildVersion(info, "", "", ""); got.Version != "v2.3.4" {
		t.Fatalf("module build: %+v", got)
	}
	got = resolveBuildVersion(info, "release-from-pipeline", "pipeline-commit", "pipeline-time")
	if got.Version != "release-from-pipeline" || got.Commit != "pipeline-commit" || got.BuildTime != "pipeline-time" {
		t.Fatalf("release build: %+v", got)
	}
	if got := resolveBuildVersion(nil, "", "", ""); got.Version != "dev" || got.Commit != "" {
		t.Fatalf("unknown source: %+v", got)
	}
	if got := resolveBuildVersion(nil, "", "abc", ""); got.Version != "dev+abc" {
		t.Fatalf("short revision: %+v", got)
	}
}
