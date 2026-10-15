// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSearchIndexCandidateReadIsTheSelectionWithWeakerLocks pins the #7427
// read: it is byte-for-byte the shipped selection except for its marker and
// its two row-lock clauses, which become FOR NO KEY UPDATE ... SKIP LOCKED.
// An edit to the selection's lock clauses that the derivation no longer
// recognizes fails here instead of silently taking FOR UPDATE again.
func TestSearchIndexCandidateReadIsTheSelectionWithWeakerLocks(t *testing.T) {
	read := generationRetentionSearchIndexReadQuery
	if !strings.HasPrefix(read, generationRetentionSearchIndexReadMarker+"\n") {
		t.Fatalf("read does not start with its marker:\n%s", read)
	}
	if strings.Contains(read, "FOR UPDATE") {
		t.Fatalf("read still takes FOR UPDATE:\n%s", read)
	}
	if got := strings.Count(read, "FOR NO KEY UPDATE OF "); got != 2 {
		t.Fatalf("read has %d FOR NO KEY UPDATE clauses, want the selection's 2", got)
	}
	if got := strings.Count(read, "SKIP LOCKED"); got != strings.Count(generationRetentionCandidateQuery, "SKIP LOCKED") {
		t.Fatalf("read has %d SKIP LOCKED, want the selection's count", got)
	}
	back := strings.ReplaceAll(strings.TrimPrefix(read, generationRetentionSearchIndexReadMarker+"\n"), "FOR NO KEY UPDATE OF ", "FOR UPDATE OF ")
	if back != generationRetentionCandidateQuery {
		t.Fatal("read differs from the shipped selection beyond its marker and lock strength")
	}
}

// TestGenerationRetentionSkipsUndrainedAndBusyCandidates drives the store
// against the hermetic fake: the pre-delete answers every batch as drained,
// one generation's advisory lock is held elsewhere (so its batch is busy and
// it stays undrained), and the pass prunes only the other one. A key-index
// refusal still changes nothing: no pre-delete batch runs.
func TestGenerationRetentionSkipsUndrainedAndBusyCandidates(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "gen-a", "repository", old, old, false},
			{"scope-b", "gen-b", "repository", old.Add(time.Hour), old.Add(time.Hour), false},
		},
		countRows:         [][]any{{"gen-a", "fact_records", int64(1)}, {"gen-b", "fact_records", int64(1)}},
		predeleteLockBusy: []string{"gen-b"},
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(context.Background(), DefaultGenerationRetentionPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	if result.Skipped[searchIndexUndrainedSkippedReason] != 1 {
		t.Fatalf("Skipped = %v, want gen-b skipped as search_index_undrained", result.Skipped)
	}
	if result.SearchIndexBatches != 3 {
		t.Fatalf("SearchIndexBatches = %d, want 3 (gen-a: one empty page per table; gen-b: one busy try)", result.SearchIndexBatches)
	}
	for _, call := range database.queries {
		if strings.Contains(call.query, "retention: targeted candidate lock") {
			ids, _ := call.args[4].([]string)
			if len(ids) != 1 || ids[0] != "gen-a" {
				t.Fatalf("targeted re-lock ids = %v, want only the drained gen-a", ids)
			}
		}
	}

	refused := &generationRetentionFakeDB{candidateRows: database.candidateRows, missingKeyIndexes: []string{"fact_records_file_key_idx"}}
	if _, err := NewGenerationRetentionStore(refused).PruneSupersededGenerations(context.Background(), DefaultGenerationRetentionPolicy()); err == nil {
		t.Fatal("refused cycle returned nil error")
	}
	for _, statement := range refused.predeleteStatements {
		if strings.Contains(statement, "pre-delete batch") || strings.Contains(statement, "candidate read") {
			t.Fatalf("refused cycle ran pre-delete statement %q", statement)
		}
	}
}

// TestGenerationRetentionDurationExcludesThePredelete pins #7427 review F4:
// Duration (eshu_dp_generation_retention_duration_seconds) stays the prune
// transaction's time; the pre-delete's committed batches are reported only
// through their own phases and the batches histogram.
func TestGenerationRetentionDurationExcludesThePredelete(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-10 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows:       [][]any{{"scope-a", "gen-a", "repository", old, old, false}},
		countRows:           [][]any{{"gen-a", "fact_records", int64(1)}},
		predeleteBatchDelay: 40 * time.Millisecond,
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	result, err := store.PruneSupersededGenerations(context.Background(), DefaultGenerationRetentionPolicy())
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	predelete := result.PhaseDurations[GenerationRetentionPhasePredeleteSearchIndex] +
		result.PhaseDurations[GenerationRetentionPhaseSweepSearchIndex]
	if predelete < 80*time.Millisecond {
		t.Fatalf("pre-delete phases = %v, want at least the two 40ms batches", predelete)
	}
	if result.Duration >= 40*time.Millisecond {
		t.Fatalf("Duration = %v includes the pre-delete (%v); want the prune transaction only", result.Duration, predelete)
	}
}
