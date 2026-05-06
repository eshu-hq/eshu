package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is injected at build time via ldflags. Source builds default to dev.
var Version = "dev"

// AppVersion returns the normalized application version used by runtime and API
// surfaces. Linker-injected versions win; otherwise module-aware Go installs
// can report the main module version embedded in build info.
func AppVersion() string {
	info, ok := debug.ReadBuildInfo()
	return normalizeAppVersion(Version, info, ok)
}

// normalizeAppVersion centralizes version precedence so ldflags, go install
// module versions, and local source builds report consistently.
func normalizeAppVersion(rawVersion string, info *debug.BuildInfo, ok bool) string {
	version := strings.TrimSpace(rawVersion)
	if version != "" && version != "dev" {
		return version
	}
	if version == "dev" && ok && info != nil {
		moduleVersion := strings.TrimSpace(info.Main.Version)
		if moduleVersion != "" && moduleVersion != "(devel)" {
			return moduleVersion
		}
	}
	if version == "" {
		return "dev"
	}
	return version
}
