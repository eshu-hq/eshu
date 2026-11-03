// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// eventRange builds one range from events written as "i:2.0.0", "f:2.5.0",
// "l:3.5.0" (introduced, fixed, last_affected) and "limit:9.0.0".
func eventRange(kind string, events ...string) supplychainmodel.AffectedRange {
	out := supplychainmodel.AffectedRange{Kind: kind}
	for _, raw := range events {
		var event supplychainmodel.AffectedRangeEvent
		switch {
		case len(raw) > 6 && raw[:6] == "limit:":
			event.Limit = raw[6:]
		case raw[:2] == "i:":
			event.Introduced = raw[2:]
		case raw[:2] == "f:":
			event.Fixed = raw[2:]
		case raw[:2] == "l:":
			event.LastAffected = raw[2:]
		}
		out.Events = append(out.Events, event)
	}
	return out
}

// TestRangeEventsAreEvaluatedInVersionOrder pins #9865: the OSV algorithm
// evaluates a range's events sorted by version, and the schema does not
// require a feed to sort them. A range listed out of order must contain the
// same versions as its sorted twin, for every walk family.
func TestRangeEventsAreEvaluatedInVersionOrder(t *testing.T) {
	t.Parallel()
	// possibly stands for "not affected_exact": the per-line known-fixed rule
	// (#9854) may turn an npm version outside every range into
	// not_affected_known_fixed, which is also correct.
	const (
		affected = "affected_exact"
		possibly = "not_affected_exact"
	)
	// Every ecosystem family that reads typed range events: the shared semver
	// walk (npm, cargo, hex, swift, pub), go, pypi, nuget, composer, rubygems,
	// and the two OS-package families (dpkg, apk) that share the same walk.
	families := []struct {
		ecosystem string
		kind      string
	}{
		{"npm", "SEMVER"},
		{"cargo", "SEMVER"},
		{"hex", "SEMVER"},
		{"swift", "SEMVER"},
		{"pub", "SEMVER"},
		{"go", "SEMVER"},
		{"go", "ECOSYSTEM"},
		{"pypi", "ECOSYSTEM"},
		{"pypi", "SEMVER"},
		{"nuget", "SEMVER"},
		{"composer", "SEMVER"},
		{"rubygems", "SEMVER"},
		{"rubygems", "ECOSYSTEM"},
		{"debian", "ECOSYSTEM"},
		{"alpine", "ECOSYSTEM"},
	}
	shapes := []struct {
		name   string
		events []string
		// verdicts maps an observed version to the OSV verdict.
		verdicts map[string]string
	}{
		{
			// The issue shape: two pairs, the later-listed pair is lower.
			name:   "two pairs, higher pair listed first",
			events: []string{"i:2.0.0", "f:2.5.0", "i:0", "f:1.5.0"},
			verdicts: map[string]string{
				"2.2.0": affected, "1.0.0": affected, "1.7.0": possibly, "2.5.0": possibly, "3.0.0": possibly,
			},
		},
		{
			name:   "sorted twin of the issue shape",
			events: []string{"i:0", "f:1.5.0", "i:2.0.0", "f:2.5.0"},
			verdicts: map[string]string{
				"2.2.0": affected, "1.0.0": affected, "1.7.0": possibly, "2.5.0": possibly, "3.0.0": possibly,
			},
		},
		{
			name:   "three pairs fully reversed",
			events: []string{"i:5.0.0", "f:5.5.0", "i:3.0.0", "f:3.5.0", "i:1.0.0", "f:1.5.0"},
			verdicts: map[string]string{
				"1.2.0": affected, "3.2.0": affected, "5.2.0": affected, "2.0.0": possibly, "4.0.0": possibly, "6.0.0": possibly,
			},
		},
		{
			name:   "terminators listed before their introduced events",
			events: []string{"f:1.5.0", "f:3.5.0", "i:3.0.0", "i:0"},
			verdicts: map[string]string{
				"1.0.0": affected, "3.2.0": affected, "2.0.0": possibly, "4.0.0": possibly,
			},
		},
		{
			name:   "last_affected pair listed before a lower fixed pair",
			events: []string{"i:3.0.0", "l:3.5.0", "i:0", "f:1.5.0"},
			verdicts: map[string]string{
				"3.2.0": affected, "3.5.0": affected, "1.0.0": affected, "2.0.0": possibly, "3.6.0": possibly,
			},
		},
		{
			name:   "open introduced listed before a lower closed pair",
			events: []string{"i:4.0.0", "i:0", "f:1.5.0"},
			verdicts: map[string]string{
				"5.0.0": affected, "1.0.0": affected, "2.0.0": possibly,
			},
		},
	}
	for _, family := range families {
		for _, shape := range shapes {
			for observed, want := range shape.verdicts {
				t.Run(family.ecosystem+"/"+family.kind+"/"+shape.name+"/"+observed, func(t *testing.T) {
					t.Parallel()
					pkgs := affectedPackageWithRanges(eventRange(family.kind, shape.events...))
					got := evaluateSupplyChainVersionMatch(family.ecosystem, observed, "", "", pkgs)
					isAffected := string(got.Status) == affected
					if isAffected != (want == affected) {
						t.Fatalf("status=%q reason=%q, want %s (events %v)", got.Status, got.Reason, want, shape.events)
					}
				})
			}
		}
	}
}

