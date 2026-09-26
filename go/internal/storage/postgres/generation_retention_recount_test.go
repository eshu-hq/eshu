// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestGenerationRetentionStoreRecountsAfterRowLimitSkip pins the recount a
// row-limit skip triggers. The skipped generation stays on disk, so its facts
// protect keys the first count charged to the selected generation; the events
// and RowsPruned must carry the recount over the selected ids only (#6809).
func TestGenerationRetentionStoreRecountsAfterRowLimitSkip(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-huge", "repository", now.Add(-12 * 24 * time.Hour), now.Add(-13 * 24 * time.Hour)},
			{"scope-a", "generation-small", "repository", now.Add(-11 * 24 * time.Hour), now.Add(-12 * 24 * time.Hour)},
		},
		countRows: [][]any{
			{"generation-huge", "fact_records", int64(101)},
			{"generation-small", "fact_records", int64(2)},
			{"generation-small", "content_entities", int64(5)},
		},
		// With generation-huge retained, the shared keys stay: nothing is doomed.
		recountRows: [][]any{
			{"generation-small", "fact_records", int64(2)},
			{"generation-small", "content_entities", int64(0)},
		},
		execResults: generationRetentionExecResults(1, 0, 0, 0, 0, 0, 0, 1),
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
	if got := result.Skipped["row_limit"]; got != 1 {
		t.Fatalf("Skipped[row_limit] = %d, want 1", got)
	}
	var countIDs [][]string
	for _, query := range database.queries {
		if strings.Contains(query.query, "generation_retention_row_counts") {
			ids, _ := query.args[0].([]string)
			countIDs = append(countIDs, ids)
		}
	}
	want := [][]string{{"generation-huge", "generation-small"}, {"generation-small"}}
	if !slices.EqualFunc(countIDs, want, slices.Equal[[]string]) {
		t.Fatalf("row-count query ids = %v, want %v (a recount over the selected id)", countIDs, want)
	}
	var event map[string]int64
	if err := json.Unmarshal(database.execs[0].args[9].([]byte), &event); err != nil {
		t.Fatalf("decode event row_counts: %v", err)
	}
	if event["content_entities"] != 0 || event["fact_records"] != 2 {
		t.Fatalf("event row_counts = %v, want the recount (content_entities 0, fact_records 2)", event)
	}
	if got := result.RowsPruned["fact_records"]; got != 2 {
		t.Fatalf("RowsPruned[fact_records] = %d, want 2", got)
	}
}

// TestGenerationRetentionStoreCountsCandidatesOldestFirst pins the order the
// row count receives its ids: oldest superseded first, generation id breaking
// ties. The count attributes a shared content row to the last id naming it, so
// the order decides which event carries that row.
func TestGenerationRetentionStoreCountsCandidatesOldestFirst(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	older := now.Add(-12 * 24 * time.Hour)
	newer := now.Add(-11 * 24 * time.Hour)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{"scope-a", "generation-newer", "repository", newer, newer},
			{"scope-a", "generation-tie-b", "repository", older, older},
			{"scope-a", "generation-tie-a", "repository", older, older},
		},
		execResults: generationRetentionExecResults(3, 0, 0, 0, 0, 0, 0, 3),
	}
	store := NewGenerationRetentionStore(database)
	store.Now = func() time.Time { return now }
	if _, err := store.PruneSupersededGenerations(context.Background(), GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     10,
		BatchRowLimit:            100,
	}); err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	for _, query := range database.queries {
		if strings.Contains(query.query, "generation_retention_row_counts") {
			want := []string{"generation-tie-a", "generation-tie-b", "generation-newer"}
			if ids, _ := query.args[0].([]string); !slices.Equal(ids, want) {
				t.Fatalf("row-count ids = %v, want %v", ids, want)
			}
			return
		}
	}
	t.Fatal("no row-count query issued")
}
