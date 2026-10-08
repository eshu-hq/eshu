// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestGenerationRetentionStoreLargeFixtureLive prunes 100 superseded
// generations (50,000 fact rows) on the real bootstrap schema and proves the
// active generation and the retained window survive. It was
// TestGenerationRetentionStoreLargeFixtureIntegration on a hand-built schema;
// #7695 ported it to the migrated-schema opener per the #6680 ruling after the
// fixture drifted (missing key indexes the store requires since #6809) and
// rotted outside every CI lane. It runs in the live-postgres-readiness runner.
func TestGenerationRetentionStoreLargeFixtureLive(t *testing.T) {
	// Bridge the live-postgres-readiness runner's family DSN onto the shared
	// helper's generic variable, as TestGenerationRetentionHardCeilingLive
	// does. Local runs keep using ESHU_POSTGRES_TEST_DSN directly.
	if dsn := strings.TrimSpace(os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")); dsn != "" {
		if os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GENERATION_RETENTION_PROOF_DSN is set without ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE=1")
		}
		t.Setenv("ESHU_POSTGRES_TEST_DSN", dsn)
	}
	database, ctx := openGenerationRetentionMigratedSchema(t)
	seedGenerationRetentionLargeFixture(t, ctx, database)

	beforeActiveFacts, beforeActiveRead := generationRetentionProofTimedCount(
		t,
		database,
		ctx,
		"SELECT COUNT(*) FROM fact_records WHERE generation_id = 'gen-active'",
	)
	if beforeActiveFacts != 500 {
		t.Fatalf("active fact_records before prune = %d, want 500", beforeActiveFacts)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: database})
	start := time.Now()
	result, err := store.PruneSupersededGenerations(ctx, GenerationRetentionPolicy{
		MinSupersededGenerations: 24,
		MaxSupersededAge:         7 * 24 * time.Hour,
		BatchGenerationLimit:     100,
		BatchRowLimit:            1_000_000,
		PolicyScope:              "global",
		PolicyRevision:           "integration-proof",
	})
	if err != nil {
		t.Fatalf("PruneSupersededGenerations() error = %v", err)
	}
	t.Logf(
		"Performance Evidence: pruned_generations=%d fact_rows=%d work_rows=%d duration=%s wall_time=%s",
		result.GenerationsPruned,
		result.RowsPruned["fact_records"],
		result.RowsPruned["fact_work_items"],
		result.Duration,
		time.Since(start),
	)
	if result.GenerationsPruned != 100 {
		t.Fatalf("GenerationsPruned = %d, want 100", result.GenerationsPruned)
	}
	if result.RowsPruned["fact_records"] != 50_000 {
		t.Fatalf("fact_records = %d, want 50000", result.RowsPruned["fact_records"])
	}
	afterActiveFacts, afterActiveRead := generationRetentionProofTimedCount(
		t,
		database,
		ctx,
		"SELECT COUNT(*) FROM fact_records WHERE generation_id = 'gen-active'",
	)
	if afterActiveFacts != beforeActiveFacts {
		t.Fatalf("active fact_records after prune = %d, want %d", afterActiveFacts, beforeActiveFacts)
	}
	retainedWindowFacts, retainedWindowRead := generationRetentionProofTimedCount(
		t,
		database,
		ctx,
		"SELECT COUNT(*) FROM fact_records WHERE generation_id = 'gen-25'",
	)
	if retainedWindowFacts != 500 {
		t.Fatalf("retained-window fact_records = %d, want 500", retainedWindowFacts)
	}
	t.Logf(
		"No-Regression Evidence: active_fact_read_before=%s active_fact_read_after=%s retained_window_fact_read=%s active_fact_rows=%d retained_window_fact_rows=%d",
		beforeActiveRead,
		afterActiveRead,
		retainedWindowRead,
		afterActiveFacts,
		retainedWindowFacts,
	)

	var remainingFacts int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM fact_records").Scan(&remainingFacts); err != nil {
		t.Fatalf("count remaining facts: %v", err)
	}
	if remainingFacts != 13_000 {
		t.Fatalf("remaining fact_records = %d, want 13000", remainingFacts)
	}
}

func generationRetentionProofTimedCount(
	t *testing.T,
	db *sql.DB,
	ctx context.Context,
	query string,
) (int, time.Duration) {
	t.Helper()
	start := time.Now()
	var count int
	if err := db.QueryRowContext(ctx, query).Scan(&count); err != nil {
		t.Fatalf("proof count query failed: %v", err)
	}
	return count, time.Since(start)
}

// seedGenerationRetentionLargeFixture writes the large-prune fixture against
// the real bootstrap schema: one active generation with 500 facts, 125
// superseded generations with 500 facts each, and one succeeded work item and
// shared intent per superseded generation. Column lists must satisfy every
// NOT NULL column and foreign key the migrations declare; the bootstrap
// provides the key indexes the store requires.
func seedGenerationRetentionLargeFixture(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()
	steps := []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, payload)
VALUES ('scope-proof', 'repository', 'github', 'acme/proof', 'git', 'acme/proof', now(), now(), 'active', '{}'::jsonb)`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ('gen-active', 'scope-proof', 'snapshot', now(), now(), 'active')`,
		`UPDATE ingestion_scopes SET active_generation_id = 'gen-active' WHERE scope_id = 'scope-proof'`,
		`INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at
)
SELECT
    'gen-' || n::text,
    'scope-proof',
    'snapshot',
    now() - interval '9 days' - (n * interval '1 minute'),
    now() - interval '9 days' - (n * interval '1 minute'),
    'superseded',
    now() - interval '8 days' - (n * interval '1 minute')
FROM generate_series(1, 125) AS n`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT
    'fact-active-' || f.n::text,
    'scope-proof',
    'gen-active',
    'fact',
    'k-active-' || f.n::text,
    'git',
    'k-active-' || f.n::text,
    now(),
    now(),
    jsonb_build_object('repo_id', 'repo-proof', 'relative_path', 'src/active_' || f.n::text || '.go')
FROM generate_series(1, 500) AS f(n)`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    source_system, source_fact_key, observed_at, ingested_at, payload)
SELECT
    'fact-' || g.n::text || '-' || f.n::text,
    'scope-proof',
    'gen-' || g.n::text,
    'fact',
    'k-' || g.n::text || '-' || f.n::text,
    'git',
    'k-' || g.n::text || '-' || f.n::text,
    now(),
    now(),
    jsonb_build_object('repo_id', 'repo-proof', 'relative_path', 'src/file_' || f.n::text || '.go')
FROM generate_series(1, 125) AS g(n)
CROSS JOIN generate_series(1, 500) AS f(n)`,
		`INSERT INTO fact_work_items (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
SELECT 'work-' || n::text, 'scope-proof', 'gen-' || n::text, 'reducer', 'domain', 'succeeded', now(), now()
FROM generate_series(1, 125) AS n`,
		`INSERT INTO shared_projection_intents (intent_id, projection_domain, partition_key, scope_id,
    acceptance_unit_id, repository_id, source_run_id, generation_id, payload, created_at)
SELECT 'intent-' || n::text, 'domain', 'p', 'scope-proof', 'unit-' || n::text, 'repo-proof', 'run-' || n::text,
    'gen-' || n::text, '{}'::jsonb, now()
FROM generate_series(1, 125) AS n`,
	}
	for i, step := range steps {
		if _, err := database.ExecContext(ctx, step); err != nil {
			t.Fatalf("seed step %d: %v", i, err)
		}
	}
}
