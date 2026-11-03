// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// unlistedMissingEvidence is the missing evidence the #9806 PR-2b ruling sets
// for a tagged OSV fact whose ranges name no line for the installed row.
const unlistedMissingEvidence = "the advisory lists no range for the installed package's release line"

func osvAlpineCurlFact(ranges ...supplychainmodel.AffectedRange) supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "ALPINE-CVE-2025-13034", Source: "osv", AdvisoryID: "ALPINE-CVE-2025-13034",
		PackageID: "os://alpine/curl", Ecosystem: "os", Name: "curl",
		PURL:           "pkg:apk/alpine/curl",
		AffectedRanges: ranges,
		FixedVersions:  rangeFixedVersions(ranges),
	}
}

func taggedIntroducedRange(introduced, fixed string, releases ...string) supplychainmodel.AffectedRange {
	return supplychainmodel.AffectedRange{
		Kind:     "ECOSYSTEM",
		Events:   []supplychainmodel.AffectedRangeEvent{{Introduced: introduced}, {Fixed: fixed}},
		Releases: releases,
	}
}

func osvDebianCurlFactWith(ranges ...supplychainmodel.AffectedRange) supplychainmodel.AffectedPackage {
	fact := osvDebianCurlFact()
	fact.AffectedVersions = nil
	fact.AffectedRanges = ranges
	fact.FixedVersions = rangeFixedVersions(ranges)
	return fact
}

// classifyTaggedOSFact runs the production chain a joined OSV OS fact takes:
// release selection against the installed row, then the version matcher fed
// the same single fixed version the finding builder derives.
func classifyTaggedOSFact(
	fact supplychainmodel.AffectedPackage,
	row supplychainmodel.OSPackage,
	observed string,
) (supplychainmodel.AffectedPackage, supplyChainVersionMatchDecision) {
	pkgs := selectOSAdvisoryRelease([]supplychainmodel.AffectedPackage{fact}, row)
	decision := evaluateSupplyChainVersionMatch("os", observed, "", singleFixedVersion(pkgs[0].FixedVersions), pkgs)
	return pkgs[0], decision
}

// TestUnlistedReleaseVerdict pins the #9806 PR-2b ruling (decisions 1 and 2):
// a tagged OSV fact that names no range for the installed row's release line
// never yields affected_exact and never carries release evidence. A listed
// range that contains the host version reads possibly_affected with the single
// closed reason os_release_unlisted. When no range contains it and every
// listed fix is at or below it, dpkg reads known-fixed under a named reason,
// and apk does so only when the host's upstream version alone settles it,
// because an apk -rN counter is per branch and orders nothing across lines.
func TestUnlistedReleaseVerdict(t *testing.T) {
	t.Parallel()

	alpine := func(version string) supplychainmodel.OSPackage {
		return supplychainmodel.OSPackage{Distro: "alpine", DistroVersion: version}
	}
	bullseye := supplychainmodel.OSPackage{Distro: "debian", DistroVersion: "11"}
	curlRanges := func() []supplychainmodel.AffectedRange {
		return []supplychainmodel.AffectedRange{
			taggedRange("8.14.1-r3", "alpine:3.21"),
			taggedRange("8.14.1-r4", "alpine:3.22"),
		}
	}

	for _, tc := range []struct {
		name       string
		fact       supplychainmodel.AffectedPackage
		row        supplychainmodel.OSPackage
		observed   string
		wantStatus SupplyChainImpactStatus
		wantReason string
	}{
		{
			name: "dpkg listed range contains the unlisted host version",
			fact: osvDebianCurlFact(), row: bullseye, observed: "7.74.0-1.3+deb11u7",
			wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnlisted,
		},
		{
			name: "dpkg host above every listed fix is known fixed",
			fact: osvDebianCurlFact(), row: bullseye, observed: "9.0.0-1",
			wantStatus: SupplyChainImpactNotAffectedKnownFixed, wantReason: supplyChainVersionReasonDPKGKnownFixedUnlistedRelease,
		},
		{
			name: "dpkg host at a listed fix is known fixed",
			fact: osvDebianCurlFact(), row: bullseye, observed: "8.3.0-1",
			wantStatus: SupplyChainImpactNotAffectedKnownFixed, wantReason: supplyChainVersionReasonDPKGKnownFixedUnlistedRelease,
		},
		{
			// Ruling decision 6: an exact-version list that names the host
			// version is read before any known-fixed settlement, listed or
			// not. The list is no release evidence, so the host stays
			// possibly affected under the unlisted reason (#9806 PR-2b review).
			name: "dpkg a version list naming the host beats the unlisted known-fixed settlement",
			fact: func() supplychainmodel.AffectedPackage {
				fact := osvDebianCurlFact()
				fact.AffectedVersions = []string{"9.0.0-1"}
				return fact
			}(),
			row: bullseye, observed: "9.0.0-1",
			wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnlisted,
		},
		{
			name: "dpkg a listed fix above the host is not extrapolated",
			fact: osvDebianCurlFactWith(
				taggedRange("7.88.1-10+deb12u3", "debian:12"),
				taggedIntroducedRange("8.5.0-1", "9.0.0-1", "debian:13"),
			),
			row: bullseye, observed: "8.0.0-1",
			wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnlisted,
		},
		{
			name: "extrapolate apk by revision: same upstream, higher revision",
			fact: osvAlpineCurlFact(curlRanges()...), row: alpine("3.23.0"), observed: "8.14.1-r5",
			wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnlisted,
		},
		{
			name: "apk host on a newer upstream is known fixed",
			fact: osvAlpineCurlFact(curlRanges()...), row: alpine("3.23.0"), observed: "8.15.0-r0",
			wantStatus: SupplyChainImpactNotAffectedKnownFixed, wantReason: supplyChainVersionReasonAPKKnownFixedUnlistedRelease,
		},
		{
			name: "apk a version list naming the host beats the unlisted known-fixed settlement",
			fact: func() supplychainmodel.AffectedPackage {
				fact := osvAlpineCurlFact(curlRanges()...)
				fact.AffectedVersions = []string{"8.15.0-r0"}
				return fact
			}(),
			row: alpine("3.23.0"), observed: "8.15.0-r0",
			wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnlisted,
		},
		{
			name: "apk host inside a listed range is possibly affected",
			fact: osvAlpineCurlFact(curlRanges()...), row: alpine("3.20.1"), observed: "8.9.1-r1",
			wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnlisted,
		},
		{
			name: "apk fix at revision zero settles a same-upstream host",
			fact: osvAlpineCurlFact(
				taggedRange("8.14.1-r0", "alpine:3.21"),
				taggedRange("8.14.1-r0", "alpine:3.22"),
			),
			row: alpine("3.23.0"), observed: "8.14.1-r2",
			wantStatus: SupplyChainImpactNotAffectedKnownFixed, wantReason: supplyChainVersionReasonAPKKnownFixedUnlistedRelease,
		},
		{
			name: "apk one backport fix among revision-zero fixes blocks the extrapolation",
			fact: osvAlpineCurlFact(
				taggedRange("8.14.1-r0", "alpine:3.21"),
				taggedRange("8.14.1-r4", "alpine:3.22"),
			),
			row: alpine("3.23.0"), observed: "8.14.1-r5",
			wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonOSReleaseUnlisted,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			selected, decision := classifyTaggedOSFact(tc.fact, tc.row, tc.observed)
			if selected.ReleaseLine != "" {
				t.Fatalf("ReleaseLine = %q, want empty for an unlisted host", selected.ReleaseLine)
			}
			if !selected.ReleaseUnlisted {
				t.Fatalf("fact not marked ReleaseUnlisted")
			}
			if decision.Status == SupplyChainImpactAffectedExact {
				t.Fatalf("unlisted host read affected_exact (%s)", decision.Reason)
			}
			if decision.Status != tc.wantStatus || decision.Reason != tc.wantReason {
				t.Fatalf("decision = %s/%s, want %s/%s", decision.Status, decision.Reason, tc.wantStatus, tc.wantReason)
			}
			if decision.Status == SupplyChainImpactPossiblyAffected &&
				(len(decision.MissingEvidence) != 1 || decision.MissingEvidence[0] != unlistedMissingEvidence) {
				t.Fatalf("missing evidence = %v, want [%q]", decision.MissingEvidence, unlistedMissingEvidence)
			}
		})
	}
}

