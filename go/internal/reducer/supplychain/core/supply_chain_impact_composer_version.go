// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

func evaluateComposerSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplychainmodel.AffectedPackage,
) supplyChainVersionMatchDecision {
	if !validComposerVersion(observed) {
		return malformedInstalledVersionDecision()
	}
	if affected, malformed := composerAffectedByAnyPackage(observed, pkgs); affected {
		return affectedVersionDecision(supplyChainVersionReasonComposerSemverAffectedRange)
	} else if malformed {
		return malformedVersionDecision()
	}
	if fixedVersion != "" {
		cmp, valid := compareComposerVersion(observed, fixedVersion)
		if !valid {
			return malformedVersionDecision()
		}
		if cmp >= 0 {
			return knownFixedDecision(supplyChainVersionReasonComposerSemverKnownFixed)
		}
	}
	return possiblyAffectedDecision(supplyChainVersionReasonNoAffectedMatch, nil)
}

func composerAffectedByAnyPackage(observed string, pkgs []supplychainmodel.AffectedPackage) (bool, bool) {
	malformed := false
	for _, pkg := range pkgs {
		if affected, valid := composerAffectedByPackage(observed, pkg); affected {
			return true, false
		} else if !valid {
			malformed = true
		}
	}
	return false, malformed
}

func composerAffectedByPackage(observed string, pkg supplychainmodel.AffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.AffectedVersions {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if cmp, ok := compareComposerVersion(observed, candidate); ok && cmp == 0 {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	for _, affectedRange := range pkg.AffectedRanges {
		if !strings.EqualFold(affectedRange.Kind, "SEMVER") {
			continue
		}
		if affected, ok := versionRangeContainsDecision(affectedRange, observed, compareComposerVersion); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.AffectedRangeRaw); raw != "" {
		if affected, ok := composerConstraintContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}
