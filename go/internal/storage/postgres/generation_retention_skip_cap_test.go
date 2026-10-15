// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"slices"
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
// the same Skipped total (#7334 fix 3 renames its reason to
// row_limit_own_rows; the count of skips is unchanged).
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
	// here), so a single pass sees exactly 10 over-limit candidates. Each
	// is 101 own facts over a limit of 100, so #7334 fix 3 reports
	// row_limit_own_rows, not row_limit.
	if got, want := result.Skipped["row_limit_own_rows"], 10; got != want {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want %d", got, want)
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

// TestGenerationRetentionOwnRowOverLimitReportsOwnRowsReason is #7334 fix 3
// on the recount path: a candidate whose rows outside the changed-since
// ledger exceed BatchRowLimit at count time, after a clean pre-screen, must
// report the own-rows reason row_limit_own_rows, leaving row_limit for the
// transient batch-full case an operator can distinguish. Since #7981 the
// reason marks this facts-grew race only: the next cycle's pre-screen
// diverts the candidate to a bounded chunk. RED: the store reports row_limit
// for both.
func TestGenerationRetentionOwnRowOverLimitReportsOwnRowsReason(t *testing.T) {
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
	// Ten candidates seen (batch limit 10), each over the limit on its own
	// non-ledger rows, none selected, so the re-check loop breaks after its
	// first iteration and each skip is recorded exactly once.
	if got, want := result.Skipped["row_limit_own_rows"], 10; got != want {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want %d", got, want)
	}
	if got := result.Skipped["row_limit"]; got != 0 {
		t.Fatalf("Skipped[row_limit] = %d, want 0 (no transient batch-full skip here)", got)
	}
}

// TestGenerationRetentionBatchFullKeepsTransientReason locks the other half
// of the #7334 fix-3 split: a candidate that fits alone but loses its batch
// to an older candidate keeps the transient row_limit reason and never
// reports the own-rows one. GREEN before and after the split.
func TestGenerationRetentionBatchFullKeepsTransientReason(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	candidates := generationRetentionCandidateRows(40, now)
	db := &generationRetentionFakeDB{
		candidateRows: candidates,
		countRows: generationRetentionCountRows(candidates, map[string]int64{
			"fact_records": 60,
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
	if got := result.Skipped["row_limit"]; got == 0 {
		t.Fatalf("Skipped[row_limit] = 0, want the batch-full skips recorded as transient")
	}
	if got := result.Skipped["row_limit_own_rows"]; got != 0 {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want 0 (every candidate fits alone)", got)
	}
}

// TestGenerationRetentionChunkDrainsOverLimitAlongsideBatch is #7981: a
// generation over the limit on own facts alone drains one bounded chunk per
// cycle instead of skipping forever, in the same pass that prunes the normal
// batch. generation-huge (101 own facts) never appears in a count
// statement's ids, while generation-small (2 facts) is counted and pruned,
// and the chunk's page, PK delete and generation delete prove the bounded
// statement shape from the recorder.
func TestGenerationRetentionChunkDrainsOverLimitAlongsideBatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	page := make([][]any, 0, 100)
	for i := 0; i < 100; i++ {
		page = append(page, []any{
			"huge-fact-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)),
			"metric", false, "", "", "",
		})
	}
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
			{"scope-a", "generation-small", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour), false},
		},
		prescreenRows: [][]any{
			{"generation-huge", int64(101)},
			{"generation-small", int64(2)},
		},
		countRows: [][]any{
			{"generation-small", "fact_records", int64(2)},
		},
		chunkPageRows: page,
		// The chunk's fact delete runs before the normal batch's event
		// insert and deletes, so its result leads the script.
		execResults: append(
			[]sql.Result{fakeRowsAffected{n: 100}},
			generationRetentionExecResults(1, 0, 0, 0, 0, 0, 0, 1)...,
		),
	}
	store := NewGenerationRetentionStore(database)
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
	if got := result.Skipped["row_limit_chunked"]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1 (generation-huge drained one chunk)", got)
	}
	if got := result.Skipped["row_limit_own_rows"]; got != 0 {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want 0 (the prescreen skip is a chunk now)", got)
	}
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1 (generation-small)", result.GenerationsPruned)
	}
	// The chunk's counted rows merge after the normal batch rebuilds
	// RowsPruned: 2 counted small facts plus the 100 drained huge facts.
	if got := result.RowsPruned["fact_records"]; got != 102 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 102 (2 counted + 100 chunked)", got)
	}
	if _, ok := result.PhaseDurations[GenerationRetentionPhasePruneChunkedFacts]; !ok {
		t.Fatal("PhaseDurations has no prune_chunked_facts phase for the drained chunk")
	}
	for _, query := range database.queries {
		switch {
		case strings.Contains(query.query, "generation_retention_row_counts"):
			ids, _ := query.args[0].([]string)
			if !slices.Equal(ids, []string{"generation-small"}) {
				t.Fatalf("row-count query ids = %v, want [generation-small] only", ids)
			}
		case strings.Contains(query.query, "retention: over-limit chunk page"):
			if got := query.args[2]; got != 100 {
				t.Fatalf("chunk page limit = %v, want 100 (BatchRowLimit bounds the page)", got)
			}
		}
	}
	if database.countCalls == 0 {
		t.Fatal("row-count query never ran; generation-small was never counted")
	}
	var chunkDelete, generationDelete int
	var generationDeleteIDs []string
	for _, exec := range database.execs {
		switch {
		case strings.Contains(exec.query, "retention: over-limit chunk fact delete"):
			chunkDelete++
			ids, _ := exec.args[0].([]string)
			if len(ids) != 100 {
				t.Fatalf("chunk fact delete ids = %d, want exactly the 100 paged facts", len(ids))
			}
		case strings.Contains(exec.query, "DELETE FROM scope_generations"):
			generationDelete++
			generationDeleteIDs, _ = exec.args[0].([]string)
		}
	}
	if chunkDelete != 1 {
		t.Fatalf("chunk fact deletes = %d, want 1 bounded PK-array delete", chunkDelete)
	}
	if generationDelete != 1 {
		t.Fatalf("generation deletes = %d, want 1 (the normal batch only)", generationDelete)
	}
	if !slices.Equal(generationDeleteIDs, []string{"generation-small"}) {
		t.Fatalf("generation delete ids = %v, want [generation-small] (the chunk target's row survives)", generationDeleteIDs)
	}
}

