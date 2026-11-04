// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// orderEvent is one generated range event on an integer version. kind is 'i'
// (introduced), 'f' (fixed), or 'l' (last_affected). Version n renders as
// "n/10.n%10.0", and introduced 0 renders as the OSV floor "0".
type orderEvent struct {
	kind    byte
	version int
}

// rangeContainsObserved reports whether the decision says some advisory range
// contains the observed version. An npm-style family answers with
// affected_exact. An OS family (debian, alpine) treats a fact's ranges as
// per-release (#9848, ruling D): one range containing the version while
// another range's fix is at or below it is os_release_ambiguous, and a fact
// with no release evidence is os_release_unverified. Both still mean a range
// contains the version, so the order-sensitive containment answer is the same
// one the oracle computes. possibly_affected with any other reason, such as
// version_not_in_advisory_range, means no range contains it.
func rangeContainsObserved(ecosystem string, got supplyChainVersionMatchDecision) bool {
	if string(got.Status) == "affected_exact" {
		return true
	}
	if ecosystem != "debian" && ecosystem != "alpine" {
		return false
	}
	return string(got.Status) == string(SupplyChainImpactPossiblyAffected) &&
		(got.Reason == supplyChainVersionReasonOSReleaseAmbiguous ||
			got.Reason == supplyChainVersionReasonOSReleaseUnverified)
}

func (e orderEvent) text() string {
	if e.kind == 'i' && e.version == 0 {
		return "0"
	}
	return fmt.Sprintf("%d.%d.0", e.version/10, e.version%10)
}

func orderEventsToModel(events []orderEvent) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = string(e.kind) + ":" + e.text()
	}
	return out
}

// osvAffected is the OSV reference algorithm on integers: sort the events by
// version, then apply them in order. tieRank orders events of one version
// (nil keeps the listed order, which is a stable sort).
func osvAffected(events []orderEvent, observed int, tieRank func(orderEvent) int) bool {
	sorted := append([]orderEvent(nil), events...)
	sort.SliceStable(sorted, func(a, b int) bool {
		if sorted[a].version != sorted[b].version {
			return sorted[a].version < sorted[b].version
		}
		return tieRank != nil && tieRank(sorted[a]) < tieRank(sorted[b])
	})
	return osvApply(sorted, observed)
}

// listedAffected is the walk before #9865: events applied in listed order.
func listedAffected(events []orderEvent, observed int) bool {
	return osvApply(events, observed)
}

func osvApply(events []orderEvent, observed int) bool {
	affected := false
	for _, e := range events {
		switch {
		case e.kind == 'i' && observed >= e.version:
			affected = true
		case e.kind == 'f' && observed >= e.version:
			affected = false
		case e.kind == 'l' && observed > e.version:
			affected = false
		}
	}
	return affected
}

func genOrderRange(rng *rand.Rand) []orderEvent {
	var events []orderEvent
	for pairs := 1 + rng.Intn(3); pairs > 0; pairs-- {
		low := rng.Intn(20)
		if rng.Intn(4) == 0 {
			low = 0
		}
		events = append(events, orderEvent{'i', low})
		kind := byte('f')
		if rng.Intn(4) == 0 {
			kind = 'l'
		}
		if rng.Intn(6) != 0 { // else an open line
			events = append(events, orderEvent{kind, low + 1 + rng.Intn(12)})
		}
	}
	if rng.Intn(5) == 0 { // a stray event, so ties at one version occur
		events = append(events, orderEvent{"ifl"[rng.Intn(3)], rng.Intn(30)})
	}
	rng.Shuffle(len(events), func(a, b int) { events[a], events[b] = events[b], events[a] })
	return events
}

// TestRangeEventOrderParityAgainstOSVOracle is the #9865 differential. For
// seeded advisories whose events are shuffled, every ecosystem family's
// verdict must equal an independent OSV oracle (integer versions, sorted
// events, no shared comparator). OSV leaves a same-version tie undefined, so
// the oracle is evaluated under both tie orders: where they agree the matcher
// must agree, and where they differ the matcher keeps the listed order.
func TestRangeEventOrderParityAgainstOSVOracle(t *testing.T) {
	t.Parallel()
	families := []struct{ ecosystem, kind string }{
		{"npm", "SEMVER"},
		{"cargo", "SEMVER"},
		{"hex", "SEMVER"},
		{"swift", "SEMVER"},
		{"pub", "SEMVER"},
		{"go", "SEMVER"},
		{"go", "ECOSYSTEM"},
		{"pypi", "ECOSYSTEM"},
		{"nuget", "SEMVER"},
		{"composer", "SEMVER"},
		{"rubygems", "SEMVER"},
		{"debian", "ECOSYSTEM"},
		{"alpine", "ECOSYSTEM"},
	}
	introducedFirst := func(e orderEvent) int {
		if e.kind == 'i' {
			return 0
		}
		return 1
	}
	terminatorFirst := func(e orderEvent) int {
		if e.kind == 'i' {
			return 1
		}
		return 0
	}
	rng := rand.New(rand.NewSource(9865))
	var rows, wrong, tieWrong, movedToAffected, movedToUnaffected, unsortedAdvisories int
	for n := 0; n < 400; n++ {
		ranges := make([][]orderEvent, 1+rng.Intn(3))
		for i := range ranges {
			ranges[i] = genOrderRange(rng)
		}
		sortedAll := true
		for _, events := range ranges {
			if !sort.SliceIsSorted(events, func(a, b int) bool { return events[a].version < events[b].version }) {
				sortedAll = false
			}
		}
		if !sortedAll {
			unsortedAdvisories++
		}
		for _, family := range families {
			model := make([]supplychainmodel.AffectedRange, len(ranges))
			for i, events := range ranges {
				model[i] = eventRange(family.kind, orderEventsToModel(events)...)
			}
			pkgs := verifiedForOSHost(affectedPackageWithRanges(model...))
			for observed := 0; observed < 36; observed++ {
				got := evaluateSupplyChainVersionMatch(family.ecosystem, orderEvent{'f', observed}.text(), "", "", pkgs)
				gotAffected := rangeContainsObserved(family.ecosystem, got)
				stable, intro, term, listed := false, false, false, false
				for _, events := range ranges {
					stable = stable || osvAffected(events, observed, nil)
					intro = intro || osvAffected(events, observed, introducedFirst)
					term = term || osvAffected(events, observed, terminatorFirst)
					listed = listed || listedAffected(events, observed)
				}
				rows++
				if gotAffected != stable {
					wrong++
					t.Errorf("%s/%s observed %d ranges %v: got affected=%v, oracle(listed ties)=%v", family.ecosystem, family.kind, observed, ranges, gotAffected, stable)
				}
				if intro == term && gotAffected != intro {
					tieWrong++
				}
				switch {
				case gotAffected && !listed:
					movedToAffected++
				case !gotAffected && listed:
					movedToUnaffected++
				}
			}
		}
	}
	t.Logf("rows=%d wrong=%d tie-insensitive wrong=%d; unsorted advisories=%d; "+
		"moves vs the listed-order walk: not-affected->affected=%d, affected->not-affected=%d",
		rows, wrong, tieWrong, unsortedAdvisories, movedToAffected, movedToUnaffected)
	if wrong != 0 || tieWrong != 0 {
		t.Fatalf("%d rows disagree with the OSV oracle (%d tie-insensitive)", wrong, tieWrong)
	}
	if movedToAffected == 0 {
		t.Fatal("the corpus never exercised an out-of-order range; the differential proves nothing")
	}
}
