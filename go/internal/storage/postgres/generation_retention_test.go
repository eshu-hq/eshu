// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestGenerationRetentionSchemaStoresOnlySafeIdentifiers(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS generation_retention_events",
		"event_id TEXT PRIMARY KEY",
		"scope_id_hash TEXT NOT NULL",
		"generation_id_hash TEXT NOT NULL",
		"policy_scope TEXT NOT NULL",
		"policy_revision TEXT NOT NULL",
		"row_counts JSONB NOT NULL DEFAULT '{}'::jsonb",
		"generation_retention_events_scope_idx",
	} {
		if !strings.Contains(generationRetentionEventSchemaSQL, want) {
			t.Fatalf("generation retention schema missing %q:\n%s", want, generationRetentionEventSchemaSQL)
		}
	}
	for _, forbidden := range []string{"scope_id TEXT", "generation_id TEXT", "source_key", "source_name", "repository", "raw"} {
		if strings.Contains(generationRetentionEventSchemaSQL, forbidden) {
			t.Fatalf("generation retention schema stores forbidden field %q:\n%s", forbidden, generationRetentionEventSchemaSQL)
		}
	}
	if !strings.Contains(insertGenerationRetentionEventQuery, "ON CONFLICT (event_id) DO NOTHING") {
		t.Fatalf("generation retention insert is not idempotent:\n%s", insertGenerationRetentionEventQuery)
	}
}

func TestGenerationRetentionCandidateQueryProtectsWindowAndLocks(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"ROW_NUMBER() OVER (PARTITION BY generation.scope_id",
		"generation.status = 'superseded'",
		"generation.superseded_at < $1",
		"superseded_rank > $2",
		"scope.active_generation_id",
		"status IN ('claimed', 'running', 'retrying')",
		"FOR UPDATE",
		"SKIP LOCKED",
		"LIMIT $3",
	} {
		if !strings.Contains(generationRetentionCandidateQuery, want) {
			t.Fatalf("candidate query missing %q:\n%s", want, generationRetentionCandidateQuery)
		}
	}
}

func TestGenerationRetentionRowCountsAreGenerationAware(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"SELECT candidate.generation_id, 'fact_records'",
		"GROUP BY candidate.generation_id",
		"SELECT candidate.generation_id, 'shared_projection_intents'",
		"SELECT candidate.generation_id, 'content_file_references'",
		"SELECT candidate.generation_id, 'content_entities'",
		"SELECT candidate.generation_id, 'content_files'",
	} {
		if !strings.Contains(generationRetentionRowCountsQuery, want) {
			t.Fatalf("row-count query missing %q:\n%s", want, generationRetentionRowCountsQuery)
		}
	}
}

