// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// chunkPredrainPage builds one fixture page of n metric facts: no file or
// entity keys, so batches run only the fact delete and never touch the
// content or mirror statements.
func chunkPredrainPage(prefix string, n int) [][]any {
	page := make([][]any, 0, n)
	for i := 0; i < n; i++ {
		page = append(page, []any{prefix, "metric", false, "", "", ""})
	}
	return page
}

func chunkPredrainPolicy() GenerationRetentionPolicy {
	return GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
		PolicyScope:              "global",
		PolicyRevision:           "test-revision",
	}
}

// TestGenerationRetentionChunkPredrainDrainsToFitAndPrunesWhole proves the
// lock-scope fix end to end on the double: an over-limit generation drains
// three bounded pages in committed batches before Begin, the selection then
// sees it fit, and one pass prunes both generations whole. The locked
// recount and the planning counts land on separately observable phases, and
// no page drains under the lock.
func TestGenerationRetentionChunkPredrainDrainsToFitAndPrunesWhole(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
			{"scope-a", "generation-small", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour), false},
		},
		// The predrain read sees the over-limit generation; the
		// selection, running after the committed batches, sees it
		// drained.
		prescreenScripts: [][][]any{
			{{"generation-huge", int64(250)}, {"generation-small", int64(2)}},
			{{"generation-huge", int64(0)}, {"generation-small", int64(2)}},
		},
		countRows: [][]any{
			{"generation-huge", "fact_records", int64(0)},
			{"generation-small", "fact_records", int64(2)},
		},
		chunkPageScripts: [][][]any{
			chunkPredrainPage("huge-fact-a", 100),
			chunkPredrainPage("huge-fact-b", 100),
			chunkPredrainPage("huge-fact-c", 50),
		},
		execResults: append(
			[]sql.Result{fakeRowsAffected{n: 100}, fakeRowsAffected{n: 100}, fakeRowsAffected{n: 50}},
			generationRetentionExecResults(2, 0, 0, 0, 0, 0, 0, 2)...,
		),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), chunkPredrainPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 2 {
		t.Fatalf("GenerationsPruned = %d, want 2 (the drained generation prunes whole beside the small one)", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 252 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 252 (250 predrained + 2 counted)", got)
	}
	if len(result.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want none (nothing deferred: the target fit after its drain)", result.Skipped)
	}
	if got := result.ChunkPredeleteBatches; got != 3 {
		t.Fatalf("ChunkPredeleteBatches = %d, want 3 (100 + 100 + short 50)", got)
	}
	for _, phase := range []string{
		GenerationRetentionPhasePredrainChunk,
		GenerationRetentionPhaseLockedRecount,
		GenerationRetentionPhaseCountRows,
	} {
		if _, ok := result.PhaseDurations[phase]; !ok {
			t.Fatalf("PhaseDurations has no %s phase", phase)
		}
	}
}

// TestGenerationRetentionChunkPredrainBudgetStopsAndDefers proves the row
// cap bounds one pass's pre-delete: after the second batch pushes the pass
// over the cap the target stops mid-drain, the pass prunes nothing, and
// the drained rows merge with the deferral skip for the next pass.
func TestGenerationRetentionChunkPredrainBudgetStopsAndDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
		},
		prescreenRows: [][]any{{"generation-huge", int64(250)}},
		chunkPageScripts: [][][]any{
			chunkPredrainPage("huge-fact-a", 100),
			chunkPredrainPage("huge-fact-b", 100),
			chunkPredrainPage("huge-fact-c", 50),
		},
		execResults: []sql.Result{fakeRowsAffected{n: 100}, fakeRowsAffected{n: 100}},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	policy := chunkPredrainPolicy()
	policy.ChunkPredeleteRowCap = 150
	result, err := store.PruneSupersededGenerations(context.Background(), policy)
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0 (the budget stopped the drain before the short page)", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 200 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 200 (two committed pages stay deleted)", got)
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1", got)
	}
	if got := result.ChunkPredeleteBatches; got != 2 {
		t.Fatalf("ChunkPredeleteBatches = %d, want 2 (the third batch never starts past the cap)", got)
	}
}

// TestGenerationRetentionChunkPredrainBusyLockDefers proves a generation
// whose advisory lock another pass holds is left alone: no batch runs, the
// exclusion tallies the deferral, and the pass prunes nothing.
func TestGenerationRetentionChunkPredrainBusyLockDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
		},
		prescreenRows:     [][]any{{"generation-huge", int64(250)}},
		predeleteLockBusy: []string{"generation-huge"},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), chunkPredrainPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %v, want none (no batch ran)", result.RowsPruned)
	}
	// The busy advisory lock stops the search drain too (same lock class),
	// so admission — not the chunk gate — excludes the generation as
	// search_index_undrained before the chunk target is ever diverted.
	if got := result.Skipped[searchIndexUndrainedSkippedReason]; got != 1 {
		t.Fatalf("Skipped[%s] = %d, want 1", searchIndexUndrainedSkippedReason, got)
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 0 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 0 (never diverted, never deferred as a chunk)", got)
	}
	if got := result.ChunkPredeleteBatches; got != 0 {
		t.Fatalf("ChunkPredeleteBatches = %d, want 0", got)
	}
	if _, ok := result.PhaseDurations[GenerationRetentionPhasePredrainChunk]; ok {
		t.Fatal("PhaseDurations has predrain_chunk with no committed batch")
	}
}

