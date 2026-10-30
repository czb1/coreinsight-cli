package cli

import (
	"runtime/debug"
	"strings"
)

// Release builds may populate these using -ldflags -X. Source builds use Go's
// module/VCS metadata instead of reporting an unrelated fixed release number.
var Version, Commit, BuildTime string

type buildVersion struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Commit     string `json:"commit,omitempty"`
	CommitTime string `json:"commit_time,omitempty"`
	BuildTime  string `json:"build_time,omitempty"`
	Modified   bool   `json:"modified"`
	GoVersion  string `json:"go_version,omitempty"`
}

func currentBuildVersion() buildVersion {
	info, _ := debug.ReadBuildInfo()
	return resolveBuildVersion(info, Version, Commit, BuildTime)
}

func resolveBuildVersion(info *debug.BuildInfo, release, commit, buildTime string) buildVersion {
	result := buildVersion{Name: "coreinsight-cli", Version: strings.TrimSpace(release), Commit: strings.TrimSpace(commit), BuildTime: strings.TrimSpace(buildTime)}
	if info != nil {
		result.GoVersion = info.GoVersion
		if result.Version == "" && info.Main.Version != "(devel)" {
			result.Version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if result.Commit == "" {
					result.Commit = setting.Value
				}
			case "vcs.time":
				result.CommitTime = setting.Value
			case "vcs.modified":
				result.Modified = setting.Value == "true"
			}
		}
	}
	if result.Version == "" {
		result.Version = "dev"
		if result.Commit != "" {
			short := result.Commit
			if len(short) > 12 {
				short = short[:12]
			}
			result.Version += "+" + short
		}
		if result.Modified {
			result.Version += ".dirty"
		}
	}
	return result
}
