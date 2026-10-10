// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "testing"

// TestMinTruthLevelRanksPartialBetweenDerivedAndFallback pins the order of the
// partial level: a bounded search that stopped early is weaker than a complete
// derived answer and stronger than an exploratory fallback.
func TestMinTruthLevelRanksPartialBetweenDerivedAndFallback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b TruthLevel
		want TruthLevel
	}{
		{"exact vs partial", TruthLevelExact, TruthLevelPartial, TruthLevelPartial},
		{"derived vs partial", TruthLevelDerived, TruthLevelPartial, TruthLevelPartial},
		{"partial vs derived", TruthLevelPartial, TruthLevelDerived, TruthLevelPartial},
		{"partial vs fallback", TruthLevelPartial, TruthLevelFallback, TruthLevelFallback},
		{"fallback vs partial", TruthLevelFallback, TruthLevelPartial, TruthLevelFallback},
	}
	for _, tc := range cases {
		if got := MinTruthLevel(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: MinTruthLevel(%q, %q) = %q, want %q", tc.name, tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSearchCursorIsZero(t *testing.T) {
	t.Parallel()

	if !(SearchCursor{}).IsZero() {
		t.Fatal("empty cursor must be zero")
	}
	if (SearchCursor{RepoID: "r", RelativePath: "a"}).IsZero() {
		t.Fatal("named cursor must not be zero")
	}
}