// TestRangeEventOrderKeepsListedOrderForSameVersionTies pins that a tie at one
// version keeps the order the feed listed: OSV leaves it undefined, and the
// walk's answer for a tie must not change with this fix.
func TestRangeEventOrderKeepsListedOrderForSameVersionTies(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		events   []string
		observed string
		want     bool
	}{
		{"fixed then introduced at 1.5.0 reads affected", []string{"i:0", "f:1.5.0", "i:1.5.0", "f:2.0.0"}, "1.5.0", true},
		{"introduced then fixed at 1.5.0 reads not affected", []string{"i:0", "i:1.5.0", "f:1.5.0", "f:2.0.0"}, "1.5.0", false},
		{"tie below the observed version, later listed wins", []string{"f:1.0.0", "i:1.0.0"}, "1.5.0", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, valid := semverRangeContainsDecision(eventRange("SEMVER", tc.events...), tc.observed)
			if !valid || got != tc.want {
				t.Fatalf("contains=%v valid=%v, want %v true", got, valid, tc.want)
			}
		})
	}
}

// TestRangeEventOrderFailsClosedOnUncomparableEvents pins that an event
// version no comparator can read still makes the range malformed, wherever it
// sits in the list.
func TestRangeEventOrderFailsClosedOnUncomparableEvents(t *testing.T) {
	t.Parallel()
	for _, events := range [][]string{
		{"i:2.0.0", "f:not-a-version", "i:0", "f:1.5.0"},
		{"i:not-a-version", "f:2.5.0", "i:0", "f:1.5.0"},
		{"i:2.0.0", "f:2.5.0", "i:0", "l:oops"},
	} {
		if got, valid := semverRangeContainsDecision(eventRange("SEMVER", events...), "2.2.0"); valid || got {
			t.Fatalf("events %v: contains=%v valid=%v, want false false", events, got, valid)
		}
	}
}

// TestGoModuleFloorLiftsAPseudoVersionThroughTheMatcher pins that the Go
// matcher keeps its introduced "0" floor after the walk changed: a
// pseudo-version sorts below v0.0.0 in plain semver, yet "introduced 0" means
// every version, so an out-of-order range holding the floor still affects it.
func TestGoModuleFloorLiftsAPseudoVersionThroughTheMatcher(t *testing.T) {
	t.Parallel()
	const pseudo = "v0.0.0-20200101000000-abcdefabcdef"
	for _, events := range [][]string{
		{"i:0", "f:1.5.0"},
		{"f:1.5.0", "i:0"},
	} {
		pkgs := affectedPackageWithRanges(eventRange("SEMVER", events...))
		got := evaluateSupplyChainVersionMatch("go", pseudo, "", "", pkgs)
		if string(got.Status) != "affected_exact" {
			t.Fatalf("events %v: status=%q reason=%q, want affected_exact", events, got.Status, got.Reason)
		}
	}
}
