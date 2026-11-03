// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// osUnlistedOutcome is what one ReleaseUnlisted fact says about the host.
type osUnlistedOutcome int

const (
	// osUnlistedPossible: the fact names no range for the host's line. A
	// listed range may contain the host version or not; either way the
	// advisory provably omits the host's line, so the fact contributes the
	// weakest signal, never known-fixed (#9806, ruling Q1.3).
	osUnlistedPossible osUnlistedOutcome = iota
	// osUnlistedMalformed: a range or fix could not be compared. Fail-closed
	// beats the weak signal.
	osUnlistedMalformed
)

// osUnlistedFactOutcome reads one fact marked ReleaseUnlisted for observed
// (#9806, ruling decision 1). It never yields an exact or a known-fixed
// answer: the fact carries no release evidence for the host.
func osUnlistedFactOutcome(
	observed string,
	pkg supplychainmodel.AffectedPackage,
	compare versionCompareFunc,
) osUnlistedOutcome {
	contains, _, malformed := osPackageRangeSignals(observed, pkg, compare)
	if contains || !malformed {
		return osUnlistedPossible
	}
	return osUnlistedMalformed
}

// osVersionListNamesObserved reports whether an OSV OS fact's exact-version
// list names observed. Such a fact carries no release evidence, so the match
// is possibly_affected and must be read before the single-fix shortcut (#9806
// PR-2b, ruling decision 6). Unreadable entries are left to the later scan.
//
// When some fact names no range for the host's line (ReleaseUnlisted), a list
// on a fact that does carry release evidence counts too: that fact settles the
// host exactly in the scan below, and the weak not-in-advisory read must not
// mask it. ReleaseUnlisted is set only on a tagged fact, so with untagged
// facts this keeps the earlier rule unchanged.
func osVersionListNamesObserved(observed string, pkgs []supplychainmodel.AffectedPackage, compare versionCompareFunc) bool {
	anyUnlisted := false
	for _, pkg := range pkgs {
		if pkg.ReleaseUnlisted {
			anyUnlisted = true
			break
		}
	}
	for _, pkg := range pkgs {
		if !anyUnlisted && osAffectedFactCarriesReleaseEvidence(pkg) {
			continue
		}
		for _, candidate := range pkg.AffectedVersions {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			if cmp, ok := compare(observed, candidate); ok && cmp == 0 {
				return true
			}
		}
	}
	return false
}
