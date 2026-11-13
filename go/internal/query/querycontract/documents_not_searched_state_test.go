// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"math"
	"testing"
)

// #10611: documents_not_searched_state is derived purely from the integer
// beside the sentinel: 0 reads exact, a positive count reads partial, and
// the -1 count-cancelled sentinel reads unknown. The integer is unchanged.
func TestDocumentsNotSearchedStateCoversTheSentinelSpace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		notSearched int64
		want        string
	}{
		{DocumentsNotSearchedUnknownValue, DocumentsNotSearchedUnknown},
		{-1, DocumentsNotSearchedUnknown},
		{0, DocumentsNotSearchedExact},
		{1, DocumentsNotSearchedPartial},
		{7, DocumentsNotSearchedPartial},
		{139369, DocumentsNotSearchedPartial},
		{math.MaxInt64, DocumentsNotSearchedPartial},
	} {
		got, ok := DocumentsNotSearchedState(tc.notSearched)
		if !ok {
			t.Errorf("DocumentsNotSearchedState(%d) rejected, want %q", tc.notSearched, tc.want)
			continue
		}
		if got != tc.want {
			t.Errorf("DocumentsNotSearchedState(%d) = %q, want %q", tc.notSearched, got, tc.want)
		}
	}
}

// #10611: only -1 is a documented negative. Any other negative is outside
// the sentinel space and must be rejected, never silently read as exact,
// partial, or unknown.
func TestDocumentsNotSearchedStateRejectsOutsideTheSentinelSpace(t *testing.T) {
	t.Parallel()

	for _, n := range []int64{-2, -3, -100, math.MinInt64} {
		if got, ok := DocumentsNotSearchedState(n); ok {
			t.Errorf("DocumentsNotSearchedState(%d) = %q, want rejection outside the sentinel space", n, got)
		}
	}
}

// #10611 closed-enum guard: the wire states are exactly exact, partial, and
// unknown. A planted fourth state fails here, either as a new constant
// outside the set or as a mapping output outside it.
func TestDocumentsNotSearchedStateEnumIsClosed(t *testing.T) {
	t.Parallel()

	want := map[string]bool{"exact": true, "partial": true, "unknown": true}
	states := []string{DocumentsNotSearchedExact, DocumentsNotSearchedPartial, DocumentsNotSearchedUnknown}
	if len(states) != len(want) {
		t.Fatalf("state constants = %d, want exactly %d %v", len(states), len(want), states)
	}
	for _, s := range states {
		if !want[s] {
			t.Errorf("state constant %q is outside the closed enum", s)
		}
	}

	for _, n := range []int64{DocumentsNotSearchedUnknownValue, -2, -1, 0, 1, 2, 7, 139369, math.MaxInt64} {
		got, ok := DocumentsNotSearchedState(n)
		if !ok {
			continue
		}
		if !want[got] {
			t.Errorf("DocumentsNotSearchedState(%d) = %q, outside the closed enum", n, got)
		}
	}
}
