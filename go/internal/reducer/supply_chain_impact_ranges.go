// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"golang.org/x/mod/semver"
)

type supplyChainAffectedRange struct {
	kind   string
	events []supplyChainAffectedRangeEvent
}

type supplyChainAffectedRangeEvent struct {
	introduced   string
	fixed        string
	lastAffected string
	limit        string
}

// The raw-payload-map range decoders (supplyChainAffectedRangesFromPayload,
// supplyChainAffectedRangeEvents) were replaced by the typed contracts-seam
// equivalents supplyChainAffectedRangesFromTyped /
// supplyChainAffectedRangeEventsFromTyped in
// supply_chain_impact_typed_decode.go (Contract System v1 vulnerability_intelligence
// migration); every affected_ranges read now goes through the typed
// vulnerability.affected_package decode, so the raw-map path has no caller
// left.

func supplyChainAffectedRangeSummary(pkg supplyChainAffectedPackage) string {
	if raw := strings.TrimSpace(pkg.affectedRangeRaw); raw != "" {
		return raw
	}
	for _, affectedRange := range pkg.affectedRanges {
		parts := make([]string, 0, len(affectedRange.events))
		for _, event := range affectedRange.events {
			switch {
			case event.introduced != "":
				parts = append(parts, ">="+event.introduced)
			case event.fixed != "":
				parts = append(parts, "<"+event.fixed)
			case event.lastAffected != "":
				parts = append(parts, "<="+event.lastAffected)
			case event.limit != "":
				parts = append(parts, "<"+event.limit)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return ""
}

func semverRangeContainsDecision(
	affectedRange supplyChainAffectedRange,
	observed string,
) (bool, bool) {
	return versionRangeContainsDecision(affectedRange, observed, compareOSVSemver)
}

func rubyGemsRangeContainsDecision(
	affectedRange supplyChainAffectedRange,
	observed string,
) (bool, bool) {
	return versionRangeContainsDecision(affectedRange, observed, compareRubyGemsVersion)
}

func versionRangeContainsDecision(
	affectedRange supplyChainAffectedRange,
	observed string,
	compare versionCompareFunc,
) (bool, bool) {
	if ok, valid := versionBeforeLimitsDecision(observed, affectedRange.events, compare); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.events {
		switch {
		case event.introduced != "":
			if ok, valid := versionAtLeast(observed, event.introduced, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.fixed != "":
			if ok, valid := versionAtLeast(observed, event.fixed, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.lastAffected != "":
			if ok, valid := versionGreaterThan(observed, event.lastAffected, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

func versionBeforeLimitsDecision(
	observed string,
	events []supplyChainAffectedRangeEvent,
	compare versionCompareFunc,
) (bool, bool) {
	hasLimit := false
	for _, event := range events {
		limit := strings.TrimSpace(event.limit)
		if limit == "" {
			continue
		}
		hasLimit = true
		if limit == "*" {
			return true, true
		}
		if ok, valid := versionLessThan(observed, limit, compare); !valid {
			return false, false
		} else if ok {
			return true, true
		}
	}
	return !hasLimit, true
}

func semverAtLeast(left string, right string) (bool, bool) {
	return versionAtLeast(left, right, compareOSVSemver)
}

func versionAtLeast(left string, right string, compare versionCompareFunc) (bool, bool) {
	cmp, ok := compare(left, right)
	return cmp >= 0, ok
}

func versionGreaterThan(left string, right string, compare versionCompareFunc) (bool, bool) {
	cmp, ok := compare(left, right)
	return cmp > 0, ok
}

func versionLessThan(left string, right string, compare versionCompareFunc) (bool, bool) {
	cmp, ok := compare(left, right)
	return cmp < 0, ok
}

func compareOSVSemver(left string, right string) (int, bool) {
	leftNormalized, ok := normalizeOSVSemver(left)
	if !ok {
		return 0, false
	}
	rightNormalized, ok := normalizeOSVSemver(right)
	if !ok {
		return 0, false
	}
	return semver.Compare(leftNormalized, rightNormalized), true
}

func normalizeOSVSemver(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	if raw == "0" {
		return "v0.0.0", true
	}
	if !strings.HasPrefix(raw, "v") {
		raw = "v" + raw
	}
	if !semver.IsValid(raw) {
		return "", false
	}
	return raw, true
}

func compareComposerVersion(left string, right string) (int, bool) {
	leftNormalized, ok := normalizeComposerVersion(left)
	if !ok {
		return 0, false
	}
	rightNormalized, ok := normalizeComposerVersion(right)
	if !ok {
		return 0, false
	}
	return semver.Compare(leftNormalized, rightNormalized), true
}

func validComposerVersion(raw string) bool {
	_, ok := normalizeComposerVersion(raw)
	return ok
}

func normalizeComposerVersion(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	raw = strings.TrimPrefix(raw, "v")
	raw = strings.TrimPrefix(raw, "V")
	parts := strings.SplitN(raw, "-", 2)
	core := parts[0]
	numbers := strings.Split(core, ".")
	if len(numbers) > 3 {
		return "", false
	}
	for _, number := range numbers {
		if number == "" {
			return "", false
		}
		for _, char := range number {
			if char < '0' || char > '9' {
				return "", false
			}
		}
	}
	for len(numbers) < 3 {
		numbers = append(numbers, "0")
	}
	normalized := "v" + strings.Join(numbers, ".")
	if len(parts) == 2 {
		suffix := strings.TrimSpace(parts[1])
		if suffix == "" {
			return "", false
		}
		normalized += "-" + suffix
	}
	if !semver.IsValid(normalized) {
		return "", false
	}
	return normalized, true
}

func exactManifestDependencyVersion(raw string) (string, bool) {
	version := strings.TrimSpace(raw)
	if version == "" {
		return "", false
	}
	lower := strings.ToLower(version)
	if lower == "latest" || nonVersionDependencyPrefix(lower) {
		return "", false
	}
	if strings.ContainsAny(version, "<>^~*=|, []") ||
		strings.Contains(lower, " - ") ||
		strings.Contains(version, "$") ||
		strings.Contains(lower, ".x") ||
		strings.Contains(lower, "x.") {
		return "", false
	}
	return version, true
}

func exactConsumptionDependencyVersion(
	ecosystem string,
	consumption supplyChainPackageConsumption,
) (string, bool) {
	switch normalizedSupplyChainVersionEcosystem(ecosystem) {
	case string(packageidentity.EcosystemCargo), string(packageidentity.EcosystemNuGet):
		if !consumption.lockfile {
			return "", false
		}
	}
	if version, ok := exactManifestDependencyVersion(consumption.installedVersion); ok {
		return version, true
	}
	if consumption.lockfile {
		version := strings.TrimSpace(consumption.dependencyRange)
		return version, version != ""
	}
	return exactManifestDependencyVersion(consumption.dependencyRange)
}

func nonVersionDependencyPrefix(lower string) bool {
	for _, prefix := range []string{
		"file:",
		"git+",
		"github:",
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
