// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"strconv"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// unsortedTwoPairRange is the #9865 shape: two introduced..fixed pairs whose
// listed order is not version order. Sorted, the intervals are [0, 1.5.0) and
// [2.0.0, 2.5.0).
func unsortedTwoPairRange(kind string) supplychainmodel.AffectedRange {
	return supplychainmodel.AffectedRange{
		Kind: kind,
		Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: "2.0.0"},
			{Fixed: "2.5.0"},
			{Introduced: "0"},
			{Fixed: "1.5.0"},
		},
	}
}

// TestUnsortedRangeEventsContainAcrossFamilies is the #9865 acceptance red
// test: every ecosystem family that reads range events must evaluate them
// sorted, so observed 2.2.0 inside [2.0.0, 2.5.0) is affected_exact even when
// the feed lists the later pair first.
func TestUnsortedRangeEventsContainAcrossFamilies(t *testing.T) {
	t.Parallel()

	families := []struct {
		name      string
		ecosystem string
		kind      string
		reason    string
	}{
		{"npm", "npm", "SEMVER", supplyChainVersionReasonNPMSemverAffectedRange},
		{"go", "go", "SEMVER", supplyChainVersionReasonGoSemverAffectedRange},
		{"composer", "composer", "SEMVER", supplyChainVersionReasonComposerSemverAffectedRange},
		{"rubygems", "rubygems", "SEMVER", supplyChainVersionReasonRubyGemsAffectedRange},
		{"pypi", "pypi", "ECOSYSTEM", supplyChainVersionReasonPyPIPep440AffectedRange},
		{"nuget", "nuget", "SEMVER", supplyChainVersionReasonNuGetSemverAffectedRange},
	}

	for _, family := range families {
		t.Run(family.name, func(t *testing.T) {
			t.Parallel()
			pkgs := []supplychainmodel.AffectedPackage{{
				PackageID:      "pkg:" + family.name + "/example",
				Ecosystem:      family.ecosystem,
				Name:           "example",
				AffectedRanges: []supplychainmodel.AffectedRange{unsortedTwoPairRange(family.kind)},
			}}
			decision := evaluateSupplyChainVersionMatch(family.ecosystem, "2.2.0", "", "", pkgs)
			if decision.Status != SupplyChainImpactAffectedExact {
				t.Fatalf("Status = %q, want affected_exact for unsorted two-pair range at 2.2.0", decision.Status)
			}
			if decision.Reason != family.reason {
				t.Fatalf("Reason = %q, want %q", decision.Reason, family.reason)
			}
		})
	}
}

// TestUnsortedRangeEventsContainOSWalks covers the OS families (dpkg, apk) at
// the shared-walk level, pinning the fix for their comparators independent
// of the OS release-evidence plumbing. Before the fix the walk missed, and
// the miss landed on not_affected_known_fixed via the other pair's fix, not
// merely possibly_affected.
func TestUnsortedRangeEventsContainOSWalks(t *testing.T) {
	t.Parallel()

	walks := []struct {
		name    string
		compare versionCompareFunc
	}{
		{"dpkg", compareDPKGVersion},
		{"apk", compareAPKVersion},
	}
	for _, walk := range walks {
		t.Run(walk.name, func(t *testing.T) {
			t.Parallel()
			affected, valid := versionRangeContainsDecision(unsortedTwoPairRange("ECOSYSTEM"), "2.2.0", walk.compare)
			if !valid {
				t.Fatalf("valid = false, want true for well-formed versions")
			}
			if !affected {
				t.Fatalf("affected = false, want true for 2.2.0 inside [2.0.0, 2.5.0)")
			}
		})
	}
}

// TestUnsortedRangeBesideSortedLineReadsAffected pins the #9892 guard
// scenario under the #9865 fix: an unsorted two-pair range beside an
// unrelated sorted line still reports the version it contains.
func TestUnsortedRangeBesideSortedLineReadsAffected(t *testing.T) {
	t.Parallel()

	pkgs := affectedPackageWithRanges(semverLine("0", "1.0.0"), unsortedTwoPairRange("SEMVER"))
	decision := evaluateSupplyChainVersionMatch("npm", "2.2.0", "", "", pkgs)
	if decision.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected_exact for unsorted range beside a sorted line", decision.Status)
	}
	if decision.Reason != supplyChainVersionReasonNPMSemverAffectedRange {
		t.Fatalf("Reason = %q, want %q", decision.Reason, supplyChainVersionReasonNPMSemverAffectedRange)
	}
}

