// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// osVersionFamily names the version scheme an OS advisory is matched with. It
// selects the known-fixed rule for a release the advisory does not list.
type osVersionFamily int

const (
	// osFamilyDPKG covers Debian and Ubuntu: the release is stamped into the
	// version (+debNuM, ubuntuN.M), so versions order across release lines.
	osFamilyDPKG osVersionFamily = iota
	// osFamilyAPK covers Alpine: -rN is a per-branch counter, so a revision
	// orders nothing across release lines.
	osFamilyAPK
)

// unlistedKnownFixedReason is the named known-fixed reason for a host on a
// release the advisory does not list.
func (f osVersionFamily) unlistedKnownFixedReason() string {
	if f == osFamilyAPK {
		return supplyChainVersionReasonAPKKnownFixedUnlistedRelease
	}
	return supplyChainVersionReasonDPKGKnownFixedUnlistedRelease
}

// osUnlistedOutcome is what one ReleaseUnlisted fact says about the host.
type osUnlistedOutcome int

const (
	// osUnlistedSilent: no listed range contains the host version and the
	// fixes do not settle it (for example every range starts above it).
	osUnlistedSilent osUnlistedOutcome = iota
	// osUnlistedPossible: a listed range contains the host version, or a fix
	// does not provably sit at or below it. The host's own line is unknown.
	osUnlistedPossible
	// osUnlistedFixed: no listed range contains the host version and every
	// listed fix is at or below it, in a scheme that orders across lines.
	osUnlistedFixed
	// osUnlistedMalformed: a range or fix could not be compared.
	osUnlistedMalformed
)

// osUnlistedFactOutcome reads one fact marked ReleaseUnlisted for observed
// (#9806 PR-2b, ruling decisions 1 and 2). It never yields an exact answer:
// the fact carries no release evidence for the host.
func osUnlistedFactOutcome(
	observed string,
	pkg supplychainmodel.AffectedPackage,
	family osVersionFamily,
	compare versionCompareFunc,
) osUnlistedOutcome {
	contains, fixedBelow, malformed := osPackageRangeSignals(observed, pkg, compare)
	switch {
	case contains:
		return osUnlistedPossible
	case malformed:
		return osUnlistedMalformed
	case !fixedBelow:
		return osUnlistedSilent
	}
	for _, affectedRange := range pkg.AffectedRanges {
		if !strings.EqualFold(affectedRange.Kind, "ECOSYSTEM") {
			continue
		}
		for _, event := range affectedRange.Events {
			fixed := strings.TrimSpace(event.Fixed)
			if fixed == "" {
				continue
			}
			atLeast, valid := versionAtLeast(observed, fixed, compare)
			if !valid {
				return osUnlistedMalformed
			}
			if !atLeast {
				return osUnlistedPossible
			}
			if family == osFamilyAPK && !apkUpstreamSettlesFix(observed, fixed) {
				return osUnlistedPossible
			}
		}
	}
	return osUnlistedFixed
}

// apkUpstreamSettlesFix reports whether the host's upstream version alone
// proves it carries an Alpine fix "U-rN" it cannot order by revision: the
// host's upstream is strictly above U, or equals U and N is 0 (the upstream
// release itself carries the fix). A higher -rN on the same U proves nothing,
// because -rN counts rebuilds per branch (the live Alpine pull lists the same
// upstream fix at different -r on different branches). It reuses
// parseAPKVersion's split of base and revision.
func apkUpstreamSettlesFix(observed, fixed string) bool {
	host, ok := parseAPKVersion(observed)
	if !ok {
		return false
	}
	fix, ok := parseAPKVersion(fixed)
	if !ok {
		return false
	}
	cmp := compareRPMSegments(host.main, fix.main)
	if cmp == 0 {
		cmp = compareAPKSuffixes(host.suffixes, fix.suffixes)
	}
	return cmp > 0 || (cmp == 0 && fix.release == 0)
}

// osVersionListNamesObserved reports whether an OSV OS fact's exact-version
// list names observed. Such a fact carries no release evidence, so the match
// is possibly_affected and must be read before the single-fix shortcut (#9806
// PR-2b, ruling decision 6). Unreadable entries are left to the later scan.
//
// When some fact names no range for the host's line (ReleaseUnlisted), a list
// on a fact that does carry release evidence counts too. That fact settles the
// host exactly in the scan below, so a known-fixed answer built from the
// unlisted fact's ranges must not preempt it. ReleaseUnlisted is set only on a
// tagged fact, so with untagged facts this keeps the earlier rule unchanged.
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
