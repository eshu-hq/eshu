// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// deb11u builds a Debian 11 curl version ending in the given suffix.
func deb11u(suffix string) string { return "7.74.0-1.3+deb11u" + suffix }

// TestLiveRecordHoldingTheInstalledVersionDecidesTheFinding: when two live
// records for a CVE disagree, the record whose range holds the installed version
// names the finding and supplies its fix and range. Among holders the highest fix
// wins, because a lower fix leaves the host inside the other record's range.
//
// Measured with host u11 at 9c99579a2a under all five orderings: dla_u10_dsa_u12
// read affected_exact naming DLA-1000-1, "upgrade to u10" for a host on u11, which
// is the record that says the host is fixed. reversed read DLA u12, right only by
// AdvisoryID sort order. both_hold read DLA u12, the lower fix. Base e3d816172a
// read two findings on the first shape, DLA not_affected_known_fixed and DSA
// affected_exact, so the base carried the right fix on its DSA row.
func TestLiveRecordHoldingTheInstalledVersionDecidesTheFinding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		host         string
		dlaKey       string
		dsaKey       string
		wantStatus   SupplyChainImpactStatus
		wantAdvisory string
		wantFix      string
	}{
		{"dla_u10_dsa_u12", deb11u("11"), "built-holding-dla-bullseye-u10", "built-holding-dsa-bullseye-u12", SupplyChainImpactAffectedExact, "DSA-2000-1", deb11u("12")},
		{"reversed", deb11u("11"), "built-holding-dla-bullseye-u12", "built-holding-dsa-bullseye-u10", SupplyChainImpactAffectedExact, "DLA-1000-1", deb11u("12")},
		{"both_hold", deb11u("11"), "built-holding-dla-bullseye-u12", "built-holding-dsa-bullseye-u14", SupplyChainImpactAffectedExact, "DSA-2000-1", deb11u("14")},
		// Rule 3: neither holds, so the first sorted record names it as before.
		{"neither_holds", deb11u("11"), "built-holding-dla-bullseye-u8", "built-holding-dsa-bullseye-u10", SupplyChainImpactNotAffectedKnownFixed, "DLA-1000-1", deb11u("8")},
		// u10 beats u9 by dpkg order and loses by string order, and DLA sorts first.
		{"highest_by_dpkg_order", deb11u("8"), "built-holding-dla-bullseye-u9", "built-holding-dsa-bullseye-u10", SupplyChainImpactAffectedExact, "DSA-2000-1", deb11u("10")},
		// A tilde sorts below the bare version, so u10~bpo is still inside <u10.
		{"holding_by_dpkg_tilde", deb11u("10~bpo11+1"), "built-holding-dla-bullseye-u9", "built-holding-dsa-bullseye-u10", SupplyChainImpactAffectedExact, "DSA-2000-1", deb11u("10")},
		// An epoch beats the upstream version, so 1:7.0 is the higher fix.
		{"highest_by_dpkg_epoch", deb11u("8"), "built-holding-dla-bullseye-epoch", "built-holding-dsa-bullseye-u12", SupplyChainImpactAffectedExact, "DLA-1000-1", "1:7.0.0-1"},
		{"equal_fixes_keep_sort_order", deb11u("11"), "built-holding-dla-bullseye-u12", "built-holding-dsa-bullseye-u12", SupplyChainImpactAffectedExact, "DLA-1000-1", deb11u("12")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := append(upstreamEnvelopes(t, nil,
				tc.dlaKey, tc.dsaKey,
			), debianCurl("11", tc.host).envelope())
			wantRange := ">=0 <" + tc.wantFix
			for order, shuffled := range orderings(envelopes) {
				findings := BuildSupplyChainImpactFindings(shuffled)
				if len(findings) != 1 {
					t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
				}
				got := findings[0]
				assertSupplyChainImpactStatus(t, got, tc.wantStatus)
				if got.AdvisoryID != tc.wantAdvisory {
					t.Errorf("%s: AdvisoryID = %q, want %q", order, got.AdvisoryID, tc.wantAdvisory)
				}
				if got.FixedVersion != tc.wantFix {
					t.Errorf("%s: FixedVersion = %q, want %q", order, got.FixedVersion, tc.wantFix)
				}
				if got.VulnerableRange != wantRange {
					t.Errorf("%s: VulnerableRange = %q, want %q", order, got.VulnerableRange, wantRange)
				}
				if len(got.AdvisorySources) != 2 {
					t.Errorf("%s: AdvisorySources = %#v, want both records kept", order, got.AdvisorySources)
				}
			}
		})
	}
}

