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

// progressiveCountIDs returns the generation ids of every planning row-count
// query in the order the fake database answered them.
func progressiveCountIDs(database *generationRetentionFakeDB) [][]string {
	var ids [][]string
	for _, query := range database.queries {
		if strings.Contains(query.query, "generation_retention_row_counts") {
			callIDs, _ := query.args[0].([]string)
			ids = append(ids, callIDs)
		}
	}
	return ids
}

// progressiveCountGenerations returns the distinct generations of the
// candidates oldest first, for window-id assertions.
func progressiveCountGenerations(now time.Time, scope string, ids ...string) [][]any {
	rows := make([][]any, 0, len(ids))
	for i, id := range ids {
		rows = append(rows, recheckCandidate(scope, id, now, 14-i))
	}
	return rows
}

// TestGenerationRetentionProgressiveWindowExtendsOnRoom pins the PR-2
// planning count (#10525): the first count covers only the maximal
// oldest-first prefix whose cumulative prescreen own-facts stay within the
// window, and the window doubles while the fit selects everything with room
// left. The final re-check still settles the batch and tallies each skip
// exactly once: no tallying happens inside the extension loop.
//
// RED on the shipped full-set count: it counts all four generations first,
// so the first window ids are the full set, not the prefix pair.
func TestGenerationRetentionProgressiveWindowExtendsOnRoom(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: progressiveCountGenerations(now, "scope-p", "g1", "g2", "g3", "g4"),
		prescreenRows: [][]any{
			{"g1", int64(40000)},
			{"g2", int64(40000)},
			{"g3", int64(40000)},
			{"g4", int64(40000)},
		},
		countRows: [][]any{
			{"g1", "fact_records", int64(1000)},
			{"g2", "fact_records", int64(1000)},
			{"g3", "fact_records", int64(1000)},
			{"g4", "fact_records", int64(1000)},
		},
		recountRows: [][]any{
			{"g1", "fact_records", int64(40000)},
			{"g2", "fact_records", int64(40000)},
			{"g3", "fact_records", int64(40000)},
			{"g4", "fact_records", int64(40000)},
		},
		execResults: generationRetentionExecResults(2, 0, 0, 0, 0, 0, 0, 2),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(100000))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if database.prescreenCalls != 1 {
		t.Fatalf("pre-screen calls = %d, want 1 (the window reuses the single pre-screen, never re-runs it)", database.prescreenCalls)
	}
	// 40k + 40k fits the window, the third 40k would exceed it: the first
	// count covers g1 and g2 only, the doubled window recounts the whole
	// prefix, then the re-check recounts the fitted pair and the re-lock
	// recounts it once more under the locks.
	want := [][]string{{"g1", "g2"}, {"g1", "g2", "g3", "g4"}, {"g1", "g2"}, {"g1", "g2"}}
	if ids := progressiveCountIDs(database); !slices.EqualFunc(ids, want, slices.Equal[[]string]) {
		t.Fatalf("row-count query ids = %v, want %v (window, doubled recount, re-check, re-lock recount)", ids, want)
	}
	// g3 and g4 lose the full-window fit and are tallied exactly once, by
	// the final re-check only: the extension loop tallies nothing.
	if got := result.Skipped["row_limit"]; got != 2 {
		t.Fatalf("Skipped[row_limit] = %d, want 2 tallied once each by the final re-check", got)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2 (g1, g2)", result.GenerationsPruned)
	}
	// The events carry the final window's recount: 40k counted rows each.
	if got := result.RowsPruned["fact_records"]; got != 80000 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 80000 (the final window's authoritative recount)", got)
	}
}

// TestGenerationRetentionProgressiveWindowExtendsWhileFitEmpty pins PR-2
// liveness (#10525): when the first window selects nothing because every
// member grew past the limit after a clean pre-screen (the facts-grew race),
// the window still extends to exhaustion instead of stalling, and the later
// generations prune. The grown members are tallied exactly once.
//
// RED on the shipped full-set count: it counts all four generations first,
// so the first window ids are the full set, not the prefix pair.
func TestGenerationRetentionProgressiveWindowExtendsWhileFitEmpty(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: progressiveCountGenerations(now, "scope-e", "g1", "g2", "g3", "g4"),
		prescreenRows: [][]any{
			{"g1", int64(40000)},
			{"g2", int64(40000)},
			{"g3", int64(40000)},
			{"g4", int64(40000)},
		},
		countRows: [][]any{
			{"g1", "fact_records", int64(200000)},
			{"g2", "fact_records", int64(150000)},
		},
		recountRows: [][]any{
			{"g1", "fact_records", int64(200000)},
			{"g2", "fact_records", int64(150000)},
			{"g3", "fact_records", int64(1000)},
			{"g4", "fact_records", int64(1000)},
		},
		execResults: generationRetentionExecResults(2, 0, 0, 0, 0, 0, 0, 2),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(100000))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if database.prescreenCalls != 1 {
		t.Fatalf("pre-screen calls = %d, want 1", database.prescreenCalls)
	}
	// The first window selects nothing (both members over on their own
	// rows); the doubled window recounts the whole prefix and admits g3
	// and g4, then the re-check and the re-lock recount the fitted pair.
	want := [][]string{{"g1", "g2"}, {"g1", "g2", "g3", "g4"}, {"g3", "g4"}, {"g3", "g4"}}
	if ids := progressiveCountIDs(database); !slices.EqualFunc(ids, want, slices.Equal[[]string]) {
		t.Fatalf("row-count query ids = %v, want %v (empty-fit window, doubled recount, re-check, re-lock recount)", ids, want)
	}
	if got := result.Skipped["row_limit_own_rows"]; got != 2 {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want 2 (g1 and g2 tallied once each)", got)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2 (g3, g4)", result.GenerationsPruned)
	}
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "DELETE FROM scope_generations") {
			if ids, _ := exec.args[0].([]string); !slices.Equal(ids, []string{"g3", "g4"}) {
				t.Fatalf("generation delete ids = %v, want [g3 g4]", ids)
			}
		}
	}
	if got := result.RowsPruned["fact_records"]; got != 2000 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 2000", got)
	}
}

