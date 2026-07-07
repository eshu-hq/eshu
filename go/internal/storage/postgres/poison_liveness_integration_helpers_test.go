// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// poisonLivenessProofSchemaSQL creates the table set needed by
// countPoisonDeadLettersQuery and recoverPoisonDeadLettersQuery
// (ingestion_scopes, scope_generations, fact_work_items) plus the two extra
// tables CountActiveGenerationsByAge references (shared_projection_intents,
// graph_projection_phase_state), so the detection-gap proof can run both the
// existing generation-liveness gauge and the new poison gauge against one
// schema.
const poisonLivenessProofSchemaSQL = `
CREATE TABLE ingestion_scopes (
    scope_id            TEXT PRIMARY KEY,
    scope_kind          TEXT NOT NULL,
    source_system       TEXT NOT NULL,
    source_key          TEXT NOT NULL,
    parent_scope_id     TEXT NULL,
    collector_kind      TEXT NOT NULL,
    partition_key       TEXT NOT NULL,
    observed_at         TIMESTAMPTZ NOT NULL,
    ingested_at         TIMESTAMPTZ NOT NULL,
    status              TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE scope_generations (
    generation_id   TEXT PRIMARY KEY,
    scope_id        TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind    TEXT NOT NULL,
    freshness_hint  TEXT NULL,
    source_commit_sha TEXT NULL,
    is_delta        BOOLEAN NOT NULL DEFAULT false,
    observed_at     TIMESTAMPTZ NOT NULL,
    ingested_at     TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL,
    activated_at    TIMESTAMPTZ NULL,
    superseded_at   TIMESTAMPTZ NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE fact_work_items (
    work_item_id    TEXT PRIMARY KEY,
    scope_id        TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id   TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    stage           TEXT NOT NULL,
    domain          TEXT NOT NULL,
    conflict_domain TEXT NOT NULL DEFAULT 'scope',
    conflict_key    TEXT NULL,
    status          TEXT NOT NULL,
    attempt_count   INTEGER NOT NULL DEFAULT 0,
    lease_owner     TEXT NULL,
    claim_until     TIMESTAMPTZ NULL,
    visible_at      TIMESTAMPTZ NULL,
    last_attempt_at TIMESTAMPTZ NULL,
    next_attempt_at TIMESTAMPTZ NULL,
    failure_class   TEXT NULL,
    failure_message TEXT NULL,
    failure_details TEXT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX fact_work_items_dead_letter_poison_idx
    ON fact_work_items (scope_id, generation_id)
    WHERE status = 'dead_letter';

CREATE TABLE shared_projection_intents (
    intent_id         TEXT PRIMARY KEY,
    projection_domain TEXT NOT NULL,
    partition_key     TEXT NOT NULL,
    scope_id          TEXT NOT NULL DEFAULT '',
    acceptance_unit_id TEXT NOT NULL DEFAULT '',
    repository_id     TEXT NOT NULL,
    source_run_id     TEXT NOT NULL,
    generation_id     TEXT NOT NULL,
    partition_hash    NUMERIC(20, 0) NULL,
    payload           JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ NULL
);

CREATE TABLE graph_projection_phase_state (
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    acceptance_unit_id TEXT NOT NULL, source_run_id TEXT NOT NULL,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    keyspace TEXT NOT NULL, phase TEXT NOT NULL,
    committed_at TIMESTAMPTZ NOT NULL, updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (scope_id, acceptance_unit_id, source_run_id, generation_id, keyspace, phase)
);
`

// openPoisonLivenessProofDB opens a Postgres connection for a poison-liveness
// integration proof run. Single-threaded (MaxOpenConns=1) to avoid
// cross-connection search_path confusion, mirroring openLivenessProofDB.
func openPoisonLivenessProofDB(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// provisionPoisonLivenessSchema creates a fresh isolated proof schema, applies
// the minimal table set, and seeds it. The schema is dropped via t.Cleanup.
func provisionPoisonLivenessSchema(t *testing.T, db *sql.DB, seedSQL string) {
	t.Helper()
	ctx := context.Background()
	schemaName := fmt.Sprintf("poison_liveness_proof_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	if _, err := db.ExecContext(ctx, poisonLivenessProofSchemaSQL); err != nil {
		t.Fatalf("create proof tables: %v", err)
	}
	if seedSQL != "" {
		if _, err := db.ExecContext(ctx, seedSQL); err != nil {
			t.Fatalf("seed proof data: %v", err)
		}
	}
}
