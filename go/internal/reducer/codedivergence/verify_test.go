// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import "testing"

// TestJaccardPinsPositiveNegativeAndBoundary proves the #6837 verify
// contract: identical shingle sets score 1.0 and admit, disjoint sets score
// 0.0 and reject, and the ship threshold from the #6834 theory proof admits
// exactly at 0.7 (7 of 10) while just-below rejects.
func TestJaccardPinsPositiveNegativeAndBoundary(t *testing.T) {
	t.Parallel()

	identical := []uint64{3, 7, 42, 99}
	if got := Jaccard(identical, []uint64{3, 7, 42, 99}); got != 1.0 {
		t.Fatalf("Jaccard(identical) = %v, want 1.0", got)
	}
	if !Admitted(identical, []uint64{3, 7, 42, 99}) {
		t.Fatal("identical sets must admit as drifted")
	}

	disjointA := []uint64{1, 2, 3}
	disjointB := []uint64{4, 5, 6}
	if got := Jaccard(disjointA, disjointB); got != 0.0 {
		t.Fatalf("Jaccard(disjoint) = %v, want 0.0", got)
	}
	if Admitted(disjointA, disjointB) {
		t.Fatal("disjoint sets must reject")
	}

	// Exactly 7 of 10: |inter| = 7, |union| = 10.
	atThresholdA := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	atThresholdB := []uint64{1, 2, 3, 4, 5, 6, 7, 9, 10}
	if got := Jaccard(atThresholdA, atThresholdB); got != 0.7 {
		t.Fatalf("Jaccard(threshold pair) = %v, want 0.7", got)
	}
	if !Admitted(atThresholdA, atThresholdB) {
		t.Fatal("similarity exactly at threshold must admit")
	}

	// 6 of 10: just below the ship threshold.
	belowA := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	belowB := []uint64{1, 2, 3, 4, 5, 6, 9, 10, 11}
	if got := Jaccard(belowA, belowB); got >= DriftedSimilarityThreshold {
		t.Fatalf("Jaccard(below pair) = %v, want below %v", got, DriftedSimilarityThreshold)
	}
	if Admitted(belowA, belowB) {
		t.Fatal("similarity below threshold must reject")
	}
}

// TestJaccardHandlesEmptyAndUnorderedInput proves the ambiguous legs: empty
// sets carry no similarity evidence (0.0, never admitted), and scoring does
// not depend on input order since persisted rows may predate the sorted
// encoding.
func TestJaccardHandlesEmptyAndUnorderedInput(t *testing.T) {
	t.Parallel()

	if got := Jaccard(nil, nil); got != 0.0 {
		t.Fatalf("Jaccard(nil, nil) = %v, want 0.0", got)
	}
	if Admitted(nil, []uint64{1, 2}) {
		t.Fatal("empty set must never admit")
	}

	ordered := []uint64{1, 2, 3, 4, 5, 6, 7, 8}
	shuffled := []uint64{8, 6, 4, 2, 1, 3, 5, 7}
	other := []uint64{1, 2, 3, 4, 5, 6, 7, 9, 10}
	if Jaccard(ordered, other) != Jaccard(shuffled, other) {
		t.Fatal("Jaccard must not depend on input order")
	}
}
