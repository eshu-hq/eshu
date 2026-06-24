package elixir

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	mixManifestHexDependencyPattern = regexp.MustCompile(`\{\s*:([A-Za-z0-9_]+)\s*,\s*"([^"]+)"([^}]*)\}`)
	mixManifestVCSDependencyPattern = regexp.MustCompile(`\{\s*:([A-Za-z0-9_]+)\s*,\s*(?:github|git):\s*"([^"]+)"([^}]*)\}`)
	mixLockHexPackagePattern        = regexp.MustCompile(`^\s*"([^"]+)":\s*\{:hex,\s*:([A-Za-z0-9_]+),\s*"([^"]+)"`)
	mixLockNestedDependencyPattern  = regexp.MustCompile(`\{:([A-Za-z0-9_]+),\s*"([^"]+)",\s*\[([^\]]*)\]\}`)
)

func appendHexDependencyRows(payload map[string]any, path string, source string) {
	switch strings.ToLower(filepath.Base(path)) {
	case "mix.exs":
		appendMixManifestDependencyRows(payload, source)
	case "mix.lock":
		appendMixLockDependencyRows(payload, source)
	}
}

func appendMixManifestDependencyRows(payload map[string]any, source string) {
	lines := strings.Split(source, "\n")
	for index, line := range lines {
		lineNumber := index + 1
		if match := mixManifestHexDependencyPattern.FindStringSubmatch(line); len(match) == 4 {
			options := match[3]
			appName := strings.ToLower(strings.TrimSpace(match[1]))
			packageName := mixDependencyHexPackageName(appName, options)
			configKind := "dependency"
			if mixDependencyUsesNonRegistrySource(options) {
				configKind = "vcs_dependency"
			}
			row := hexDependencyRow(
				packageName,
				match[2],
				configKind,
				"deps",
				lineNumber,
				nil,
			)
			if packageName != appName {
				row["app_name"] = appName
			}
			row["dependency_scope"] = mixDependencyScope(options)
			if namespace := mixDependencyOrganization(options); namespace != "" {
				row["namespace"] = namespace
			}
			shared.AppendBucket(payload, "variables", row)
			continue
		}
		if match := mixManifestVCSDependencyPattern.FindStringSubmatch(line); len(match) == 4 {
			row := hexDependencyRow(match[1], match[2], "vcs_dependency", "deps", lineNumber, nil)
			row["dependency_scope"] = mixDependencyScope(match[3])
			shared.AppendBucket(payload, "variables", row)
		}
	}
}

func appendMixLockDependencyRows(payload map[string]any, source string) {
	lines := strings.Split(source, "\n")
	for index, line := range lines {
		lineNumber := index + 1
		match := mixLockHexPackagePattern.FindStringSubmatch(line)
		if len(match) != 4 {
			continue
		}
		lockName := match[2]
		row := hexDependencyRow(lockName, match[3], "dependency", "mix.lock", lineNumber, []string{lockName})
		row["lockfile"] = true
		row["dependency_depth"] = 1
		row["direct_dependency"] = true
		shared.AppendBucket(payload, "variables", row)

		for _, nestedMatch := range mixLockNestedDependencyPattern.FindAllStringSubmatch(line, -1) {
			if len(nestedMatch) != 4 {
				continue
			}
			nestedName := nestedMatch[1]
			nested := hexDependencyRow(
				nestedName,
				nestedMatch[2],
				"dependency",
				"mix.lock",
				lineNumber,
				[]string{lockName, nestedName},
			)
			nested["lockfile"] = true
			nested["dependency_depth"] = 2
			nested["direct_dependency"] = false
			nested["dependency_scope"] = mixLockDependencyScope(nestedMatch[3])
			shared.AppendBucket(payload, "variables", nested)
		}
	}
}

func hexDependencyRow(
	name string,
	value string,
	configKind string,
	section string,
	lineNumber int,
	dependencyPath []string,
) map[string]any {
	row := map[string]any{
		"name":              strings.ToLower(strings.TrimSpace(name)),
		"value":             strings.TrimSpace(value),
		"line_number":       lineNumber,
		"lang":              "elixir",
		"config_kind":       configKind,
		"package_manager":   "hex",
		"section":           section,
		"dependency_scope":  "runtime",
		"direct_dependency": true,
	}
	if len(dependencyPath) > 0 {
		row["dependency_path"] = dependencyPath
	}
	return row
}

func mixDependencyScope(options string) string {
	options = strings.ToLower(options)
	switch {
	case strings.Contains(options, "only: :test"):
		return "test"
	case strings.Contains(options, "only: :dev"):
		return "dev"
	default:
		return "runtime"
	}
}

var (
	mixDependencyOrganizationPattern = regexp.MustCompile(`organization:\s*(?::([A-Za-z0-9_]+)|"([^"]+)")`)
	mixDependencyHexPackagePattern   = regexp.MustCompile(`hex:\s*(?::([A-Za-z0-9_]+)|"([^"]+)")`)
)

func mixDependencyHexPackageName(appName string, options string) string {
	match := mixDependencyHexPackagePattern.FindStringSubmatch(options)
	if len(match) != 3 {
		return appName
	}
	if match[1] != "" {
		return strings.ToLower(strings.TrimSpace(match[1]))
	}
	return strings.ToLower(strings.TrimSpace(match[2]))
}

func mixDependencyOrganization(options string) string {
	match := mixDependencyOrganizationPattern.FindStringSubmatch(options)
	if len(match) != 3 {
		return ""
	}
	if match[1] != "" {
		return strings.ToLower(strings.TrimSpace(match[1]))
	}
	return strings.ToLower(strings.TrimSpace(match[2]))
}

func mixDependencyUsesNonRegistrySource(options string) bool {
	lower := strings.ToLower(options)
	return strings.Contains(lower, "git:") ||
		strings.Contains(lower, "github:") ||
		strings.Contains(lower, "path:") ||
		strings.Contains(lower, "in_umbrella:")
}

func mixLockDependencyScope(options string) string {
	if strings.Contains(strings.ToLower(options), "optional: true") {
		return "optional"
	}
	return "runtime"
}
