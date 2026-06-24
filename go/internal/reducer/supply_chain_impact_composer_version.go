// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "strings"

func evaluateComposerSemverMatch(
	observed string,
	fixedVersion string,
	pkgs []supplyChainAffectedPackage,
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

func composerAffectedByAnyPackage(observed string, pkgs []supplyChainAffectedPackage) (bool, bool) {
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

func composerAffectedByPackage(observed string, pkg supplyChainAffectedPackage) (bool, bool) {
	valid := true
	for _, candidate := range pkg.affectedVersions {
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
	for _, affectedRange := range pkg.affectedRanges {
		if !strings.EqualFold(affectedRange.kind, "SEMVER") {
			continue
		}
		if affected, ok := versionRangeContainsDecision(affectedRange, observed, compareComposerVersion); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	if raw := strings.TrimSpace(pkg.affectedRangeRaw); raw != "" {
		if affected, ok := composerConstraintContains(raw, observed); affected {
			return true, true
		} else if !ok {
			valid = false
		}
	}
	return false, valid
}