// vendorSourced stamps a converted record's facts as published by the vendor feed
// rather than OSV. A vendor fact whose purl names the host's line carries release
// evidence (#9945), and its exact-version list settles the verdict (#9929).
func vendorSourced(envelopes []facts.Envelope, source string) []facts.Envelope {
	for at := range envelopes {
		envelopes[at].Payload["source"] = source
	}
	return envelopes
}

// TestHoldingRecordWithNoFixDoesNotBorrowAnotherRecordsFix: the holder's own fix
// is the only one offered. Falling through to the next record would publish
// "upgrade to u10" for a host on u11, which is the defect this rule removes.
func TestHoldingRecordWithNoFixDoesNotBorrowAnotherRecordsFix(t *testing.T) {
	t.Parallel()

	host := deb11u("11")
	envelopes := append(
		vendorSourced(upstreamEnvelopes(t, nil, "built-holding-dla-debian11-u10"), "debian"),
		vendorSourced(upstreamEnvelopes(t, nil, "built-holding-listing"), "debian")...)
	envelopes = append(envelopes, debianCurl("11", host).envelope())
	for order, shuffled := range orderings(envelopes) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		if len(findings) != 1 {
			t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
		}
		got := findings[0]
		assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
		if got.AdvisoryID != "DSA-2000-1" {
			t.Errorf("%s: AdvisoryID = %q, want DSA-2000-1, the record listing the host", order, got.AdvisoryID)
		}
		if got.FixedVersion != "" || got.FixedVersionSource != "" {
			t.Errorf("%s: fix = %q from %q, want none: the holder names no fix", order, got.FixedVersion, got.FixedVersionSource)
		}
	}
}

// TestVendorRecordWithNoVerifiedLineDoesNotHold: a vendor record that sorts first
// but whose purl names another release, or a line the reducer cannot read, has no
// release evidence (#9945), so its range cannot decide the host and its fix is not
// offered. The record verified for the host names the finding.
func TestVendorRecordWithNoVerifiedLineDoesNotHold(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, dlaKey string }{
		{"another release", "built-holding-dla-debian12"},
		{"an unreadable line", "built-holding-dla-bullseye-u14"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := append(
				vendorSourced(upstreamEnvelopes(t, nil, tc.dlaKey), "debian"),
				vendorSourced(upstreamEnvelopes(t, nil, "built-holding-dsa-u12"), "debian")...)
			envelopes = append(envelopes, debianCurl("11", deb11u("11")).envelope())
			for order, shuffled := range orderings(envelopes) {
				findings := BuildSupplyChainImpactFindings(shuffled)
				if len(findings) != 1 {
					t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
				}
				got := findings[0]
				assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
				if got.AdvisoryID != "DSA-2000-1" || got.FixedVersion != deb11u("12") {
					t.Errorf("%s: advisory/fix = %q/%q, want DSA-2000-1 and u12", order, got.AdvisoryID, got.FixedVersion)
				}
			}
		})
	}
}

// TestUntaggedRecordsKeepTheFirstSortedName is rule 3's control: an OSV fact with
// no release evidence cannot settle exact, so nothing holds, the verdict stays
// possibly_affected, and the name and fix are what they were before this rule.
func TestUntaggedRecordsKeepTheFirstSortedName(t *testing.T) {
	t.Parallel()

	envelopes := append(upstreamEnvelopes(t, nil,
		"built-holding-untagged-dla", "built-holding-untagged-dsa",
	), debianCurl("11", deb11u("11")).envelope())
	for order, shuffled := range orderings(envelopes) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		if len(findings) != 1 {
			t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
		}
		got := findings[0]
		assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
		if got.MatchReason != supplyChainVersionReasonOSReleaseUnverified {
			t.Errorf("%s: MatchReason = %q, want %q", order, got.MatchReason, supplyChainVersionReasonOSReleaseUnverified)
		}
		if got.AdvisoryID != "DLA-1000-1" || got.FixedVersion != deb11u("10") {
			t.Errorf("%s: advisory/fix = %q/%q, want DLA-1000-1 and its fix", order, got.AdvisoryID, got.FixedVersion)
		}
	}
}

