// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// TestOSHoldingFactsAcrossRanges pins how a fact with several ECOSYSTEM ranges ranks:
// only a range holding the installed version offers a fix, and among those the
// highest fix counts (#9914).
func TestOSHoldingFactsAcrossRanges(t *testing.T) {
	t.Parallel()

	window := func(introduced, fixed string) supplychainmodel.AffectedRange {
		return supplychainmodel.AffectedRange{Kind: "ECOSYSTEM", Events: []supplychainmodel.AffectedRangeEvent{{Introduced: introduced}, {Fixed: fixed}}}
	}
	fact := func(id string, ranges ...supplychainmodel.AffectedRange) supplychainmodel.AffectedPackage {
		return supplychainmodel.AffectedPackage{FactID: id, AdvisoryID: id, Source: "debian", ReleaseLine: "host:line", AffectedRanges: ranges}
	}
	tests := []struct {
		name string
		pkgs []supplychainmodel.AffectedPackage
		want []osHoldingFact
	}{
		{
			"of two ranges holding the version, the higher fix ranks the fact",
			[]supplychainmodel.AffectedPackage{fact("a", window("0", "7.0-1"), window("0", "9.0-1")), fact("b", window("0", "8.0-1"))},
			[]osHoldingFact{{factID: "a", fixRank: 2}, {factID: "b", fixRank: 1}},
		},
		{
			"a range that does not hold the version offers no fix",
			[]supplychainmodel.AffectedPackage{fact("a", window("20.0-1", "30.0-1"), window("0", "7.0-1")), fact("b", window("0", "8.0-1"))},
			[]osHoldingFact{{factID: "a", fixRank: 1}, {factID: "b", fixRank: 2}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := osHoldingFacts("5.0-1", "debian", tc.pkgs, ""); !slices.Equal(got, tc.want) {
				t.Errorf("holders = %+v, want %+v", got, tc.want)
			}
		})
	}
}