func TestGenerationRetentionStorePrunesEligibleGenerationBatch(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{{
			"scope-old",
			"generation-old",
			"repository",
			now.Add(-10 * 24 * time.Hour),
			now.Add(-11 * 24 * time.Hour),
		}},
		countRows: [][]any{
			{"generation-old", "fact_records", int64(3)},
			{"generation-old", "fact_work_items", int64(1)},
			{"generation-old", "shared_projection_intents", int64(2)},
		},
		execResults: []sql.Result{
			fakeResult{},           // retention event insert
			fakeRowsAffected{n: 2}, // shared_projection_intents delete
			fakeRowsAffected{n: 0}, // shared_projection_unroutable_intents delete
			fakeRowsAffected{n: 0}, // content_file_references prune
			fakeResult{},           // infra inventory repository locks
			fakeRowsAffected{n: 0}, // content_entities prune
			fakeRowsAffected{n: 0}, // infra_resource_entities orphan delete
			fakeRowsAffected{n: 0}, // content_files prune
			fakeRowsAffected{n: 1}, // scope_generations delete cascades owned rows
		},
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
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	if got, want := result.RowsPruned["fact_records"], int64(3); got != want {
		t.Fatalf("fact_records pruned = %d, want %d", got, want)
	}
	if got, want := result.RowsPruned["shared_projection_intents"], int64(2); got != want {
		t.Fatalf("shared_projection_intents pruned = %d, want %d", got, want)
	}
	// 7 since #5984 added the shared_projection_unroutable_intents reap. That
	// table carries no foreign keys on purpose (an empty scope_id would make an
	// FK reject the insert that records a loss), so it does not cascade and
	// needs its own delete or the rows outlive their generation.
	// 9 since #6793: the infra_resource_entities read model takes the
	// per-repository derive locks before the content_entities prune and drops
	// its orphaned rows right after it, in the same transaction.
	if len(database.execs) != 9 {
		t.Fatalf("exec count = %d, want 9", len(database.execs))
	}
	pruneAt := -1
	for i, call := range database.execs {
		if strings.Contains(call.query, "DELETE FROM content_entities") {
			pruneAt = i
		}
	}
	if pruneAt < 1 || pruneAt+1 >= len(database.execs) {
		t.Fatalf("content_entities prune at %d, want it bracketed by the infra inventory statements", pruneAt)
	}
	if !strings.Contains(database.execs[pruneAt-1].query, "pg_advisory_xact_lock") {
		t.Fatalf("exec before content_entities prune = %q, want the infra inventory repository locks", database.execs[pruneAt-1].query)
	}
	if !strings.Contains(database.execs[pruneAt+1].query, "DELETE FROM infra_resource_entities") {
		t.Fatalf("exec after content_entities prune = %q, want the infra inventory orphan delete", database.execs[pruneAt+1].query)
	}
	if got, want := result.RowsPruned["infra_resource_entities"], int64(0); got != want {
		t.Fatalf("infra_resource_entities pruned = %d, want %d", got, want)
	}
	if _, reported := result.RowsPruned["infra_resource_entities"]; !reported {
		t.Fatal("RowsPruned must report infra_resource_entities")
	}
	if !strings.Contains(database.execs[0].query, "INSERT INTO generation_retention_events") {
		t.Fatalf("first exec = %q, want retention event before deletion", database.execs[0].query)
	}
	last := database.execs[len(database.execs)-1]
	if !strings.Contains(last.query, "DELETE FROM scope_generations") {
		t.Fatalf("last exec = %q, want scope_generations delete last", last.query)
	}
	for _, call := range database.execs {
		for _, arg := range call.args {
			if text, ok := arg.(string); ok {
				if strings.Contains(text, "scope-old") || strings.Contains(text, "generation-old") {
					t.Fatalf("retention write leaked raw scope/generation identifier in arg %q", text)
				}
			}
		}
	}
}

func TestGenerationRetentionStoreRowLimitSkipDoesNotReportRowsPruned(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{{
			"scope-old",
			"generation-old",
			"repository",
			now.Add(-10 * 24 * time.Hour),
			now.Add(-11 * 24 * time.Hour),
		}},
		countRows: [][]any{
			{"generation-old", "fact_records", int64(101)},
			{"generation-old", "fact_work_items", int64(1)},
		},
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
	if result.GenerationsPruned != 0 {
		t.Fatalf("GenerationsPruned = %d, want 0", result.GenerationsPruned)
	}
	if got, want := result.Skipped["row_limit"], 1; got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %#v, want empty for skipped batch", result.RowsPruned)
	}
	if len(database.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 for skipped batch", len(database.execs))
	}
}

