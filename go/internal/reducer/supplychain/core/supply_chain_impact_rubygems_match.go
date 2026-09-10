// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

const (
	supplyChainVersionReasonRubyGemsAffectedRange = "rubygems_affected_range"
	supplyChainVersionReasonRubyGemsKnownFixed    = "rubygems_known_fixed"
)

func evaluateRubyGemsVersionMatch(
	observed string,
	fixedVersion string,
	pkgs []supplychainmodel.AffectedPackage,
) supplyChainVersionMatchDecision {
	if !validRubyGemsVersion(observed) {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := rubyGemsAffectedByAnyPackage(observed, pkgs); affected {
		return affectedVersionDecision(supplyChainVersionReasonRubyGemsAffectedRange)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := compareRubyGemsVersion(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(supplyChainVersionReasonRubyGemsKnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func rubyGemsAffectedByAnyPackage(observed string, pkgs []supplychainmodel.AffectedPackage) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := rubyGemsAffectedByPackage(observed, pkg); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func rubyGemsAffectedByPackage(observed string, pkg supplychainmodel.AffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.AffectedVersions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if cmp, ok := compareRubyGemsVersion(observed, candidate); ok && cmp == 0 {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	for _, affectedRange := range pkg.AffectedRanges {
		if !rubyGemsAffectedRangeKind(affectedRange.Kind) {
			continue
		}
		if affected, ok := rubyGemsRangeContainsDecision(affectedRange, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.AffectedRangeRaw); raw != "" {
		if affected, ok := rubyGemsRequirementContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}

func rubyGemsAffectedRangeKind(kind string) bool {
	return strings.EqualFold(kind, "SEMVER") || strings.EqualFold(kind, "ECOSYSTEM")
}
