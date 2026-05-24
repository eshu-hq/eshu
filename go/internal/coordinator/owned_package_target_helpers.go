package coordinator

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"golang.org/x/mod/semver"
)

func derivationEcosystems(values []string, defaults []string) map[string]struct{} {
	return stringSet(values, defaults)
}

func stringSet(values []string, defaults []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values)+len(defaults))
	source := values
	if len(source) == 0 {
		source = defaults
	}
	for _, value := range source {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func stringSetContains(values map[string]struct{}, value string) bool {
	_, ok := values[strings.ToLower(strings.TrimSpace(value))]
	return ok
}

func sortedStringSetValues(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func derivationLimit(raw int, fallback int) int {
	if raw > 0 {
		return raw
	}
	return fallback
}

func packageRegistryDerivationFromConfig(raw string) (packageRegistryDerivationConfiguration, error) {
	var decoded packageRegistryRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return packageRegistryDerivationConfiguration{}, fmt.Errorf("decode package registry derivation config: %w", err)
	}
	return decoded.DeriveFromOwnedPackages, nil
}

func vulnerabilityDerivationFromConfig(raw string) (vulnerabilityDerivationConfiguration, error) {
	var decoded vulnerabilityRuntimeConfiguration
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return vulnerabilityDerivationConfiguration{}, fmt.Errorf("decode vulnerability derivation config: %w", err)
	}
	return decoded.DeriveFromOwnedPackages, nil
}

func exactOwnedDependencyVersion(raw string) (string, bool) {
	version := strings.TrimSpace(raw)
	if version == "" {
		return "", false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || nonVersionOwnedDependencyPrefix(lower) {
		return "", false
	}
	if strings.ContainsAny(version, "<>^~*=|, ") ||
		strings.Contains(lower, " - ") ||
		strings.Contains(lower, ".x") ||
		strings.Contains(lower, "x.") {
		return "", false
	}
	semverVersion := version
	if !strings.HasPrefix(semverVersion, "v") {
		semverVersion = "v" + semverVersion
	}
	if !semver.IsValid(semverVersion) {
		return "", false
	}
	return version, true
}

func nonVersionOwnedDependencyPrefix(lower string) bool {
	for _, prefix := range []string{
		"file:",
		"git+",
		"git://",
		"github:",
		"gitlab:",
		"http:",
		"https:",
		"link:",
		"npm:",
		"portal:",
		"workspace:",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}
