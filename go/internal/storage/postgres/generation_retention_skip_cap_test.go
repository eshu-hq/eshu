// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestGenerationRetentionStoreCandidateQueryRunsOncePerPass replaces
// TestGenerationRetentionStoreRowLimitSkipStopsAtSearchCap (arbiter ruling
// arb-7334.md section 3): the general candidate query already returns the
// full eligible set across every scope in one statement, so a pass that finds
// every candidate over the row limit reports the skips and stops without
// excluding them and re-querying for more. This is section 1's scope only
// (arb-7334c.md section 6): the narrowed path that would admit an own-rows
// over-limit generation alone instead of skipping it belongs to section 2's
// separate PR (arb-7334.md 2.1), so GenerationsPruned is 0 here, not 1.
//
// RED on the removed loop: before this change, an all-over-limit backlog
// issued one candidate query per generationRetentionSkipSearchLimit(10) == 40
// exclusions, i.e. 4 candidate queries of 10 rows each with a growing 4th
// (exclusion) argument, and reported the same Skipped count. After removing
// the loop and the exclusion parameter, this must collapse to exactly one
// candidate query, of only 4 arguments (soft cutoff, count, limit, hard
// ceiling cutoff — still no exclusion parameter), one row-count query, and
// the same Skipped total (the row_limit skip logic itself is unchanged).
func TestGenerationRetentionStoreCandidateQueryRunsOncePerPass(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	candidates := generationRetentionCandidateRows(40, now)
	db := &generationRetentionFakeDB{
		candidateRows: candidates,
		countRows: generationRetentionCountRows(candidates, map[string]int64{
			"fact_records": 101,
		}),
	}
	store := NewGenerationRetentionStore(db)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
	}
	// generationRetentionCandidateRows returns only 40 rows and the fake
	// candidate query caps its answer at args[2] (the $3 batch limit, 10
	// here), so a single pass sees exactly 10 over-limit candidates.
	if got, want := result.Skipped["row_limit"], 10; got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %#v, want empty for skipped backlog", result.RowsPruned)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 for skipped backlog", len(db.execs))
	}

	var candidateQueries int
	var countQueries int
	for _, query := range db.queries {
		switch {
		case strings.Contains(query.query, "ranked_superseded_generations"):
			candidateQueries++
			if got, want := len(query.args), 4; got != want {
				t.Fatalf("candidate query arg count = %d, want %d (soft cutoff, count, limit, hard ceiling; no exclusion parameter)", got, want)
			}
		case strings.Contains(query.query, "generation_retention_row_counts"):
			countQueries++
		}
	}
	if got, want := candidateQueries, 1; got != want {
		t.Fatalf("candidate query count = %d, want %d (the outer skip-search loop must be gone)", got, want)
	}
	if got, want := countQueries, 1; got != want {
		t.Fatalf("row-count query count = %d, want %d", got, want)
	}
}