// TestComparatorBranchRejectsHyphenRange is the red row for the #9865 scope
// note (reviewer finding N3): a bare "-" token is not a comparator, so a
// hyphen range must read invalid rather than "=2.0.0 not contained".
func TestComparatorBranchRejectsHyphenRange(t *testing.T) {
	t.Parallel()

	if affected, valid := comparatorBranchContains("2.0.0 - 2.3.0", "2.1.0", compareOSVSemver); affected || valid {
		t.Fatalf("comparatorBranchContains(hyphen) = (%v, %v), want (false, false)", affected, valid)
	}
	if affected, valid := comparatorRangeContains("2.0.0 - 2.3.0", "2.1.0", compareOSVSemver); affected || valid {
		t.Fatalf("comparatorRangeContains(hyphen) = (%v, %v), want (false, false)", affected, valid)
	}
}

// TestUnsortedRangeEventsQARowMoves is the read-only row-move count for
// #9865: a fixed corpus of QA rows (six matcher families times four observed
// versions over the unsorted two-pair shape, plus one dpkg row) evaluated
// without writing anything. The red run showed every 2.2.0 row as
// possibly_affected, and the dpkg row as not_affected_known_fixed via the
// other pair's fix; after the fix all seven read affected_exact, and no
// other row moves.
func TestUnsortedRangeEventsQARowMoves(t *testing.T) {
	t.Parallel()

	type qaRow struct {
		ecosystem string
		kind      string
		source    string
		observed  string
		status    SupplyChainImpactStatus
		reason    string
	}
	families := []struct {
		ecosystem string
		kind      string
		source    string
		exact     string
	}{
		{"npm", "SEMVER", "", supplyChainVersionReasonNPMSemverAffectedRange},
		{"go", "SEMVER", "", supplyChainVersionReasonGoSemverAffectedRange},
		{"composer", "SEMVER", "", supplyChainVersionReasonComposerSemverAffectedRange},
		{"rubygems", "SEMVER", "", supplyChainVersionReasonRubyGemsAffectedRange},
		{"pypi", "ECOSYSTEM", "", supplyChainVersionReasonPyPIPep440AffectedRange},
		{"nuget", "SEMVER", "", supplyChainVersionReasonNuGetSemverAffectedRange},
	}
	var rows []qaRow
	for _, family := range families {
		rows = append(rows,
			qaRow{family.ecosystem, family.kind, family.source, "0.5.0", SupplyChainImpactAffectedExact, family.exact},
			qaRow{family.ecosystem, family.kind, family.source, "1.5.0", SupplyChainImpactPossiblyAffected, supplyChainVersionReasonNoAffectedMatch},
			qaRow{family.ecosystem, family.kind, family.source, "2.2.0", SupplyChainImpactAffectedExact, family.exact},
			qaRow{family.ecosystem, family.kind, family.source, "3.0.0", SupplyChainImpactPossiblyAffected, supplyChainVersionReasonNoAffectedMatch},
		)
	}
	moves := 0
	for _, row := range rows {
		pkgs := []supplychainmodel.AffectedPackage{{
			PackageID:      "pkg:qa/example",
			Ecosystem:      row.ecosystem,
			Name:           "example",
			AffectedRanges: []supplychainmodel.AffectedRange{unsortedTwoPairRange(row.kind)},
		}}
		decision := evaluateSupplyChainVersionMatch(row.ecosystem, row.observed, "", "", pkgs)
		if decision.Status != row.status || decision.Reason != row.reason {
			t.Fatalf("%s @ %s: status=%q reason=%q, want %q / %q",
				row.ecosystem, row.observed, decision.Status, decision.Reason, row.status, row.reason)
		}
		if row.observed == "2.2.0" {
			moves++
		}
	}
	// The dpkg row covers the OS family at the matcher level: the same miss
	// used to land on not_affected_known_fixed via the other pair's fix, and
	// now reads affected_exact with the vendor release evidence.
	dpkg := evaluateSupplyChainVersionMatch("dpkg", "2.2.0", "", "", []supplychainmodel.AffectedPackage{{
		Source:         "debian",
		PackageID:      "pkg:dpkg/example",
		Ecosystem:      "dpkg",
		Name:           "example",
		AffectedRanges: []supplychainmodel.AffectedRange{unsortedTwoPairRange("ECOSYSTEM")},
	}})
	if dpkg.Status != SupplyChainImpactAffectedExact || dpkg.Reason != supplyChainVersionReasonDPKGAffectedRange {
		t.Fatalf("dpkg @ 2.2.0: status=%q reason=%q, want affected_exact / dpkg_affected_range",
			dpkg.Status, dpkg.Reason)
	}
	moves++
	t.Logf("QA: %d rows move to affected_exact (6 possibly_affected, dpkg not_affected_known_fixed)", moves)
}

