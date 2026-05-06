package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestAppVersionDefaultsToDevWhenEmpty(t *testing.T) {
	original := Version
	Version = "   "
	t.Cleanup(func() { Version = original })

	if got, want := AppVersion(), "dev"; got != want {
		t.Fatalf("AppVersion() = %q, want %q", got, want)
	}
}

func TestAppVersionPreservesInjectedValue(t *testing.T) {
	original := Version
	Version = "v0.0.1"
	t.Cleanup(func() { Version = original })

	if got, want := AppVersion(), "v0.0.1"; got != want {
		t.Fatalf("AppVersion() = %q, want %q", got, want)
	}
}

func TestNormalizeAppVersionFallsBackToGoModuleVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v1.2.3",
		},
	}

	if got, want := normalizeAppVersion("dev", info, true), "v1.2.3"; got != want {
		t.Fatalf("normalizeAppVersion() = %q, want %q", got, want)
	}
}

func TestNormalizeAppVersionPrefersInjectedVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v1.2.3",
		},
	}

	if got, want := normalizeAppVersion("v9.9.9", info, true), "v9.9.9"; got != want {
		t.Fatalf("normalizeAppVersion() = %q, want %q", got, want)
	}
}

func TestNormalizeAppVersionIgnoresDevelopmentModuleVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "(devel)",
		},
	}

	if got, want := normalizeAppVersion("dev", info, true), "dev"; got != want {
		t.Fatalf("normalizeAppVersion() = %q, want %q", got, want)
	}
}
