// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// osListFamilyCase carries one distro family's fixtures for the vendor
// exact-version-list precedence rule (#9929).
type osListFamilyCase struct {
	name          string
	osvSource     string
	vendorSource  string
	purl          string
	host          string // the installed version
	fixBelow      string // an OSV fix strictly below host
	otherFixBelow string // a second OSV fix, also at or below host
	otherVersion  string // a version that is not the host
	unreadable    string // a list entry the family comparator cannot read
	affected      string // the closed exact-affected reason
	knownFixed    string // the closed single-fix known-fixed reason
	rangeFixed    string // the closed multi-fix known-fixed reason
}

var osListFamilies = []osListFamilyCase{
	{
		name: "dpkg", osvSource: "osv", vendorSource: "debian", purl: "pkg:deb/debian/curl",
		host: "1:1.0-3", fixBelow: "1:1.0-2", otherFixBelow: "1:0.9-1", otherVersion: "1:1.0-4",
		unreadable: "not a version",
		affected:   supplyChainVersionReasonDPKGExactAffected,
		knownFixed: supplyChainVersionReasonDPKGKnownFixed,
		rangeFixed: supplyChainVersionReasonDPKGKnownFixed,
	},
	{
		name: "apk", osvSource: "osv", vendorSource: "alpine", purl: "pkg:apk/alpine/curl",
		host: "8.15.0-r4", fixBelow: "8.15.0-r3", otherFixBelow: "8.14.1-r0", otherVersion: "8.15.0-r5",
		unreadable: "bad-version",
		affected:   supplyChainVersionReasonAPKExactAffected,
		knownFixed: supplyChainVersionReasonAPKKnownFixed,
		rangeFixed: supplyChainVersionReasonAPKKnownFixed,
	},
}

// osvFact builds an untagged OSV OS fact: its ranges are the union of every
// release's, so it carries no release evidence.
func (c osListFamilyCase) osvFact(ranges []supplychainmodel.AffectedRange, fixes ...string) supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "CVE-2099-0929", Source: c.osvSource, AdvisoryID: "DEBIAN-CVE-2099-0929",
		PackageID: "os://" + c.name + "/curl", Ecosystem: "os", Name: "curl",
		PURL: c.purl, AffectedRanges: ranges, FixedVersions: fixes,
	}
}

// vendorFact is a vendor-sourced fact. The join reads any non-OSV source as
// release evidence but carries no release line for it, so the fixture states
// none and the rules under test match on the exact version string alone
// (#9945 tracks checking the line).
func (c osListFamilyCase) vendorFact(advisory string, versions ...string) supplychainmodel.AffectedPackage {
	return supplychainmodel.AffectedPackage{
		CVEID: "CVE-2099-0929", Source: c.vendorSource, AdvisoryID: advisory,
		PackageID: "os://" + c.vendorSource + "/curl", Ecosystem: "os", Name: "curl",
		PURL: c.purl, AffectedVersions: versions,
	}
}

func osIntroducedFixedRange(introduced, fixed string) supplychainmodel.AffectedRange {
	return supplychainmodel.AffectedRange{
		Kind:   "ECOSYSTEM",
		Events: []supplychainmodel.AffectedRangeEvent{{Introduced: introduced}, {Fixed: fixed}},
	}
}

// bothOrders runs check with facts in the given order and in reverse, since a
// join delivers advisory facts in no guaranteed order.
func bothOrders(t *testing.T, facts []supplychainmodel.AffectedPackage, check func(t *testing.T, ordered []supplychainmodel.AffectedPackage)) {
	t.Helper()
	reversed := make([]supplychainmodel.AffectedPackage, len(facts))
	for i, fact := range facts {
		reversed[len(facts)-1-i] = fact
	}
	t.Run("as given", func(t *testing.T) { check(t, facts) })
	t.Run("reversed", func(t *testing.T) { check(t, reversed) })
}

func assertOSDecision(t *testing.T, got supplyChainVersionMatchDecision, status SupplyChainImpactStatus, reason string) {
	t.Helper()
	if got.Status != status || got.Reason != reason {
		t.Fatalf("decision = %s/%s, want %s/%s", got.Status, got.Reason, status, reason)
	}
}

// TestUntaggedVendorVersionListBeatsOSVSingleFix replaces the #9927 pin
// TestUntaggedVendorVersionListKeepsSingleFixShortcut (#9929). A vendor fact's
// exact version list that names the host settles the host as affected,
// whatever an untagged OSV fact's single fix says, in either fact order. The
// vendor fact alone already reads affected_exact, so the join must not turn it
// into known fixed.
func TestUntaggedVendorVersionListBeatsOSVSingleFix(t *testing.T) {
	t.Parallel()
	for _, c := range osListFamilies {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			vendor := c.vendorFact("VENDOR-9929-1", c.host)
			alone := evaluateSupplyChainVersionMatch("os", c.host, "", "", []supplychainmodel.AffectedPackage{vendor})
			assertOSDecision(t, alone, SupplyChainImpactAffectedExact, c.affected)
			osv := c.osvFact(nil, c.fixBelow)
			bothOrders(t, []supplychainmodel.AffectedPackage{osv, vendor}, func(t *testing.T, ordered []supplychainmodel.AffectedPackage) {
				got := evaluateSupplyChainVersionMatch("os", c.host, "", singleFixedVersion(osv.FixedVersions), ordered)
				assertOSDecision(t, got, SupplyChainImpactAffectedExact, c.affected)
			})
		})
	}
}

