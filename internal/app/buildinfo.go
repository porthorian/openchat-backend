package app

import (
	"runtime/debug"
	"strings"
)

var (
	BuildVersion = "main"
	BuildCommit  = ""
	BuildTime    = ""
)

type BuildInfo struct {
	Version     string
	Commit      string
	CommitShort string
	BuildTime   string
	VCSModified string
}

type buildInfoSettings struct {
	moduleVersion string
	vcsRevision   string
	vcsTime       string
	vcsModified   string
}

func CurrentBuildInfo() BuildInfo {
	settings := buildInfoSettings{}
	if info, ok := debug.ReadBuildInfo(); ok {
		settings.moduleVersion = strings.TrimSpace(info.Main.Version)
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				settings.vcsRevision = strings.TrimSpace(setting.Value)
			case "vcs.time":
				settings.vcsTime = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				settings.vcsModified = strings.TrimSpace(setting.Value)
			}
		}
	}

	return resolveBuildInfo(
		strings.TrimSpace(BuildVersion),
		strings.TrimSpace(BuildCommit),
		strings.TrimSpace(BuildTime),
		settings,
	)
}

func resolveBuildInfo(version string, commit string, buildTime string, settings buildInfoSettings) BuildInfo {
	if version == "" {
		version = "main"
	}
	if version == "main" && settings.moduleVersion != "" && settings.moduleVersion != "(devel)" {
		version = settings.moduleVersion
	}

	if commit == "" {
		commit = settings.vcsRevision
	}
	if commit == "" {
		commit = "unknown"
	}

	if buildTime == "" {
		buildTime = settings.vcsTime
	}
	if buildTime == "" {
		buildTime = "unknown"
	}

	vcsModified := settings.vcsModified
	if vcsModified == "" {
		vcsModified = "unknown"
	}

	commitShort := commit
	if len(commitShort) > 12 {
		commitShort = commitShort[:12]
	}

	return BuildInfo{
		Version:     version,
		Commit:      commit,
		CommitShort: commitShort,
		BuildTime:   buildTime,
		VCSModified: vcsModified,
	}
}