// TestUnlistedReleaseSettlesFinding pins that an unlisted release, like an
// unverified one, settles an installed-package finding when no SBOM path
// competes: the package is installed, only the advisory's line is unknown.
func TestUnlistedReleaseSettlesFinding(t *testing.T) {
	t.Parallel()

	decision := possiblyAffectedDecision(supplyChainVersionReasonOSReleaseUnlisted, []string{unlistedMissingEvidence})
	if !osPackageSettlesFinding(decision, false) {
		t.Fatalf("os_release_unlisted must settle an installed row without an SBOM path")
	}
	if osPackageSettlesFinding(decision, true) {
		t.Fatalf("os_release_unlisted must not settle over an SBOM component path")
	}
}

// TestOSVVersionListBeatsSingleFixShortcut pins review-3 P3-a carried into
// #9806 PR-2b (ruling decision 6): an OSV OS fact never carries release
// evidence, so an exact-version list that names the observed version must be
// read before the single-fix shortcut can call it known-fixed.
func TestOSVVersionListBeatsSingleFixShortcut(t *testing.T) {
	t.Parallel()

	fact := supplychainmodel.AffectedPackage{
		CVEID: "DEBIAN-CVE-2099-0100", Source: "osv", AdvisoryID: "DEBIAN-CVE-2099-0100",
		PackageID: "os://debian/curl", Ecosystem: "os", Name: "curl",
		PURL:             "pkg:deb/debian/curl",
		AffectedVersions: []string{"1.0-3"},
		FixedVersions:    []string{"1.0-2"},
	}
	pkgs := []supplychainmodel.AffectedPackage{fact}
	decision := evaluateSupplyChainVersionMatch("os", "1.0-3", "", singleFixedVersion(pkgs[0].FixedVersions), pkgs)
	if decision.Status != SupplyChainImpactPossiblyAffected || decision.Reason != supplyChainVersionReasonOSReleaseUnverified {
		t.Fatalf("decision = %s/%s, want possibly_affected/%s", decision.Status, decision.Reason, supplyChainVersionReasonOSReleaseUnverified)
	}
	// A version the list does not name keeps the shortcut.
	decision = evaluateSupplyChainVersionMatch("os", "1.0-4", "", singleFixedVersion(pkgs[0].FixedVersions), pkgs)
	if decision.Status != SupplyChainImpactNotAffectedKnownFixed {
		t.Fatalf("version outside the list: decision = %s/%s, want known fixed", decision.Status, decision.Reason)
	}
}
