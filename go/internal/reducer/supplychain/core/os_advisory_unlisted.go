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

// osReleaseEvidenceListNamesObserved reports whether an OS advisory fact that
// carries release evidence has an exact-version list entry equal to observed.
// A vendor fact (Debian, Ubuntu, Alpine) is a per-release record, and an entry
// equal to the host's exact installed version names the host as affected. That
// is exact evidence, and it outranks whatever an untagged OSV fact's ranges or
// fixes say: those are the union of every release's (#9929).
//
// The match is on the exact version string only. A vendor fact carries no
// release or distro field and the join drops the purl qualifiers, so nothing
// checks that the fact was published for the host's own release line; a fact
// for another line whose list names the same version string would match too
// (the same limit a vendor-only fact already had). Scoping vendor facts to the
// host's line needs a data-model change tracked in the follow-up named in the
// #9929 evidence note. An unreadable entry never counts as a match;
// osVersionListBlocksKnownFixed keeps it from being dropped silently.
func osReleaseEvidenceListNamesObserved(observed string, pkgs []supplychainmodel.AffectedPackage, compare versionCompareFunc) bool {
	for _, pkg := range pkgs {
		if !osAffectedFactCarriesReleaseEvidence(pkg) {
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

// osVersionListBlocksKnownFixed reports whether an exact-version list keeps a
// known-fixed shortcut from answering for observed, so the full scan reads the
// lists first. Two things block it:
//
//   - An entry on any fact that equals observed. On a fact without release
//     evidence (untagged OSV) the match is possibly_affected, never exact
//     (#9806 PR-2b, ruling decision 6). On a fact with release evidence the
//     match is exact and osReleaseEvidenceListNamesObserved answers before
//     this runs.
//   - An unreadable entry on a fact with release evidence (#9929). It might
//     have been the host's version, so a shortcut must not claim known fixed
//     past it; the scan then reports the finding fail-closed as
//     malformed_advisory_range. An unreadable entry on a fact without release
//     evidence keeps the earlier rule and is left to the later scan, which a
//     shortcut can still skip.
func osVersionListBlocksKnownFixed(observed string, pkgs []supplychainmodel.AffectedPackage, compare versionCompareFunc) bool {
	for _, pkg := range pkgs {
		evidence := osAffectedFactCarriesReleaseEvidence(pkg)
		for _, candidate := range pkg.AffectedVersions {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				continue
			}
			cmp, ok := compare(observed, candidate)
			if !ok {
				if evidence {
					return true
				}
				continue
			}
			if cmp == 0 {
				return true
			}
		}
	}
	return false
}
