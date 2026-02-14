package app

import "testing"

func TestResolveBuildInfoPrefersLdflagsValues(t *testing.T) {
	resolved := resolveBuildInfo(
		"v1.4.2",
		"deadbeefcafebabefeedface1234567890abcd",
		"2026-02-14T22:11:12Z",
		buildInfoSettings{
			moduleVersion: "v9.9.9",
			vcsRevision:   "1111111111111111111111111111111111111111",
			vcsTime:       "2020-01-01T00:00:00Z",
			vcsModified:   "false",
		},
	)

	if resolved.Version != "v1.4.2" {
		t.Fatalf("expected ldflags version, got %q", resolved.Version)
	}
	if resolved.Commit != "deadbeefcafebabefeedface1234567890abcd" {
		t.Fatalf("expected ldflags commit, got %q", resolved.Commit)
	}
	if resolved.CommitShort != "deadbeefcafe" {
		t.Fatalf("expected shortened commit, got %q", resolved.CommitShort)
	}
	if resolved.BuildTime != "2026-02-14T22:11:12Z" {
		t.Fatalf("expected ldflags build time, got %q", resolved.BuildTime)
	}
	if resolved.VCSModified != "false" {
		t.Fatalf("expected vcs modified flag from settings, got %q", resolved.VCSModified)
	}
}

func TestResolveBuildInfoFallsBackToVCSSettings(t *testing.T) {
	revision := "abcdef1234567890abcdef1234567890abcdef12"
	resolved := resolveBuildInfo(
		"main",
		"",
		"",
		buildInfoSettings{
			moduleVersion: "(devel)",
			vcsRevision:   revision,
			vcsTime:       "2026-02-14T10:00:00Z",
			vcsModified:   "true",
		},
	)

	if resolved.Version != "main" {
		t.Fatalf("expected main version fallback, got %q", resolved.Version)
	}
	if resolved.Commit != revision {
		t.Fatalf("expected vcs revision fallback, got %q", resolved.Commit)
	}
	if resolved.CommitShort != revision[:12] {
		t.Fatalf("expected short vcs revision, got %q", resolved.CommitShort)
	}
	if resolved.BuildTime != "2026-02-14T10:00:00Z" {
		t.Fatalf("expected vcs time fallback, got %q", resolved.BuildTime)
	}
	if resolved.VCSModified != "true" {
		t.Fatalf("expected vcs modified=true, got %q", resolved.VCSModified)
	}
}

func TestResolveBuildInfoDefaultsWhenUnavailable(t *testing.T) {
	resolved := resolveBuildInfo("", "", "", buildInfoSettings{})

	if resolved.Version != "main" {
		t.Fatalf("expected default version=main, got %q", resolved.Version)
	}
	if resolved.Commit != "unknown" {
		t.Fatalf("expected default commit=unknown, got %q", resolved.Commit)
	}
	if resolved.CommitShort != "unknown" {
		t.Fatalf("expected default commit short=unknown, got %q", resolved.CommitShort)
	}
	if resolved.BuildTime != "unknown" {
		t.Fatalf("expected default build time=unknown, got %q", resolved.BuildTime)
	}
	if resolved.VCSModified != "unknown" {
		t.Fatalf("expected default vcs_modified=unknown, got %q", resolved.VCSModified)
	}
}
