// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// oracleVersion is a semver value held as plain integers so the oracle shares
// no comparator with the matcher. pre is 0 for a release and k for the
// prerelease "-alpha.k", which sorts below its release.
type oracleVersion struct{ major, minor, patch, pre int }

func (v oracleVersion) String() string {
	if v.pre > 0 {
		return fmt.Sprintf("%d.%d.%d-alpha.%d", v.major, v.minor, v.patch, v.pre)
	}
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

func (v oracleVersion) rank() int {
	if v.pre == 0 {
		return 1 << 20
	}
	return v.pre
}

func (v oracleVersion) cmp(o oracleVersion) int {
	for _, pair := range [][2]int{{v.major, o.major}, {v.minor, o.minor}, {v.patch, o.patch}, {v.rank(), o.rank()}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// oracleEvent is one range event: kind is "introduced", "fixed", or
// "last_affected".
type oracleEvent struct {
	kind    string
	version oracleVersion
}

// oracleRange is one generated range. kind is the OSV range type. gitHashes
// marks a GIT range whose events are commit hashes, which no version compare
// can evaluate. listed is the order the matcher receives; the OSV algorithm
// evaluates the events sorted by version.
type oracleRange struct {
	kind      string
	gitHashes bool
	listed    []oracleEvent
}

func (r oracleRange) sorted() []oracleEvent {
	out := slices.Clone(r.listed)
	slices.SortStableFunc(out, func(a, b oracleEvent) int { return a.version.cmp(b.version) })
	return out
}

func (r oracleRange) listedAscending() bool {
	return slices.IsSortedFunc(r.listed, func(a, b oracleEvent) int { return a.version.cmp(b.version) })
}

// osvAffects restates the OSV range algorithm on sorted events: a version is
// affected when it ends the walk inside an introduced..terminator interval.
func (r oracleRange) osvAffects(v oracleVersion) bool {
	vulnerable := false
	for _, event := range r.sorted() {
		switch event.kind {
		case "introduced":
			if v.cmp(event.version) >= 0 {
				vulnerable = true
			}
		case "fixed":
			if v.cmp(event.version) >= 0 {
				vulnerable = false
			}
		case "last_affected":
			if v.cmp(event.version) > 0 {
				vulnerable = false
			}
		}
	}
	return vulnerable
}

func (r oracleRange) model() supplychainmodel.AffectedRange {
	out := supplychainmodel.AffectedRange{Kind: r.kind}
	for i, event := range r.listed {
		value := event.version.String()
		if r.gitHashes {
			value = fmt.Sprintf("%040x", i+1)
		}
		switch event.kind {
		case "introduced":
			out.Events = append(out.Events, supplychainmodel.AffectedRangeEvent{Introduced: value})
		case "fixed":
			out.Events = append(out.Events, supplychainmodel.AffectedRangeEvent{Fixed: value})
		default:
			out.Events = append(out.Events, supplychainmodel.AffectedRangeEvent{LastAffected: value})
		}
	}
	return out
}

// oracleKnownFixed restates the #9854 policy for a shape the matcher may
// read: every range SEMVER with events listed ascending. Outside every range,
// a Fixed at or below v must exist and no last_affected below v may be nearer.
func oracleKnownFixed(ranges []oracleRange, v oracleVersion) bool {
	var nearestFixed, nearestLastAffected *oracleVersion
	for _, r := range ranges {
		if r.osvAffects(v) {
			return false
		}
		for _, event := range r.listed {
			end := event.version
			switch event.kind {
			case "fixed":
				if v.cmp(end) >= 0 && (nearestFixed == nil || end.cmp(*nearestFixed) > 0) {
					nearestFixed = &end
				}
			case "last_affected":
				if v.cmp(end) > 0 && (nearestLastAffected == nil || end.cmp(*nearestLastAffected) > 0) {
					nearestLastAffected = &end
				}
			}
		}
	}
	if nearestFixed == nil {
		return false
	}
	return nearestLastAffected == nil || nearestFixed.cmp(*nearestLastAffected) >= 0
}

func oracleVersionPool() []oracleVersion {
	var pool []oracleVersion
	for major := 0; major <= 4; major++ {
		for minor := 0; minor <= 2; minor++ {
			for patch := 0; patch <= 1; patch++ {
				for _, pre := range []int{0, 1, 2} {
					pool = append(pool, oracleVersion{major, minor, patch, pre})
				}
			}
		}
	}
	return pool
}

// generateOracleRange builds one range of one or two introduced..terminator
// pairs over strictly increasing versions, then optionally lists the events
// out of order, labels it ECOSYSTEM or GIT, or leaves the last line open.
func generateOracleRange(rng *rand.Rand, pool []oracleVersion) oracleRange {
	count := 2 + 2*rng.Intn(2)
	if rng.Intn(5) == 0 {
		count--
	}
	picked := map[int]bool{}
	for len(picked) < count {
		picked[rng.Intn(len(pool))] = true
	}
	indexes := make([]int, 0, count)
	for index := range picked {
		indexes = append(indexes, index)
	}
	slices.SortFunc(indexes, func(a, b int) int { return pool[a].cmp(pool[b]) })
	r := oracleRange{kind: "SEMVER"}
	for i, index := range indexes {
		kind := "introduced"
		if i%2 == 1 {
			kind = "fixed"
			if rng.Intn(4) == 0 {
				kind = "last_affected"
			}
		}
		r.listed = append(r.listed, oracleEvent{kind: kind, version: pool[index]})
	}
	switch roll := rng.Intn(20); {
	case roll < 3:
		r.kind = "ECOSYSTEM"
	case roll < 5:
		r.kind, r.gitHashes = "GIT", true
	}
	if rng.Intn(4) == 0 {
		rng.Shuffle(len(r.listed), func(i, j int) { r.listed[i], r.listed[j] = r.listed[j], r.listed[i] })
	}
	return r
}

// TestNPMPerLineKnownFixedParity is the accuracy differential for #9854. Over
// seeded random advisories (one to three ranges of one or two pairs each,
// shuffled event order, ECOSYSTEM and GIT ranges, prerelease versions, and the
// ranges split across one or two package entries) it asserts, against an
// oracle that follows the OSV algorithm (events sorted, union over every range
// kind) and shares no comparator with the matcher:
//   - no row moves except possibly_affected to known-fixed;
//   - a flip never happens when OSV says the version is affected, or when the
//     advisory holds a range the containment check cannot read (non-SEMVER) or
//     a range listed out of version order;
//   - on the shape the matcher may read (all SEMVER, events ascending), the
//     flip set equals the oracle's nearest-Fixed policy exactly.
func TestNPMPerLineKnownFixedParity(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(9854))
	pool := oracleVersionPool()
	var rows, flips, readableAdvisories, guardedAdvisories int
	for advisory := 0; advisory < 700; advisory++ {
		ranges := make([]oracleRange, 0, 3)
		for n := 1 + rng.Intn(3); n > 0; n-- {
			ranges = append(ranges, generateOracleRange(rng, pool))
		}
		models := make([]supplychainmodel.AffectedRange, 0, len(ranges))
		for _, r := range ranges {
			models = append(models, r.model())
		}
		pkgs := affectedPackageWithRanges(models...)
		if len(models) > 1 && rng.Intn(2) == 0 {
			// A second entry for the same package, as a second advisory source.
			pkgs = append(affectedPackageWithRanges(models[0]), affectedPackageWithRanges(models[1:]...)...)
		}
		readable := true
		for _, r := range ranges {
			if r.kind != "SEMVER" || !r.listedAscending() {
				readable = false
			}
		}
		if readable {
			readableAdvisories++
		} else {
			guardedAdvisories++
		}
		for _, v := range pool {
			rows++
			observed := v.String()
			before := evaluateSemverFamilyMatch(observed, "", pkgs, false)
			after := evaluateSemverFamilyMatch(observed, "", pkgs, true)
			flipped := before.Status != after.Status
			if flipped {
				flips++
				if before.Status != SupplyChainImpactPossiblyAffected || after.Status != SupplyChainImpactNotAffectedKnownFixed {
					t.Fatalf("advisory %d observed %s: %s -> %s, only possibly_affected -> known-fixed may move", advisory, observed, before.Status, after.Status)
				}
			}
			if before.Status == SupplyChainImpactAffectedExact && after.Reason != before.Reason {
				t.Fatalf("advisory %d observed %s: affected_exact reason moved %q -> %q", advisory, observed, before.Reason, after.Reason)
			}
			if flipped {
				for _, r := range ranges {
					if !r.gitHashes && r.osvAffects(v) {
						t.Fatalf("advisory %d observed %s: flipped to known-fixed but OSV says affected; ranges %+v", advisory, observed, ranges)
					}
				}
				if !readable {
					t.Fatalf("advisory %d observed %s: flipped on an advisory with an unreadable range; ranges %+v", advisory, observed, ranges)
				}
			}
			if readable {
				want := before.Status == SupplyChainImpactPossiblyAffected && oracleKnownFixed(ranges, v)
				if want != flipped {
					t.Fatalf("advisory %d observed %s: flip=%v, oracle says %v; ranges %+v", advisory, observed, flipped, want, ranges)
				}
			}
		}
	}
	if flips == 0 || flips == rows || readableAdvisories == 0 || guardedAdvisories == 0 {
		t.Fatalf("degenerate corpus: %d flips of %d rows, %d readable and %d guarded advisories", flips, rows, readableAdvisories, guardedAdvisories)
	}
	t.Logf("parity: %d rows, %d possibly_affected -> known-fixed flips, %d readable and %d guarded advisories, 0 other moves", rows, flips, readableAdvisories, guardedAdvisories)
}