// sortedTwoPairRange is the same two intervals as unsortedTwoPairRange
// listed in version order: the common feed shape, which takes the
// zero-allocation fast path.
func sortedTwoPairRange(kind string) supplychainmodel.AffectedRange {
	return supplychainmodel.AffectedRange{
		Kind: kind,
		Events: []supplychainmodel.AffectedRangeEvent{
			{Introduced: "0"},
			{Fixed: "1.5.0"},
			{Introduced: "2.0.0"},
			{Fixed: "2.5.0"},
		},
	}
}

// BenchmarkSortedRangeContainsScale measures the #9865 sort cost at the
// matcher level: 1x is 4 two-pair ranges per advisory, 5x and 10x scale the
// ranges. The observed version sits above every range so no range
// short-circuits and each one pays the ordering check. The unsorted listing
// pays the full sort; the sorted listing takes the zero-allocation fast
// path. Sorting is O(n log n) per range with small n, so unsorted
// allocations must grow linearly in the range count.
func BenchmarkSortedRangeContainsScale(b *testing.B) {
	build := func(kind string, scale int) []supplychainmodel.AffectedPackage {
		ranges := make([]supplychainmodel.AffectedRange, 0, 4*scale)
		for line := 0; line < 4*scale; line++ {
			if kind == "sorted" {
				ranges = append(ranges, sortedTwoPairRange("SEMVER"))
			} else {
				ranges = append(ranges, unsortedTwoPairRange("SEMVER"))
			}
		}
		return affectedPackageWithRanges(ranges...)
	}
	for _, scale := range []int{1, 5, 10} {
		for _, kind := range []string{"unsorted", "sorted"} {
			pkgs := build(kind, scale)
			// Above every range the sorted listing resolves through the
			// #9854 per-line branch to known-fixed, while the unsorted
			// listing stays fail-closed at possibly_affected; either way
			// every range pays the ordering check.
			want := SupplyChainImpactPossiblyAffected
			if kind == "sorted" {
				want = SupplyChainImpactNotAffectedKnownFixed
			}
			b.Run(kind+"/"+strconv.Itoa(scale)+"x", func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					decision := evaluateSupplyChainVersionMatch("npm", "3.0.0", "", "", pkgs)
					if decision.Status != want {
						b.Fatalf("status = %q, want %q above every range", decision.Status, want)
					}
				}
			})
		}
	}
}

// TestSortedWalkKeepsListedOrderForSameVersionTies pins that the sorted walk
// is a stable sort, like the OSV reference sorted(range.events): events tied
// at one version keep the order the feed lists them, so each listing below
// reads exactly as the listed-order walk would read it.
func TestSortedWalkKeepsListedOrderForSameVersionTies(t *testing.T) {
	t.Parallel()

	listings := []struct {
		name     string
		events   []supplychainmodel.AffectedRangeEvent
		affected bool
	}{
		{"introduced_first", []supplychainmodel.AffectedRangeEvent{{Introduced: "1.0.0"}, {Fixed: "1.0.0"}}, false},
		{"fixed_first", []supplychainmodel.AffectedRangeEvent{{Fixed: "1.0.0"}, {Introduced: "1.0.0"}}, true},
	}
	for _, listing := range listings {
		affected, valid := versionRangeContainsDecision(
			supplychainmodel.AffectedRange{Kind: "SEMVER", Events: listing.events},
			"1.0.0",
			compareOSVSemver,
		)
		if !valid {
			t.Fatalf("%s: valid = false, want true for a well-formed tie", listing.name)
		}
		if affected != listing.affected {
			t.Fatalf("%s: affected = %v, want %v for the listed tie order", listing.name, affected, listing.affected)
		}
	}
}
