// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestGenerationRetentionStoreLargeFixtureIntegration(t *testing.T) {
	dsn := os.Getenv("ESHU_GENERATION_RETENTION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_GENERATION_RETENTION_PROOF_DSN to run the large retention proof")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	schemaName := fmt.Sprintf("retention_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	defer func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, generationRetentionProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	if _, err := db.ExecContext(ctx, generationRetentionProofSeedSQL); err != nil {
		t.Fatalf("seed proof data: %v", err)
	}
	beforeActiveFacts, beforeActiveRead := generationRetentionProofTimedCount(
		t,
		db,
		ctx,
		"SELECT COUNT(*) FROM fact_records WHERE generation_id = 'gen-active'",
	)
	if beforeActiveFacts != 500 {
		t.Fatalf("active fact_records before prune = %d, want 500", beforeActiveFacts)
	}

	store := NewGenerationRetentionStore(SQLDB{DB: db})
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
		db,
		ctx,
		"SELECT COUNT(*) FROM fact_records WHERE generation_id = 'gen-active'",
	)
	if afterActiveFacts != beforeActiveFacts {
		t.Fatalf("active fact_records after prune = %d, want %d", afterActiveFacts, beforeActiveFacts)
	}
	retainedWindowFacts, retainedWindowRead := generationRetentionProofTimedCount(
		t,
		db,
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
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM fact_records").Scan(&remainingFacts); err != nil {
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

const generationRetentionProofSchemaSQL = `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    active_generation_id TEXT NULL,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    collector_kind TEXT NOT NULL,
    source_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    status TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    superseded_at TIMESTAMPTZ NULL
);

CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE fact_work_items (
    work_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    status TEXT NOT NULL
);

CREATE TABLE fact_replay_events (
    event_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE
);

CREATE TABLE semantic_extraction_jobs (
    job_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE
);

CREATE TABLE shared_projection_acceptance (
    acceptance_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE
);

CREATE TABLE graph_projection_phase_state (
    state_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE
);

CREATE TABLE graph_projection_phase_repair_queue (
    repair_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE
);

CREATE TABLE iac_reachability (
    reachability_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE
);

CREATE TABLE shared_projection_intents (
    intent_id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL
);

CREATE TABLE content_files (
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    PRIMARY KEY (repo_id, relative_path)
);

CREATE TABLE content_entities (
    repo_id TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    PRIMARY KEY (repo_id, entity_id)
);

CREATE TABLE content_file_references (
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    reference_id TEXT NOT NULL,
    PRIMARY KEY (repo_id, relative_path, reference_id)
);
` + generationRetentionEventSchemaSQL

const generationRetentionProofSeedSQL = `
INSERT INTO ingestion_scopes (
    scope_id, active_generation_id, scope_kind, source_system, collector_kind, source_key, observed_at
) VALUES (
    'scope-proof', 'gen-active', 'repository', 'github', 'git', 'acme/proof', now()
);

INSERT INTO scope_generations (
    generation_id, scope_id, status, observed_at, superseded_at
) VALUES (
    'gen-active', 'scope-proof', 'active', now(), NULL
);

INSERT INTO scope_generations (
    generation_id, scope_id, status, observed_at, superseded_at
)
SELECT
    'gen-' || n::text,
    'scope-proof',
    'superseded',
    now() - interval '9 days' - (n * interval '1 minute'),
    now() - interval '8 days' - (n * interval '1 minute')
FROM generate_series(1, 125) AS n;

INSERT INTO fact_records (fact_id, generation_id, fact_kind, is_tombstone, payload)
SELECT
    'fact-active-' || f.n::text,
    'gen-active',
    'fact',
    FALSE,
    jsonb_build_object('repo_id', 'repo-proof', 'relative_path', 'src/active_' || f.n::text || '.go')
FROM generate_series(1, 500) AS f(n);

INSERT INTO fact_records (fact_id, generation_id, fact_kind, is_tombstone, payload)
SELECT
    'fact-' || g.n::text || '-' || f.n::text,
    'gen-' || g.n::text,
    'fact',
    FALSE,
    jsonb_build_object('repo_id', 'repo-proof', 'relative_path', 'src/file_' || f.n::text || '.go')
FROM generate_series(1, 125) AS g(n)
CROSS JOIN generate_series(1, 500) AS f(n);

INSERT INTO fact_work_items (work_id, generation_id, status)
SELECT 'work-' || n::text, 'gen-' || n::text, 'succeeded'
FROM generate_series(1, 125) AS n;

INSERT INTO shared_projection_intents (intent_id, generation_id)
SELECT 'intent-' || n::text, 'gen-' || n::text
FROM generate_series(1, 125) AS n;
`
