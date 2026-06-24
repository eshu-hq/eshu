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
	db := &generationRetentionFakeDB{
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
			fakeRowsAffected{n: 0}, // content_file_references prune
			fakeRowsAffected{n: 0}, // content_entities prune
			fakeRowsAffected{n: 0}, // content_files prune
			fakeRowsAffected{n: 1}, // scope_generations delete cascades owned rows
		},
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
	if result.GenerationsPruned != 1 {
		t.Fatalf("GenerationsPruned = %d, want 1", result.GenerationsPruned)
	}
	if got, want := result.RowsPruned["fact_records"], int64(3); got != want {
		t.Fatalf("fact_records pruned = %d, want %d", got, want)
	}
	if got, want := result.RowsPruned["shared_projection_intents"], int64(2); got != want {
		t.Fatalf("shared_projection_intents pruned = %d, want %d", got, want)
	}
	if len(db.execs) != 6 {
		t.Fatalf("exec count = %d, want 6", len(db.execs))
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO generation_retention_events") {
		t.Fatalf("first exec = %q, want retention event before deletion", db.execs[0].query)
	}
	last := db.execs[len(db.execs)-1]
	if !strings.Contains(last.query, "DELETE FROM scope_generations") {
		t.Fatalf("last exec = %q, want scope_generations delete last", last.query)
	}
	for _, call := range db.execs {
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
	db := &generationRetentionFakeDB{
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
	if got, want := result.Skipped["row_limit"], 1; got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %#v, want empty for skipped batch", result.RowsPruned)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 for skipped batch", len(db.execs))
	}
}

func TestGenerationRetentionStoreRowLimitSkipDoesNotBlockLaterCandidate(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	db := &generationRetentionFakeDB{
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
			fakeRowsAffected{n: 0}, // content_file_references prune
			fakeRowsAffected{n: 0}, // content_entities prune
			fakeRowsAffected{n: 0}, // content_files prune
			fakeRowsAffected{n: 1}, // scope_generations delete
		},
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
	if got, want := result.Skipped["row_limit"], 1; got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if got, want := result.GenerationsPruned, 1; got != want {
		t.Fatalf("GenerationsPruned = %d, want %d", got, want)
	}
	if got, want := result.RowsPruned["fact_records"], int64(2); got != want {
		t.Fatalf("fact_records pruned = %d, want %d", got, want)
	}
	if len(db.execs) != 6 {
		t.Fatalf("exec count = %d, want 6", len(db.execs))
	}
	deleteIDs, ok := db.execs[1].args[0].([]string)
	if !ok {
		t.Fatalf("delete ids arg type = %T, want []string", db.execs[1].args[0])
	}
	if len(deleteIDs) != 1 || deleteIDs[0] != "generation-small" {
		t.Fatalf("delete ids = %#v, want only generation-small", deleteIDs)
	}
}

func TestGenerationRetentionStoreRowLimitCountsContentCleanupRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	db := &generationRetentionFakeDB{
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
	if got, want := result.Skipped["row_limit"], 1; got != want {
		t.Fatalf("Skipped[row_limit] = %d, want %d", got, want)
	}
	if len(result.RowsPruned) != 0 {
		t.Fatalf("RowsPruned = %#v, want empty for content-row skip", result.RowsPruned)
	}
	if len(db.execs) != 0 {
		t.Fatalf("exec count = %d, want 0 for skipped content-heavy batch", len(db.execs))
	}
}

func BenchmarkGenerationRetentionStoreLargeFixture(b *testing.B) {
	now := time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
	candidates := generationRetentionCandidateRows(100, now)
	countRows := generationRetentionCountRows(candidates, map[string]int64{
		"fact_records":                        500,
		"fact_work_items":                     1,
		"fact_replay_events":                  1,
		"semantic_extraction_jobs":            2,
		"shared_projection_acceptance":        3,
		"graph_projection_phase_state":        3,
		"graph_projection_phase_repair_queue": 1,
		"iac_reachability":                    5,
		"shared_projection_intents":           4,
	})
	policy := GenerationRetentionPolicy{
		MinSupersededGenerations: 1,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "benchmark",
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		db := &generationRetentionFakeDB{
			candidateRows: candidates,
			countRows:     countRows,
			execResults:   generationRetentionExecResults(len(candidates), 400, 20, 20, 20, 100),
		}
		store := NewGenerationRetentionStore(db)
		store.Now = func() time.Time { return now }
		result, err := store.PruneSupersededGenerations(context.Background(), policy)
		if err != nil {
			b.Fatalf("PruneSupersededGenerations() error = %v", err)
		}
		if result.GenerationsPruned != 100 {
			b.Fatalf("GenerationsPruned = %d, want 100", result.GenerationsPruned)
		}
	}
}