// TestLanguageFindingFixStaysFirstSorted is the #9693 control: the holding rule is
// OS-class only, so an npm group's fix comes from the first sorted source exactly
// as it did, even when a later source's range is the one holding the version.
func TestLanguageFindingFixStaysFirstSorted(t *testing.T) {
	t.Parallel()

	const (
		cve       = "CVE-2021-23337"
		packageID = "npm://registry.npmjs.org/lodash"
	)
	ghsaCVE := vulnerabilityCVEFactWithProvenance("ghsa-cve", cve, "osv", "GHSA-35jh-r3h4-6jhm", 7.2, "", "HIGH", "2026-10-01T00:00:00Z")
	nvdCVE := vulnerabilityCVEFactWithProvenance("nvd-cve", cve, "nvd", cve, 7.5, "", "HIGH", "2026-10-01T00:00:00Z")
	ghsaPkg := vulnerabilityAffectedPackageFact("ghsa-pkg", cve, packageID, "npm", "lodash", "4.17.20", "4.17.20")
	ghsaPkg.Payload["advisory_id"] = "GHSA-35jh-r3h4-6jhm"
	ghsaPkg.Payload["source"] = "osv"
	nvdPkg := vulnerabilityAffectedPackageFact("nvd-pkg", cve, packageID, "npm", "lodash", "4.17.20", "4.17.21")
	nvdPkg.Payload["advisory_id"] = cve
	nvdPkg.Payload["source"] = "nvd"
	consumption := packageConsumptionFactWithRange("consumption-1", packageID, testImpactRepositoryID, "4.17.20")

	for order, shuffled := range orderings([]facts.Envelope{ghsaCVE, nvdCVE, ghsaPkg, nvdPkg, consumption}) {
		findings := BuildSupplyChainImpactFindings(shuffled)
		if len(findings) != 1 {
			t.Fatalf("%s: findings = %d, want 1: %#v", order, len(findings), findings)
		}
		if got := findings[0]; got.AdvisoryID != "GHSA-35jh-r3h4-6jhm" || got.FixedVersion != "4.17.20" {
			t.Errorf("%s: advisory/fix = %q/%q, want the GHSA and its own fix", order, got.AdvisoryID, got.FixedVersion)
		}
	}
}

