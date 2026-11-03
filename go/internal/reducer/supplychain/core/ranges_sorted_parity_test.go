// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// twoPairIntervals is the independent oracle for the #9865 acceptance shape:
// the authored intervals [0, 1.5) and [2.0, 2.5), which no matcher code
// constructs. A version is affected when it falls in either half-open
// interval. It parses release segments as plain integers and shares no code
// with the matcher comparators.
func twoPairIntervalsAffected(observed string) bool {
	parts := strings.Split(observed, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil {
			return false
		}
		numbers = append(numbers, number)
	}
	for len(numbers) < 3 {
		numbers = append(numbers, 0)
	}
	atOrAbove := func(major, minor, patch int) bool {
		if numbers[0] != major {
			return numbers[0] > major
		}
		if numbers[1] != minor {
			return numbers[1] > minor
		}
		return numbers[2] >= patch
	}
	below := func(major, minor, patch int) bool { return !atOrAbove(major, minor, patch) }
	inFirst := atOrAbove(0, 0, 0) && below(1, 5, 0)
	inSecond := atOrAbove(2, 0, 0) && below(2, 5, 0)
	return inFirst || inSecond
}

// TestSortedContainmentParityOverShuffledOrders is the #9865 accuracy
// differential. Over every permutation of the unsorted two-pair event order,
// four observed versions, and every version comparator the reducer walks
// range events with, the containment answer must equal the independent
// interval oracle: 0 wrong moves.
func TestSortedContainmentParityOverShuffledOrders(t *testing.T) {
	t.Parallel()

	base := []supplychainmodel.AffectedRangeEvent{
		{Introduced: "2.0.0"},
		{Fixed: "2.5.0"},
		{Introduced: "0"},
		{Fixed: "1.5.0"},
	}
	compares := []struct {
		name    string
		compare versionCompareFunc
		decide  func(supplychainmodel.AffectedRange, string) (bool, bool)
	}{
		{"semver", compareOSVSemver, func(r supplychainmodel.AffectedRange, o string) (bool, bool) {
			return versionRangeContainsDecision(r, o, compareOSVSemver)
		}},
		{"composer", compareComposerVersion, func(r supplychainmodel.AffectedRange, o string) (bool, bool) {
			return versionRangeContainsDecision(r, o, compareComposerVersion)
		}},
		{"rubygems", compareRubyGemsVersion, func(r supplychainmodel.AffectedRange, o string) (bool, bool) {
			return versionRangeContainsDecision(r, o, compareRubyGemsVersion)
		}},
		{"pypi", comparePyPIVersion, pypiRangeContainsDecision},
		{"nuget", compareNuGetVersion, nugetSemverRangeContainsDecision},
		{"dpkg", compareDPKGVersion, func(r supplychainmodel.AffectedRange, o string) (bool, bool) {
			return versionRangeContainsDecision(r, o, compareDPKGVersion)
		}},
		{"apk", compareAPKVersion, func(r supplychainmodel.AffectedRange, o string) (bool, bool) {
			return versionRangeContainsDecision(r, o, compareAPKVersion)
		}},
	}
	observed := []string{"0.5.0", "1.5.0", "2.2.0", "3.0.0"}
	wrong, rows := 0, 0
	var indexes [4]int
	for i := range indexes {
		indexes[i] = i
	}
	var permute func(int)
	permute = func(at int) {
		if at == len(indexes) {
			events := make([]supplychainmodel.AffectedRangeEvent, 0, len(base))
			for _, index := range indexes {
				events = append(events, base[index])
			}
			for _, o := range observed {
				want := twoPairIntervalsAffected(o)
				for _, cmp := range compares {
					rows++
					got, valid := cmp.decide(supplychainmodel.AffectedRange{Kind: "SEMVER", Events: events}, o)
					if !valid {
						t.Fatalf("perm %v %s @ %s: valid = false, want true", indexes, cmp.name, o)
					}
					if got != want {
						wrong++
						t.Errorf("perm %v %s @ %s: got %v, oracle says %v", indexes, cmp.name, o, got, want)
					}
				}
			}
			return
		}
		for i := at; i < len(indexes); i++ {
			indexes[at], indexes[i] = indexes[i], indexes[at]
			permute(at + 1)
			indexes[at], indexes[i] = indexes[i], indexes[at]
		}
	}
	permute(0)
	t.Logf("parity: %d rows over 24 orders x 4 versions x 7 comparators, %d wrong moves", rows, wrong)
}

// TestSortedContainmentParityAgainstOracle is the randomized differential for
// #9865. Over seeded random advisories of SEMVER and ECOSYSTEM ranges with
// shuffled event orders, prerelease versions, and last_affected terminators,
// the shared walk must agree with oracleRange.osvAffects, which follows the
// OSV algorithm with an independent integer comparator that shares no code
// with the matcher.
func TestSortedContainmentParityAgainstOracle(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(9865))
	pool := oracleVersionPool()
	var rows, affected, shuffled int
	for advisory := 0; advisory < 300; advisory++ {
		var ranges []oracleRange
		for n := 1 + rng.Intn(3); n > 0; n-- {
			r := generateOracleRange(rng, pool)
			if r.gitHashes || r.kind == "GIT" {
				continue
			}
			rng.Shuffle(len(r.listed), func(i, j int) { r.listed[i], r.listed[j] = r.listed[j], r.listed[i] })
			if !r.listedAscending() {
				shuffled++
			}
			ranges = append(ranges, r)
		}
		if len(ranges) == 0 {
			continue
		}
		models := make([]supplychainmodel.AffectedRange, 0, len(ranges))
		for _, r := range ranges {
			models = append(models, r.model())
		}
		for _, v := range pool {
			rows++
			want := false
			for _, r := range ranges {
				if r.osvAffects(v) {
					want = true
				}
			}
			got := false
			for _, model := range models {
				affected, valid := versionRangeContainsDecision(model, v.String(), compareOSVSemver)
				if !valid {
					t.Fatalf("advisory %d observed %s: valid = false for well-formed versions", advisory, v)
				}
				if affected {
					got = true
				}
			}
			if got {
				affected++
			}
			if got != want {
				t.Fatalf("advisory %d observed %s: got %v, oracle says %v", advisory, v, got, want)
			}
		}
	}
	if rows == 0 || affected == 0 || affected == rows || shuffled == 0 {
		t.Fatalf("degenerate corpus: %d rows, %d affected, %d shuffled ranges", rows, affected, shuffled)
	}
	t.Logf("parity: %d rows, %d affected, %d shuffled ranges, 0 wrong moves", rows, affected, shuffled)
}
