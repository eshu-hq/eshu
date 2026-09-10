// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
	"golang.org/x/mod/semver"
)

// supplyChainAffectedRange and supplyChainAffectedRangeEvent moved to
// [supplychainmodel.AffectedRange] / [supplychainmodel.AffectedRangeEvent]
// (#6061 PR1); this file spells the qualified names directly.

// The raw-payload-map range decoders (supplyChainAffectedRangesFromPayload,
// supplyChainAffectedRangeEvents) were replaced by the typed contracts-seam
// equivalents supplyChainAffectedRangesFromTyped /
// supplyChainAffectedRangeEventsFromTyped in
// supply_chain_impact_typed_decode.go (Contract System v1 vulnerability_intelligence
// migration); every affected_ranges read now goes through the typed
// vulnerability.affected_package decode, so the raw-map path has no caller
// left.

func supplyChainAffectedRangeSummary(pkg supplychainmodel.AffectedPackage) string {
	if raw := strings.TrimSpace(pkg.AffectedRangeRaw); raw != "" {
		return raw
	}
	for _, affectedRange := range pkg.AffectedRanges {
		parts := make([]string, 0, len(affectedRange.Events))
		for _, event := range affectedRange.Events {
			switch {
			case event.Introduced != "":
				parts = append(parts, ">="+event.Introduced)
			case event.Fixed != "":
				parts = append(parts, "<"+event.Fixed)
			case event.LastAffected != "":
				parts = append(parts, "<="+event.LastAffected)
			case event.Limit != "":
				parts = append(parts, "<"+event.Limit)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	}
	return ""
}

func semverRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
) (bool, bool) {
	return versionRangeContainsDecision(affectedRange, observed, compareOSVSemver)
}

func rubyGemsRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
) (bool, bool) {
	return versionRangeContainsDecision(affectedRange, observed, compareRubyGemsVersion)
}

func versionRangeContainsDecision(
	affectedRange supplychainmodel.AffectedRange,
	observed string,
	compare versionCompareFunc,
) (bool, bool) {
	if ok, valid := versionBeforeLimitsDecision(observed, affectedRange.Events, compare); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.Events {
		switch {
		case event.Introduced != "":
			if ok, valid := versionAtLeast(observed, event.Introduced, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.Fixed != "":
			if ok, valid := versionAtLeast(observed, event.Fixed, compare); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.LastAffected != "":
			if ok, valid := versionGreaterThan(observed, event.LastAffected, compare); !valid {
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
	events []supplychainmodel.AffectedRangeEvent,
	compare versionCompareFunc,
) (bool, bool) {
	hasLimit := false
	for _, event := range events {
		limit := strings.TrimSpace(event.Limit)
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

// exactManifestDependencyVersion forwards to
// [payloadcore.ExactManifestDependencyVersion].
func exactManifestDependencyVersion(raw string) (string, bool) {
	return payloadcore.ExactManifestDependencyVersion(raw)
}

func exactConsumptionDependencyVersion(
	ecosystem string,
	consumption supplychainmodel.PackageConsumption,
) (string, bool) {
	switch normalizedSupplyChainVersionEcosystem(ecosystem) {
	case string(packageidentity.EcosystemCargo), string(packageidentity.EcosystemNuGet):
		if !consumption.Lockfile {
			return "", false
		}
	}
	if version, ok := exactManifestDependencyVersion(consumption.InstalledVersion); ok {
		return version, true
	}
	if consumption.Lockfile {
		version := strings.TrimSpace(consumption.DependencyRange)
		return version, version != ""
	}
	return exactManifestDependencyVersion(consumption.DependencyRange)
}

// nonVersionDependencyPrefix forwards to
// [payloadcore.NonVersionDependencyPrefix].
func nonVersionDependencyPrefix(lower string) bool {
	return payloadcore.NonVersionDependencyPrefix(lower)
}