// TestGenerationRetentionChunkOnlyCycleSkipsFullCount is #7981's degenerate
// case: when the pre-screen diverts every candidate over the limit, the pass
// runs the pre-screen once, no full row count at all, prunes no generation,
// and drains one bounded chunk of the oldest over-limit candidate while the
// younger one waits silently for a later cycle.
func TestGenerationRetentionChunkOnlyCycleSkipsFullCount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	page := make([][]any, 0, 100)
	for i := 0; i < 100; i++ {
		page = append(page, []any{
			"huge-fact-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)),
			"metric", false, "", "", "",
		})
	}
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
			{"scope-a", "generation-small", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour), false},
		},
		prescreenRows: [][]any{
			{"generation-huge", int64(101)},
			{"generation-small", int64(1000)},
		},
		chunkPageRows: page,
		execResults:   []sql.Result{fakeRowsAffected{n: 100}},
	}
	store := NewGenerationRetentionStore(database)
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
	if database.prescreenCalls != 1 {
		t.Fatalf("pre-screen calls = %d, want 1", database.prescreenCalls)
	}
	if database.countCalls != 0 {
		t.Fatalf("row-count calls = %d, want 0 (nothing countable: every candidate is over the limit)", database.countCalls)
	}
	if got := result.Skipped["row_limit_chunked"]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1 (the oldest over-limit generation drained one chunk)", got)
	}
	if got := result.Skipped["row_limit_own_rows"]; got != 0 {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want 0 (no permanent skip remains on this path)", got)
	}
	totalSkipped := 0
	for _, count := range result.Skipped {
		totalSkipped += count
	}
	if totalSkipped != 1 {
		t.Fatalf("skipped total = %d, want 1 (the younger over-limit generation waits silently)", totalSkipped)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 100 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 100 (the drained chunk)", got)
	}
	if len(database.execs) != 1 {
		t.Fatalf("exec count = %d, want 1 (the chunk's fact delete only)", len(database.execs))
	}
	for _, statement := range database.statements {
		if strings.Contains(statement, "DELETE FROM scope_generations") {
			t.Fatal("chunk-only cycle deleted a generation row, want facts only")
		}
	}
}