// TestVendorVersionListBeatsOSVRanges covers the multi-fix path: several OSV
// fixes leave the scalar fixed version blank, and an OSV range that contains
// the host, contradicts itself, or cannot be read must not preempt a vendor
// list that names the host.
func TestVendorVersionListBeatsOSVRanges(t *testing.T) {
	t.Parallel()
	for _, c := range osListFamilies {
		vendor := c.vendorFact("VENDOR-9929-2", c.host)
		for _, tc := range []struct {
			name string
			osv  supplychainmodel.AffectedPackage
		}{
			{
				name: "several fixes at or below the host",
				osv: c.osvFact([]supplychainmodel.AffectedRange{
					osIntroducedFixedRange("0", c.fixBelow), osIntroducedFixedRange("0", c.otherFixBelow),
				}, c.fixBelow, c.otherFixBelow),
			},
			{
				name: "a range contains the host",
				osv: c.osvFact([]supplychainmodel.AffectedRange{
					osIntroducedFixedRange("0", c.otherVersion),
				}, c.otherVersion),
			},
			{
				name: "one range contains the host and another fix sits below it",
				osv: c.osvFact([]supplychainmodel.AffectedRange{
					osIntroducedFixedRange("0", c.otherVersion), osIntroducedFixedRange("0", c.otherFixBelow),
				}, c.otherVersion, c.otherFixBelow),
			},
			{
				name: "an unreadable OSV range",
				osv: c.osvFact([]supplychainmodel.AffectedRange{
					osIntroducedFixedRange("0", c.unreadable),
				}, c.unreadable),
			},
			{name: "OSV fact names no fix", osv: c.osvFact(nil)},
		} {
			t.Run(c.name+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				bothOrders(t, []supplychainmodel.AffectedPackage{tc.osv, vendor}, func(t *testing.T, ordered []supplychainmodel.AffectedPackage) {
					got := evaluateSupplyChainVersionMatch("os", c.host, "", "", ordered)
					assertOSDecision(t, got, SupplyChainImpactAffectedExact, c.affected)
				})
			})
		}
	}
}

// TestVendorVersionListEdgeCases pins the cases around the rule: a list that
// does not name the host, an empty list, and two vendor facts that disagree.
// It does not model a release line: a vendor fact carries none, so the rule
// matches on the exact version string alone (see the note in
// osReleaseEvidenceListNamesObserved).
func TestVendorVersionListEdgeCases(t *testing.T) {
	t.Parallel()
	for _, c := range osListFamilies {
		osv := c.osvFact(nil, c.fixBelow)
		fixed := singleFixedVersion(osv.FixedVersions)
		for _, tc := range []struct {
			name       string
			vendors    []supplychainmodel.AffectedPackage
			wantStatus SupplyChainImpactStatus
			wantReason string
		}{
			{
				name:       "empty list keeps the single-fix shortcut",
				vendors:    []supplychainmodel.AffectedPackage{c.vendorFact("V-1")},
				wantStatus: SupplyChainImpactNotAffectedKnownFixed, wantReason: c.knownFixed,
			},
			{
				name:       "blank list entry keeps the single-fix shortcut",
				vendors:    []supplychainmodel.AffectedPackage{c.vendorFact("V-1", "  ")},
				wantStatus: SupplyChainImpactNotAffectedKnownFixed, wantReason: c.knownFixed,
			},
			{
				name:       "list naming a different version keeps the shortcut",
				vendors:    []supplychainmodel.AffectedPackage{c.vendorFact("V-1", c.otherVersion)},
				wantStatus: SupplyChainImpactNotAffectedKnownFixed, wantReason: c.knownFixed,
			},
			{
				name: "two vendor facts disagree: the one naming the host wins",
				vendors: []supplychainmodel.AffectedPackage{
					c.vendorFact("V-1", c.otherVersion), c.vendorFact("V-2", c.host),
				},
				wantStatus: SupplyChainImpactAffectedExact, wantReason: c.affected,
			},
			{
				name:       "the host named among other entries",
				vendors:    []supplychainmodel.AffectedPackage{c.vendorFact("V-1", c.otherVersion, c.host)},
				wantStatus: SupplyChainImpactAffectedExact, wantReason: c.affected,
			},
		} {
			t.Run(c.name+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				facts := append([]supplychainmodel.AffectedPackage{osv}, tc.vendors...)
				bothOrders(t, facts, func(t *testing.T, ordered []supplychainmodel.AffectedPackage) {
					got := evaluateSupplyChainVersionMatch("os", c.host, "", fixed, ordered)
					assertOSDecision(t, got, tc.wantStatus, tc.wantReason)
				})
			})
		}
	}
}