func generationRetentionCandidateRows(count int, now time.Time) [][]any {
	rows := make([][]any, 0, count)
	for i := 0; i < count; i++ {
		rows = append(rows, []any{
			"scope-old",
			"generation-old-" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)),
			"repository",
			now.Add(time.Duration(-10-i) * 24 * time.Hour),
			now.Add(time.Duration(-11-i) * 24 * time.Hour),
		})
	}
	return rows
}

func generationRetentionCountRows(candidates [][]any, counts map[string]int64) [][]any {
	rows := make([][]any, 0, len(candidates)*len(counts))
	for _, candidate := range candidates {
		generationID, _ := candidate[1].(string)
		for tableName, count := range counts {
			rows = append(rows, []any{generationID, tableName, count})
		}
	}
	return rows
}

func generationRetentionExecResults(
	eventCount int,
	sharedProjectionIntents int64,
	contentReferences int64,
	contentEntities int64,
	contentFiles int64,
	scopeGenerations int64,
) []sql.Result {
	results := make([]sql.Result, 0, eventCount+5)
	for i := 0; i < eventCount; i++ {
		results = append(results, fakeRowsAffected{})
	}
	results = append(
		results,
		fakeRowsAffected{n: sharedProjectionIntents},
		fakeRowsAffected{n: contentReferences},
		fakeRowsAffected{n: contentEntities},
		fakeRowsAffected{n: contentFiles},
		fakeRowsAffected{n: scopeGenerations},
	)
	return results
}

type generationRetentionFakeDB struct {
	candidateRows [][]any
	countRows     [][]any
	execResults   []sql.Result
	queries       []fakeQueryCall
	execs         []fakeExecCall
}

func (db *generationRetentionFakeDB) Begin(context.Context) (Transaction, error) {
	return &generationRetentionFakeTx{db: db}, nil
}

func (db *generationRetentionFakeDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, sql.ErrConnDone
}

func (db *generationRetentionFakeDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return nil, sql.ErrConnDone
}

type generationRetentionFakeTx struct {
	db *generationRetentionFakeDB
}

func (tx *generationRetentionFakeTx) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	tx.db.queries = append(tx.db.queries, fakeQueryCall{query: query, args: args})
	switch {
	case strings.Contains(query, "ranked_superseded_generations"):
		return &queueFakeRows{rows: generationRetentionCandidateFakeRows(tx.db.candidateRows, args)}, nil
	case strings.Contains(query, "generation_retention_row_counts"):
		return &queueFakeRows{rows: tx.db.countRows}, nil
	default:
		return nil, sql.ErrNoRows
	}
}

func generationRetentionCandidateFakeRows(rows [][]any, args []any) [][]any {
	limit := len(rows)
	if len(args) >= 3 {
		if queryLimit, ok := args[2].(int); ok && queryLimit >= 0 && queryLimit < limit {
			limit = queryLimit
		}
	}
	if len(args) < 4 {
		return rows[:limit]
	}
	excluded, ok := args[3].([]string)
	if !ok || len(excluded) == 0 {
		return rows[:limit]
	}
	excludedSet := make(map[string]struct{}, len(excluded))
	for _, generationID := range excluded {
		excludedSet[generationID] = struct{}{}
	}
	filtered := make([][]any, 0, len(rows))
	for _, row := range rows {
		generationID, _ := row[1].(string)
		if _, ok := excludedSet[generationID]; ok {
			continue
		}
		filtered = append(filtered, row)
		if len(filtered) >= limit {
			break
		}
	}
	return filtered
}

func (tx *generationRetentionFakeTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	tx.db.execs = append(tx.db.execs, fakeExecCall{query: query, args: args})
	if len(tx.db.execResults) == 0 {
		return fakeResult{}, nil
	}
	result := tx.db.execResults[0]
	tx.db.execResults = tx.db.execResults[1:]
	return result, nil
}

func (tx *generationRetentionFakeTx) Commit() error { return nil }

func (tx *generationRetentionFakeTx) Rollback() error { return nil }

type fakeRowsAffected struct {
	n int64
}

func (r fakeRowsAffected) LastInsertId() (int64, error) { return 0, nil }

func (r fakeRowsAffected) RowsAffected() (int64, error) { return r.n, nil }