// TestDerivedPathNamesTheHoldingRecord: the per-target pass classifies through the
// same seam, so a cross-record disagreement reads the holder's name and fix, not
// advisory_fixed_version_conflict, which is for two facts of one advisory.
func TestDerivedPathNamesTheHoldingRecord(t *testing.T) {
	t.Parallel()

	var advisories []facts.Envelope
	for _, envelopes := range [][]facts.Envelope{
		convertedOSVFixture(t, "built-holding-dla-bullseye-u10", false),
		convertedOSVFixture(t, "built-holding-dsa-bullseye-u12", false),
	} {
		kept, _ := authorizedAdvisories(envelopes)
		advisories = append(advisories, kept...)
	}
	target := authorizedOSVTarget(0, advisories, authorizedOSVAuthority(advisories), debianCurl("11", deb11u("11")))
	findings, err := BuildAuthorizedScanImpactFindings(nil, []AuthorizedScanImpactEvidence{target})
	if err != nil {
		t.Fatalf("BuildAuthorizedScanImpactFindings: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(findings), findings)
	}
	got := findings[0]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.AdvisoryID != "DSA-2000-1" || got.FixedVersion != deb11u("12") {
		t.Errorf("advisory/fix = %q/%q, want DSA-2000-1 and u12", got.AdvisoryID, got.FixedVersion)
	}
}

// TestConflictingInstalledVersionsDoNotPickAHolder: when the SBOM and the scanned
// row disagree the installed version is unknown and the verdict says so, so no
// record can be said to hold it and the name stays with the first sorted record.
func TestConflictingInstalledVersionsDoNotPickAHolder(t *testing.T) {
	t.Parallel()

	const (
		cve       = "CVE-2023-38039"
		packageID = "os://debian/curl"
		digest    = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	sbomRow, scanRow := deb11u("11"), deb11u("13")
	// On the scanned row alone the DSA holds and the DLA says fixed, so the DSA would
	// name the finding if the row counted.
	input := append(
		convertedOSVFixture(t, "built-holding-dla-bullseye-u12", false),
		convertedOSVFixture(t, "built-holding-dsa-bullseye-u14", false)...)
	input = append(input, fleetOSRow(debianCurl("11", scanRow), "image://conflict", "generation-conflict", digest)...)
	input = append(input,
		sbomComponentImpactFactWithPackageIDVersion("component-conflict", "doc-conflict", "pkg:deb/debian/curl@"+sbomRow, packageID, sbomRow),
		sbomAttachmentImpactFact("attachment-conflict", "doc-conflict", digest),
		containerImageIdentityImpactFact("image-conflict", digest, testImpactRepositoryID))
	got := supplyChainImpactFindingForDigest(t, BuildSupplyChainImpactFindings(input), digest)
	if got.MatchReason != supplyChainVersionReasonSBOMVersionConflict {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonSBOMVersionConflict)
	}
	if got.AdvisoryID != "DLA-1000-1" {
		t.Errorf("AdvisoryID = %q, want DLA-1000-1: an unknown installed version cannot pick a holder", got.AdvisoryID)
	}
}

// TestDecidingObservation pins the choice among observations, including the edges
// no converted record reaches: liveness in the holder set and what a missing fix
// does to the ordering.
func TestDecidingObservation(t *testing.T) {
	t.Parallel()

	const withdrawn = "2026-01-02T00:00:00Z"
	holder := func(id string, rank int) AdvisoryProvenanceObservation {
		return AdvisoryProvenanceObservation{AdvisoryID: id, AffectedFactID: "p-" + id, holdsInstalled: true, holdingFixRank: rank}
	}
	plain := func(id string) AdvisoryProvenanceObservation {
		return AdvisoryProvenanceObservation{AdvisoryID: id, AffectedFactID: "p-" + id}
	}
	retractedHolder := holder("A", 2)
	retractedHolder.WithdrawnAt = withdrawn

	tests := []struct {
		name        string
		sorted      []AdvisoryProvenanceObservation
		wantID      string
		wantHolding bool
	}{
		{"one holder beats the record sorted first", []AdvisoryProvenanceObservation{plain("A"), holder("B", 1)}, "B", true},
		{"the highest fix wins among holders", []AdvisoryProvenanceObservation{holder("A", 1), holder("B", 2)}, "B", true},
		{"an equal fix keeps sort order", []AdvisoryProvenanceObservation{holder("A", 1), holder("B", 1)}, "A", true},
		{"a holder with no fix keeps its place ahead of a later fix", []AdvisoryProvenanceObservation{holder("A", 0), holder("B", 1)}, "A", true},
		{"a later holder with no fix does not displace a fix", []AdvisoryProvenanceObservation{holder("A", 1), holder("B", 0)}, "A", true},
		{"a retracted holder does not name it", []AdvisoryProvenanceObservation{retractedHolder, plain("B")}, "B", false},
		{"a retracted holder loses to a live holder", []AdvisoryProvenanceObservation{retractedHolder, holder("B", 1)}, "B", true},
		{"no holder falls back to the first live fact", []AdvisoryProvenanceObservation{plain("A"), plain("B")}, "A", false},
		{"no fact at all names nothing", []AdvisoryProvenanceObservation{{AdvisoryID: "A"}}, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			at, holding := decidingObservation(tc.sorted)
			got := ""
			if at >= 0 {
				got = tc.sorted[at].AdvisoryID
			}
			if got != tc.wantID || holding != tc.wantHolding {
				t.Errorf("deciding = %q holding %v, want %q holding %v", got, holding, tc.wantID, tc.wantHolding)
			}
		})
	}
}