// TestVendorVersionListUnreadableEntries pins what happens to a vendor list
// entry the family comparator cannot read (#9929 G2). It never counts as
// naming the host. It is not dropped silently either: with no readable entry
// naming the host, an unreadable entry might have been the host's version, so
// the single-fix shortcut must not claim known fixed past it. The finding
// fails closed as possibly_affected/malformed_range, the reason the full scan
// already gives an unreadable list entry. A readable entry that names the host
// wins wherever it sits, so the verdict does not depend on entry order.
func TestVendorVersionListUnreadableEntries(t *testing.T) {
	t.Parallel()
	for _, c := range osListFamilies {
		osv := c.osvFact(nil, c.fixBelow)
		fixed := singleFixedVersion(osv.FixedVersions)
		for _, tc := range []struct {
			name       string
			versions   []string
			wantStatus SupplyChainImpactStatus
			wantReason string
		}{
			{
				name: "only an unreadable entry", versions: []string{c.unreadable},
				wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonMalformedRange,
			},
			{
				name: "unreadable entry before a readable non-match", versions: []string{c.unreadable, c.otherVersion},
				wantStatus: SupplyChainImpactPossiblyAffected, wantReason: supplyChainVersionReasonMalformedRange,
			},
			{
				name: "unreadable entry before the host", versions: []string{c.unreadable, c.host},
				wantStatus: SupplyChainImpactAffectedExact, wantReason: c.affected,
			},
			{
				name: "unreadable entry after the host", versions: []string{c.host, c.unreadable},
				wantStatus: SupplyChainImpactAffectedExact, wantReason: c.affected,
			},
		} {
			t.Run(c.name+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				vendor := c.vendorFact("V-1", tc.versions...)
				bothOrders(t, []supplychainmodel.AffectedPackage{osv, vendor}, func(t *testing.T, ordered []supplychainmodel.AffectedPackage) {
					got := evaluateSupplyChainVersionMatch("os", c.host, "", fixed, ordered)
					assertOSDecision(t, got, tc.wantStatus, tc.wantReason)
				})
			})
		}
	}
}

// TestUntaggedOSVUnreadableEntryKeepsEarlierRule pins the asymmetry with the
// vendor case above: an unreadable list entry on an untagged OSV fact does not
// block the single-fix shortcut, so the host still reads known fixed. This is
// the rule from #9806 PR-2b, which #9929 leaves unchanged; only a fact that
// carries release evidence (a vendor fact) blocks on an unreadable entry. The
// row pins that boundary so widening the block to untagged OSV facts turns a
// test red (#9929, review finding F2).
func TestUntaggedOSVUnreadableEntryKeepsEarlierRule(t *testing.T) {
	t.Parallel()
	for _, c := range osListFamilies {
		for _, tc := range []struct {
			name     string
			versions []string
		}{
			{name: "only an unreadable entry", versions: []string{c.unreadable}},
			{name: "unreadable entry beside a readable non-match", versions: []string{c.unreadable, c.otherVersion}},
		} {
			t.Run(c.name+" "+tc.name, func(t *testing.T) {
				t.Parallel()
				osv := c.osvFact(nil, c.fixBelow)
				osv.AffectedVersions = tc.versions
				fixed := singleFixedVersion(osv.FixedVersions)
				got := evaluateSupplyChainVersionMatch("os", c.host, "", fixed, []supplychainmodel.AffectedPackage{osv})
				assertOSDecision(t, got, SupplyChainImpactNotAffectedKnownFixed, c.knownFixed)
			})
		}
	}
}

// BenchmarkOSVendorListPrecedence times the per-finding classification of an
// OS host whose vendor list does not name it (the worst case for the #9929
// pass: every list entry is compared, then the single-fix shortcut answers).
func BenchmarkOSVendorListPrecedence(b *testing.B) {
	c := osListFamilies[0]
	osv := c.osvFact(nil, c.fixBelow)
	versions := make([]string, 0, 16)
	versions = append(versions, []string{
		"1:1.0-4", "1:1.0-5", "1:1.1-1", "1:1.1-2", "1:1.2-1", "1:1.2-2", "1:1.3-1", "1:1.3-2",
		"1:2.0-1", "1:2.0-2", "1:2.1-1", "1:2.1-2", "1:2.2-1", "1:2.2-2", "1:2.3-1", "1:2.3-2",
	}...)
	pkgs := []supplychainmodel.AffectedPackage{osv, c.vendorFact("V-1", versions...), c.vendorFact("V-2", versions...)}
	fixed := singleFixedVersion(osv.FixedVersions)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := evaluateSupplyChainVersionMatch("os", c.host, "", fixed, pkgs)
		if got.Status != SupplyChainImpactNotAffectedKnownFixed {
			b.Fatalf("decision = %s/%s", got.Status, got.Reason)
		}
	}
}