// TestGenerationRetentionProgressiveWindowStopsExactlyAtLimit pins the PR-2
// window's exact-limit edge (#10525): a window whose cumulative prescreen
// own-facts land exactly on the cap stops without extending. g2 is
// deliberately absent from the pre-screen: missing ownFacts entries count as
// zero, so the window still spans g1..g3 (60k + 0 + 40k = 100k exactly), and
// the count total lands exactly on the row limit, which stops the loop on
// the limit branch. g4 is never counted: a single id-set, no extension.
//
// RED on `>=` instead of `>`: the exact 100k cumulative would close the
// window at [g1 g2] and extend, so the first id-set would not be [g1 g2 g3].
func TestGenerationRetentionProgressiveWindowStopsExactlyAtLimit(t *testing.T) {
	t.Parallel()

	if got := progressivePlanningWindow(nil, nil); got != 0 {
		t.Fatalf("progressivePlanningWindow(nil, nil) = %d, want 0 (an empty scope plans no window)", got)
	}

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: progressiveCountGenerations(now, "scope-x", "g1", "g2", "g3", "g4"),
		prescreenRows: [][]any{
			{"g1", int64(60000)},
			// g2 missing on purpose: missing ownFacts entries count as 0.
			{"g3", int64(40000)},
			{"g4", int64(40000)},
		},
		countRows: [][]any{
			{"g1", "fact_records", int64(60000)},
			{"g2", "fact_records", int64(1000)},
			{"g3", "fact_records", int64(39000)},
			{"g4", "fact_records", int64(40000)},
		},
		recountRows: [][]any{
			{"g1", "fact_records", int64(60000)},
			{"g2", "fact_records", int64(1000)},
			{"g3", "fact_records", int64(39000)},
			{"g4", "fact_records", int64(40000)},
		},
		execResults: generationRetentionExecResults(3, 0, 0, 0, 0, 0, 0, 3),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(100000))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if database.prescreenCalls != 1 {
		t.Fatalf("pre-screen calls = %d, want 1", database.prescreenCalls)
	}
	// The exact-limit window fits g1..g3 with the total exactly on the row
	// limit, so the loop stops on the limit branch: the only row-count
	// queries are the planning count and the re-lock's mandatory recount
	// (the re-check has no skip to recount).
	want := [][]string{{"g1", "g2", "g3"}, {"g1", "g2", "g3"}}
	if ids := progressiveCountIDs(database); !slices.EqualFunc(ids, want, slices.Equal[[]string]) {
		t.Fatalf("row-count query ids = %v, want %v (exact-limit window, then the re-lock recount; never extended to g4)", ids, want)
	}
	if got := result.Skipped["row_limit"]; got != 0 {
		t.Fatalf("Skipped[row_limit] = %d, want 0 (the exact-limit batch admits g1..g3 whole)", got)
	}
	if result.GenerationsPruned != 3 {
		t.Fatalf("GenerationsPruned = %d, want 3 (g1, g2, g3)", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 100000 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 100000 (60000 + 1000 + 39000)", got)
	}
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "DELETE FROM scope_generations") {
			if ids, _ := exec.args[0].([]string); !slices.Equal(ids, []string{"g1", "g2", "g3"}) {
				t.Fatalf("generation delete ids = %v, want [g1 g2 g3]", ids)
			}
		}
	}
}