// TestOSHoldingFacts pins which facts hold an installed version, and that the
// comparator is the verdict's own for each package family.
func TestOSHoldingFacts(t *testing.T) {
	t.Parallel()

	rangeTo := func(fixed string) []supplychainmodel.AffectedRange {
		return []supplychainmodel.AffectedRange{{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: fixed}}}}
	}
	// Every non-OSV fact here is one a join verified, which is what ReleaseLine means.
	fact := func(id, source, fixed string) supplychainmodel.AffectedPackage {
		pkg := supplychainmodel.AffectedPackage{FactID: id, AdvisoryID: id, Source: source, AffectedRanges: rangeTo(fixed)}
		if source != "osv" {
			pkg.ReleaseLine = "host:line"
		}
		return pkg
	}
	rpmFact := func(id, fixed string) supplychainmodel.AffectedPackage {
		pkg := fact(id, "redhat", fixed)
		pkg.PURL = "pkg:rpm/redhat/curl"
		return pkg
	}
	listing := fact("list", "debian", "9.0-1")
	listing.AffectedRanges = nil
	listing.AffectedVersions = []string{"5.0-1"}
	unlisted := fact("unlisted", "debian", "9.0-1")
	unlisted.ReleaseLine, unlisted.ReleaseUnlisted = "", true
	unverified := fact("unverified", "debian", "9.0-1")
	unverified.ReleaseLine = ""
	ambiguous := fact("ambiguous", "debian", "4.0-1")
	ambiguous.AffectedRanges = append(ambiguous.AffectedRanges, rangeTo("6.0-1")...)
	windows := fact("windows", "debian", "4.0-1")
	windows.AffectedRanges[0].Events = []supplychainmodel.AffectedRangeEvent{
		{Introduced: "0"}, {Fixed: "4.0-1"}, {Introduced: "4.5-1"}, {Fixed: "6.0-1"}, {Introduced: "7.0-1"}, {Fixed: "9.0-1"},
	}

	tests := []struct {
		name      string
		ecosystem string
		observed  string
		pkgs      []supplychainmodel.AffectedPackage
		want      []osHoldingFact
	}{
		{"one fact has nothing to choose", "debian", "5.0-1", []supplychainmodel.AffectedPackage{fact("a", "debian", "6.0-1")}, nil},
		{"facts at or below the version do not hold", "debian", "5.0-1", []supplychainmodel.AffectedPackage{fact("a", "debian", "4.0-1"), fact("b", "debian", "5.0-1")}, nil},
		{
			"each holder ranks by its fix", "debian", "5.0-1",
			[]supplychainmodel.AffectedPackage{fact("a", "debian", "7.0-1"), fact("b", "debian", "6.0-1"), fact("c", "debian", "4.0-1")},
			[]osHoldingFact{{factID: "a", fixRank: 2}, {factID: "b", fixRank: 1}},
		},
		{
			"equal fixes share a rank", "debian", "5.0-1",
			[]supplychainmodel.AffectedPackage{fact("a", "debian", "6.0-1"), fact("b", "debian", "6.0-1")},
			[]osHoldingFact{{factID: "a", fixRank: 1}, {factID: "b", fixRank: 1}},
		},
		{
			"an exact list holds with no fix to rank", "debian", "5.0-1",
			[]supplychainmodel.AffectedPackage{listing, fact("a", "debian", "6.0-1")},
			[]osHoldingFact{{factID: "list"}, {factID: "a", fixRank: 1}},
		},
		{"an untagged OSV fact does not hold", "debian", "5.0-1", []supplychainmodel.AffectedPackage{fact("a", "osv", "6.0-1"), fact("b", "osv", "7.0-1")}, nil},
		{"a range holding the version beside a fix below it does not hold", "debian", "5.0-1", []supplychainmodel.AffectedPackage{ambiguous, fact("a", "debian", "7.0-1")}, []osHoldingFact{{factID: "a", fixRank: 1}}},
		{"an unlisted fact does not hold", "debian", "5.0-1", []supplychainmodel.AffectedPackage{unlisted, fact("a", "debian", "4.0-1")}, nil},
		{"an unverified vendor fact does not hold", "debian", "5.0-1", []supplychainmodel.AffectedPackage{unverified, fact("a", "debian", "4.0-1")}, nil},
		{"an unreadable installed version holds nothing", "apk", "not a version!", []supplychainmodel.AffectedPackage{fact("a", "alpine", "6.0-r1"), fact("b", "alpine", "7.0-r1")}, nil},
		{"an ecosystem with no matcher holds nothing", "suse", "5.0-1", []supplychainmodel.AffectedPackage{fact("a", "suse", "6.0-1"), fact("b", "suse", "7.0-1")}, nil},
		{
			"the os ecosystem takes its family from the purl", "os", "5.0",
			[]supplychainmodel.AffectedPackage{rpmFact("a", "1:2.0-1"), rpmFact("b", "6.0-1")},
			[]osHoldingFact{{factID: "a", fixRank: 2}, {factID: "b", fixRank: 1}},
		},
		{
			"a range with several windows answers for the window holding the version", "debian", "5.0-1",
			[]supplychainmodel.AffectedPackage{windows, fact("a", "debian", "7.0-1")},
			[]osHoldingFact{{factID: "windows", fixRank: 1}, {factID: "a", fixRank: 2}},
		},
		{
			"apk uses the apk comparator", "alpine", "5.0-r1",
			[]supplychainmodel.AffectedPackage{fact("a", "alpine", "5.0-r10"), fact("b", "alpine", "5.0-r9")},
			[]osHoldingFact{{factID: "a", fixRank: 2}, {factID: "b", fixRank: 1}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := osHoldingFacts(tc.observed, tc.ecosystem, tc.pkgs, ""); !slices.Equal(got, tc.want) {
				t.Errorf("holders = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestOSHoldingFactsWithNoFixInTheHoldingRange: an open range still holds the
// version and has no rank, so it cannot be compared with a fix.
func TestOSHoldingFactsWithNoFixInTheHoldingRange(t *testing.T) {
	t.Parallel()

	open := supplychainmodel.AffectedPackage{FactID: "a", Source: "debian", ReleaseLine: "host:line", AffectedRanges: []supplychainmodel.AffectedRange{{
		Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}},
	}}}
	readable := supplychainmodel.AffectedPackage{FactID: "b", Source: "debian", ReleaseLine: "host:line", AffectedRanges: []supplychainmodel.AffectedRange{{
		Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: "0"}, {Fixed: "6.0-1"}},
	}}}
	got := osHoldingFacts("5.0-1", "debian", []supplychainmodel.AffectedPackage{open, readable}, "")
	want := []osHoldingFact{{factID: "a"}, {factID: "b", fixRank: 1}}
	if !slices.Equal(got, want) {
		t.Errorf("holders = %+v, want %+v", got, want)
	}
}

// TestOSHoldingFactsDoesNotAllocateForOneFact pins the hot path: almost every
// finding has one package fact, and with one fact there is nothing to choose.
func TestOSHoldingFactsDoesNotAllocateForOneFact(t *testing.T) {
	pkgs := []supplychainmodel.AffectedPackage{{FactID: "a", Source: "debian"}}
	if allocs := testing.AllocsPerRun(100, func() { osHoldingFacts("5.0-1", "debian", pkgs, "") }); allocs != 0 {
		t.Errorf("allocs = %v, want 0", allocs)
	}
}

// TestFleetDigestsEachNameTheirOwnHolder: the fleet-wide pass classifies each image
// digest on its own (#9858), so the holder is judged against that image's installed
// version. An image on u11 sits inside the DSA's range and past the DLA's fix, and an
// image on u13 is past both, so no record holds it and the first sorted one names it.
func TestFleetDigestsEachNameTheirOwnHolder(t *testing.T) {
	t.Parallel()

	vulnerable := "sha256:" + strings.Repeat("a", 64)
	patched := "sha256:" + strings.Repeat("b", 64)
	advisories := append(
		convertedOSVFixture(t, "built-holding-dla-bullseye-u10", false),
		convertedOSVFixture(t, "built-holding-dsa-bullseye-u12", false)...,
	)
	rows := append(
		fleetOSRow(debianCurl("11", deb11u("11")), "image://fleet/u11", "gen-u11", vulnerable),
		fleetOSRow(debianCurl("11", deb11u("13")), "image://fleet/u13", "gen-u13", patched)...,
	)

	byDigest := make(map[string]SupplyChainImpactFinding)
	for _, finding := range BuildSupplyChainImpactFindings(append(advisories, rows...)) {
		byDigest[finding.SubjectDigest] = finding
	}
	if len(byDigest) != 2 {
		t.Fatalf("findings by digest = %d, want 2: %#v", len(byDigest), byDigest)
	}
	got := byDigest[vulnerable]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.AdvisoryID != "DSA-2000-1" || got.FixedVersion != deb11u("12") {
		t.Errorf("u11 image advisory/fix = %q/%q, want DSA-2000-1 and u12", got.AdvisoryID, got.FixedVersion)
	}
	got = byDigest[patched]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.AdvisoryID != "DLA-1000-1" {
		t.Errorf("u13 image advisory = %q, want DLA-1000-1: no record holds it", got.AdvisoryID)
	}
}