func TestGenerationRetentionStoreRowLimitSkipDoesNotBlockLaterCandidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{
			{
				"scope-huge",
				"generation-huge",
				"repository",
				now.Add(-12 * 24 * time.Hour),
				now.Add(-13 * 24 * time.Hour),
			},
			{
				"scope-small",
				"generation-small",
				"repository",
				now.Add(-11 * 24 * time.Hour),
				now.Add(-12 * 24 * time.Hour),
			},
		},
		countRows: [][]any{
			{"generation-huge", "fact_records", int64(101)},
			{"generation-small", "fact_records", int64(2)},
			{"generation-small", "fact_work_items", int64(1)},
		},
		execResults: []sql.Result{
			fakeResult{},           // retention event insert for generation-small
			fakeRowsAffected{n: 0}, // shared_projection_intents delete
			fakeRowsAffected{n: 0}, // shared_projection_unroutable_intents delete
			fakeRowsAffected{n: 0}, // content_file_references prune
			fakeResult{},           // infra inventory repository locks
			fakeRowsAffected{n: 0}, // content_entities prune
			fakeRowsAffected{n: 0}, // infra_resource_entities orphan delete
			fakeRowsAffected{n: 0}, // content_files prune
			fakeRowsAffected{n: 1}, // scope_generations delete
		},
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
	if got, want := result.Skipped["row_limit"], 1; got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if got, want := result.GenerationsPruned, 1; got != want {
		t.Fatalf("GenerationsPruned = %d, want %d", got, want)
	}
	if got, want := result.RowsPruned["fact_records"], int64(2); got != want {
		t.Fatalf("fact_records pruned = %d, want %d", got, want)
	}
	// 7 since #5984 added the shared_projection_unroutable_intents reap. That
	// table carries no foreign keys on purpose (an empty scope_id would make an
	// FK reject the insert that records a loss), so it does not cascade and
	// needs its own delete or the rows outlive their generation.
	// 9 since #6793: the infra_resource_entities read model takes the
	// per-repository derive locks before the content_entities prune and drops
	// its orphaned rows right after it, in the same transaction.
	if len(database.execs) != 9 {
		t.Fatalf("exec count = %d, want 9", len(database.execs))
	}
	pruneAt := -1
	for i, call := range database.execs {
		if strings.Contains(call.query, "DELETE FROM content_entities") {
			pruneAt = i
		}
	}
	if pruneAt < 1 || pruneAt+1 >= len(database.execs) {
		t.Fatalf("content_entities prune at %d, want it bracketed by the infra inventory statements", pruneAt)
	}
	if !strings.Contains(database.execs[pruneAt-1].query, "pg_advisory_xact_lock") {
		t.Fatalf("exec before content_entities prune = %q, want the infra inventory repository locks", database.execs[pruneAt-1].query)
	}
	if !strings.Contains(database.execs[pruneAt+1].query, "DELETE FROM infra_resource_entities") {
		t.Fatalf("exec after content_entities prune = %q, want the infra inventory orphan delete", database.execs[pruneAt+1].query)
	}
	if got, want := result.RowsPruned["infra_resource_entities"], int64(0); got != want {
		t.Fatalf("infra_resource_entities pruned = %d, want %d", got, want)
	}
	if _, reported := result.RowsPruned["infra_resource_entities"]; !reported {
		t.Fatal("RowsPruned must report infra_resource_entities")
	}
	deleteIDs, ok := database.execs[1].args[0].([]string)
	if !ok {
		t.Fatalf("delete ids arg type = %T, want []string", database.execs[1].args[0])
	}
	if len(deleteIDs) != 1 || deleteIDs[0] != "generation-small" {
		t.Fatalf("delete ids = %#v, want only generation-small", deleteIDs)
	}
}

func TestGenerationRetentionStoreRowLimitCountsContentCleanupRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	database := &generationRetentionFakeDB{
		candidateRows: [][]any{{
			"scope-content",
			"generation-content",
			"repository",
			now.Add(-10 * 24 * time.Hour),
			now.Add(-11 * 24 * time.Hour),
		}},
		countRows: [][]any{
			{"generation-content", "fact_records", int64(1)},
			{"generation-content", "content_file_references", int64(101)},
		},
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
	if got, want := result.Skipped["row_limit"], 1; got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %#v, want empty for content-row skip", result.RowsPruned)
	}
	if len(database.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 for skipped content-heavy batch", len(database.execs))
	}
}
