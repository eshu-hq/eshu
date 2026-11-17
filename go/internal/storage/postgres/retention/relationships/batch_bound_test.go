// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshipretention

import (
	"slices"
	"testing"
)

// TestDefaultBatchSizeBoundsEveryBatch pins the default batch size. Every other
// test that bounds a batch lowers the size through Options.BatchGenerations, so
// a default that stopped bounding it (an unbounded size when the option is
// unset) would pass them all and put the whole census list into one
// transaction. With default options, 120 orphans must go through in batches of
// 50, 50 and 20, each statement of a batch seeing at most that many ids, and
// the batches must walk the census list in order, each id once.
func TestDefaultBatchSizeBoundsEveryBatch(t *testing.T) {
	t.Parallel()
	const listed = 120
	list := ids("g", listed)
	s := script{evidence: list, rows: [3]int64{1, 1, 1}}
	result, database, err := sweepWith(t, s, Options{Deletes: true})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if BatchGenerations != 50 {
		t.Fatalf("BatchGenerations = %d, want the measured 50", BatchGenerations)
	}
	want := []int{50, 50, 20}
	for name, statement := range map[string]string{
		"try lock":           tryLockQuery,
		"classify":           classifyQuery,
		"delete evidence":    deleteEvidenceQuery,
		"delete candidates":  deleteCandidatesQuery,
		"delete generations": deleteGenerationsQuery,
	} {
		var got []int
		for _, c := range database.callsFor(statement) {
			got = append(got, len(idsArg(c)))
		}
		if !slices.Equal(got, want) {
			t.Errorf("%s received %v ids per batch, want %v: the default batch size must bound every batch", name, got, want)
		}
	}
	var walked []string
	for _, c := range database.callsFor(tryLockQuery) {
		walked = append(walked, idsArg(c)...)
	}
	if !slices.Equal(walked, list) {
		t.Errorf("the batches walked %d ids, want the %d listed ids once each, in census order", len(walked), listed)
	}
	if result.Batches != 3 || result.GenerationsSwept != listed {
		t.Fatalf("pass = %d batches, %d generations swept, want 3 batches and all %d", result.Batches, result.GenerationsSwept, listed)
	}
}