// TestGenerationRetentionProgressiveWindowOfOneExhausts pins the PR-2
// batch-of-one edge (#10525): an over-budget head (prescreen 200k against
// the 100k window cap, still countable under the 500k batch limit) yields
// window=1 through the at-least-one guard, the room-left branch extends it,
// and the exhaustion stop settles the full batch. Progress (all three prune)
// plus termination (exactly four count queries) proves no infinite loop.
//
// RED on `i` instead of `max(i, 1)`: the head closes the window at 0, the
// empty window selects nothing with candidates left, and the loop never
// advances (the test times out instead of pruning).
func TestGenerationRetentionProgressiveWindowOfOneExhausts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: progressiveCountGenerations(now, "scope-b", "g1", "g2", "g3"),
		prescreenRows: [][]any{
			{"g1", int64(200000)},
			{"g2", int64(1000)},
			{"g3", int64(1000)},
		},
		countRows: [][]any{
			{"g1", "fact_records", int64(200000)},
			{"g2", "fact_records", int64(1000)},
			{"g3", "fact_records", int64(1000)},
		},
		recountRows: [][]any{
			{"g1", "fact_records", int64(200000)},
			{"g2", "fact_records", int64(1000)},
			{"g3", "fact_records", int64(1000)},
		},
		execResults: generationRetentionExecResults(3, 0, 0, 0, 0, 0, 0, 3),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), ledgerRetentionPolicy(500000))
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if database.prescreenCalls != 1 {
		t.Fatalf("pre-screen calls = %d, want 1", database.prescreenCalls)
	}
	// The head alone exceeds the window cap, so the first count covers only
	// g1; room left doubles the window twice to exhaustion, and the re-lock
	// recounts the settled batch. Four queries, then done: no infinite loop.
	want := [][]string{{"g1"}, {"g1", "g2"}, {"g1", "g2", "g3"}, {"g1", "g2", "g3"}}
	if ids := progressiveCountIDs(database); !slices.EqualFunc(ids, want, slices.Equal[[]string]) {
		t.Fatalf("row-count query ids = %v, want %v (window of one, two room-left extensions, re-lock recount)", ids, want)
	}
	if got := result.Skipped["row_limit"]; got != 0 {
		t.Fatalf("Skipped[row_limit] = %d, want 0 (the whole batch fits the 500k limit)", got)
	}
	if result.GenerationsPruned != 3 {
		t.Fatalf("GenerationsPruned = %d, want 3 (g1, g2, g3 all make progress)", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 202000 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 202000 (200000 + 1000 + 1000)", got)
	}
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "DELETE FROM scope_generations") {
			if ids, _ := exec.args[0].([]string); !slices.Equal(ids, []string{"g1", "g2", "g3"}) {
				t.Fatalf("generation delete ids = %v, want [g1 g2 g3]", ids)
			}
		}
	}
}

// TestGenerationRetentionChunkTargetStaysOutsideWindowCounts pins that the
// PR-2 window never swallows the #7981 chunk target: the over-limit
// generation stays out of every row-count query's ids while it drains its
// bounded chunk beside the pruned batch, and the pre-screen still runs once.
// GREEN before and after the window change (the countable set already fits
// one window here); the window assertions pin the coexistence.
func TestGenerationRetentionChunkTargetStaysOutsideWindowCounts(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	page := make([][]any, 0, 100)
	for i := 0; i < 100; i++ {
		page = append(page, []any{
			"chunk-fact-outside-window-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)),
			"metric", false, "", "", "",
		})
	}
	database := &generationRetentionFakeDB{
		candidateRows: progressiveCountGenerations(now, "scope-c", "generation-huge", "g1", "g2"),
		prescreenRows: [][]any{
			{"generation-huge", int64(101)},
			{"g1", int64(2)},
			{"g2", int64(2)},
		},
		countRows: [][]any{
			{"g1", "fact_records", int64(2)},
			{"g2", "fact_records", int64(2)},
		},
		chunkPageRows: page,
		execResults: append(
			[]sql.Result{fakeRowsAffected{n: 100}},
			generationRetentionExecResults(2, 0, 0, 0, 0, 0, 0, 2)...,
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
	if database.prescreenCalls != 1 {
		t.Fatalf("pre-screen calls = %d, want 1", database.prescreenCalls)
	}
	ids := progressiveCountIDs(database)
	if len(ids) == 0 {
		t.Fatal("row-count query never ran; g1 and g2 were never counted")
	}
	for _, callIDs := range ids {
		if slices.Contains(callIDs, "generation-huge") {
			t.Fatalf("row-count query ids = %v, want the chunk target outside every window count", ids)
		}
		if !slices.Equal(callIDs, []string{"g1", "g2"}) {
			t.Fatalf("row-count query ids = %v, want the countable window [g1 g2] every time", ids)
		}
	}
	if got := result.Skipped["row_limit_chunked"]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1 (generation-huge drained one chunk)", got)
	}
	if got := result.Skipped["row_limit_own_rows"]; got != 0 {
		t.Fatalf("Skipped[row_limit_own_rows] = %d, want 0 (the over-limit generation chunks, it is never skipped)", got)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2 (g1, g2)", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 104 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 104 (2 counted + 2 counted + 100 chunked)", got)
	}
	for _, exec := range database.execs {
		if strings.Contains(exec.query, "DELETE FROM scope_generations") {
			if deleteIDs, _ := exec.args[0].([]string); !slices.Equal(deleteIDs, []string{"g1", "g2"}) {
				t.Fatalf("generation delete ids = %v, want [g1 g2] (the chunk target's row survives)", deleteIDs)
			}
		}
	}
}