// TestGenerationRetentionChunkPredrainIneligibleStopsMidDrain proves the
// per-batch eligibility re-check: the first batch drains, the second finds
// the generation flipped ineligible and rolls back with nothing applied,
// and the pass merges the first batch's rows with the deferral skip.
func TestGenerationRetentionChunkPredrainIneligibleStopsMidDrain(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	healthy := []any{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false}
	database := &generationRetentionFakeDB{
		candidateRows:       [][]any{healthy},
		prescreenRows:       [][]any{{"generation-huge", int64(250)}},
		predrainLockScripts: [][][]any{{healthy}, {}},
		chunkPageScripts: [][][]any{
			chunkPredrainPage("huge-fact-a", 100),
			chunkPredrainPage("huge-fact-b", 100),
		},
		execResults: []sql.Result{fakeRowsAffected{n: 100}},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), chunkPredrainPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
	}
	if got := result.RowsPruned["fact_records"]; got != 100 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 100 (only the eligible batch stays deleted)", got)
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1", got)
	}
	// The dropped target is never relocked, so no refit recounts it: the
	// exclusion is structural (not locked, not pruned), and the single
	// deferral skip above is its only signal.
	if got := result.ChunkPredeleteBatches; got != 1 {
		t.Fatalf("ChunkPredeleteBatches = %d, want 1 (the ineligible batch rolls back uncounted)", got)
	}
}

// TestGenerationRetentionChunkPredrainEventCountsAndFinish proves the
// attribution: pruned generations carry their predrained rows in their
// retention events and the counter, while generations the pass leaves
// behind merge their rows with the deferral skip instead of an event.
func TestGenerationRetentionChunkPredrainEventCountsAndFinish(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	target := generationRetentionCandidate{scopeID: "scope-a", generationID: "generation-huge", supersededAt: now.Add(-time.Hour)}
	left := generationRetentionCandidate{scopeID: "scope-b", generationID: "generation-small", supersededAt: now.Add(-time.Hour)}
	predrain := &generationRetentionChunkPredrain{
		targets: []generationRetentionCandidate{target, left},
		chunks: map[string]generationRetentionChunk{
			"generation-huge":  {factsDeleted: 100, filesPruned: 8, factKeysPruned: map[string]int64{"relationship_reference_candidate_keys": 100}},
			"generation-small": {factsDeleted: 50},
		},
		touched: map[string]bool{"generation-huge": true, "generation-small": true},
	}
	perGeneration := map[string]map[string]int64{
		"generation-huge": {"fact_records": 0},
	}
	counts := predrain.eventCounts(perGeneration)
	if got := counts["generation-huge"]["fact_records"]; got != 100 {
		t.Fatalf("event fact_records = %d, want 100 (the recount's remainder plus the predrained page)", got)
	}
	if got := counts["generation-huge"]["content_files"]; got != 8 {
		t.Fatalf("event content_files = %d, want 8", got)
	}
	if got := counts["generation-huge"]["relationship_reference_candidate_keys"]; got != 100 {
		t.Fatalf("event key sidecars = %d, want 100", got)
	}
	if _, ok := counts["generation-small"]; ok {
		t.Fatalf("event counts carry an unpruned generation: %v", counts)
	}

	result := newGenerationRetentionResult()
	predrain.finish(context.Background(), nil, &result, []generationRetentionCandidate{target}, now)
	if got := result.RowsPruned["fact_records"]; got != 150 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 150 (pruned rows plus deferred progress)", got)
	}
	if got := result.RowsPruned["content_files"]; got != 8 {
		t.Fatalf("RowsPruned[content_files] = %d, want 8", got)
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1 (only the generation left behind)", got)
	}
}

// TestGenerationRetentionChunkPredrainLockTimeoutStopsAndDefers proves a
// lock_timeout (SQLSTATE 55P03) inside a predrain batch is a graceful stop,
// not a pass failure: the timed-out statement applied nothing, so the batch
// transaction rolls back, the generation stays undrained for this pass, the
// exclusion tallies the deferral skip, and the next pass retries it.
func TestGenerationRetentionChunkPredrainLockTimeoutStopsAndDefers(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour), false},
		},
		prescreenRows: [][]any{{"generation-huge", int64(250)}},
		chunkPageScripts: [][][]any{
			chunkPredrainPage("huge-fact-a", 100),
		},
		chunkDeleteFactsErr: &pgconn.PgError{Code: "55P03", Message: "canceling statement due to lock timeout"},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }

	result, err := store.PruneSupersededGenerations(context.Background(), chunkPredrainPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v, want nil (a lock_timeout stops the drain, not the pass)", err)
	}
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0 (the timed-out batch rolled back, nothing was drained)", result.GenerationsPruned)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %v, want none (no batch committed)", result.RowsPruned)
	}
	if got := result.Skipped[generationRetentionChunkedProgressSkippedReason]; got != 1 {
		t.Fatalf("Skipped[row_limit_chunked] = %d, want 1 (the undrained target waits for the next pass)", got)
	}
	if got := result.ChunkPredeleteBatches; got != 0 {
		t.Fatalf("ChunkPredeleteBatches = %d, want 0 (the timed-out batch rolls back uncounted)", got)
	}
	if _, ok := result.PhaseDurations[GenerationRetentionPhasePredrainChunk]; ok {
		t.Fatal("PhaseDurations has predrain_chunk with no committed batch")
	}
}
