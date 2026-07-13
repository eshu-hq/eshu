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

// TestSeedSearchVectorScopeStateSkipsFailedGenerationScopeLive reproduces the
// real bug this fix guards: a repository scope whose ingestion failed never
// gets an active generation (ingestion_scopes.active_generation_id stays
// NULL). SeedSearchVectorScopeState's projection-state seeder used to run one
// unconditional INSERT ... SELECT across every repository scope with no
// filter excluding those, so the NOT NULL constraint on generation_id
// aborted the whole batch — crashing the reducer on every restart and
// leaving every OTHER (healthy) scope unseeded too, not just the failed one.
//
// This scope needs no facts/documents/metadata (unlike the sibling test in
// eshu_search_vector_scope_state_live_test.go, which exercises the
// pending/ready/CAS lifecycle for healthy scopes) — a failed scope is bare
// by definition, so this test is intentionally minimal and self-contained
// rather than sharing that file's larger fixture harness.
//
// Set ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE=1 and ESHU_POSTGRES_DSN to run.
func TestSeedSearchVectorScopeStateSkipsFailedGenerationScopeLive(t *testing.T) {
	if os.Getenv("ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE") != "1" {
		t.Skip("set ESHU_SEARCH_VECTOR_SCOPE_STATE_LIVE=1 and ESHU_POSTGRES_DSN to run")
	}
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}

	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	db := SQLDB{DB: sqlDB}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	identity := EshuSearchVectorIdentity{
		ProviderProfileID:  "semantic-search-default",
		SourceClass:        "search_documents",
		EmbeddingModelID:   "local-hash-v1",
		VectorIndexVersion: "vector-v1",
	}
	now := time.Now().UTC()

	// Unique prefix isolates this test run.
	scopeID := fmt.Sprintf("4233-failed-gen-live-%d:scope", time.Now().UnixNano())
	if _, err := sqlDB.ExecContext(
		ctx, `
		INSERT INTO ingestion_scopes
		  (scope_id, scope_kind, source_system, source_key, collector_kind,
		   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
		VALUES ($1::text, 'repository', 'git', $1::text, 'git', $1::text, $2, $2, 'failed', NULL,
		        jsonb_build_object('repo_id', $1::text))
		ON CONFLICT (scope_id) DO NOTHING`,
		scopeID, now,
	); err != nil {
		t.Fatalf("insert failed ingestion_scope %s: %v", scopeID, err)
	}
	t.Cleanup(func() {
		cleanCtx := context.Background()
		_, _ = sqlDB.ExecContext(cleanCtx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scopeID)
	})

	result, err := SeedSearchVectorScopeState(ctx, db, identity)
	if err != nil {
		t.Fatalf("SeedSearchVectorScopeState: %v (the failed scope must be skipped, not fatal)", err)
	}

	// Asserted with >= 1, not ==, because this can run against a database
	// that already has other failed scopes outside this test's own scope —
	// the precise, deterministic check is the scope's absence from
	// projection_state immediately below.
	if result.FailedScopesSkipped < 1 {
		t.Fatalf("FailedScopesSkipped = %d, want >= 1 (scope %s)", result.FailedScopesSkipped, scopeID)
	}

	var projectionRows int
	if err := sqlDB.QueryRowContext(
		ctx,
		`SELECT count(*) FROM eshu_search_document_projection_state WHERE scope_id = $1`, scopeID,
	).Scan(&projectionRows); err != nil {
		t.Fatalf("count projection_state for failed scope %s: %v", scopeID, err)
	}
	if projectionRows != 0 {
		t.Fatalf("failed scope %s got %d projection_state rows, want 0 (it has no active generation)", scopeID, projectionRows)
	}
}
