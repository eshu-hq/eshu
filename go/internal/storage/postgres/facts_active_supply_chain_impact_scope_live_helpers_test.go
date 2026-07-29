// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// newSupplyChainImpactScopeLiveTestDB creates an isolated schema on the
// ESHU_POSTGRES_TEST_DSN instance and the three tables
// FactStore.ListActiveSupplyChainImpactFacts reads, registering cleanup that
// drops the schema. It skips the test when no DSN is configured. Shared by
// every #5466 live suppression-scope-prefilter proof (originally defined
// alongside those tests in facts_active_supply_chain_impact_scope_live_test.go,
// split out here once that file's own deployment-context-only-suppression
// tests were removed as unreachable dead-weight -- see this package's other
// facts_active_supply_chain_impact*live_test.go files, which still depend on
// this helper).
func newSupplyChainImpactScopeLiveTestDB(t *testing.T, schema string) *sql.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5466 suppression-scope prefilter proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated proof schema: %v", err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_key TEXT NOT NULL,
    parent_scope_id TEXT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL,
    source_commit_sha TEXT NULL,
    is_delta BOOLEAN NOT NULL DEFAULT false,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);
`); err != nil {
		t.Fatalf("create isolated proof tables: %v", err)
	}

	return db
}

// seedSupplyChainImpactScopeLiveFact inserts one active ingestion scope,
// generation, and vulnerability.suppression fact so each live test can seed
// its own rows without repeating the boilerplate three-table insert.
func seedSupplyChainImpactScopeLiveFact(t *testing.T, db *sql.DB, factID, scopeSuffix, payloadJSON string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	// Each statement runs as its own ExecContext call: the extended query
	// protocol pgx's stdlib driver uses for parameterized statements only
	// supports one command per Parse, so a single semicolon-separated,
	// parameterized multi-statement Exec (unlike the no-parameter, DDL-only
	// CREATE TABLE call in newSupplyChainImpactScopeLiveTestDB) would fail.
	scopeID := "vex-scope:" + scopeSuffix
	generationID := "gen-" + scopeSuffix
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'vex_document', 'vex', $2, 'vulnerability_intelligence', 'p0', now(), now(), 'active', $3)
`, scopeID, scopeSuffix, generationID); err != nil {
		t.Fatalf("seed suppression fixture %s ingestion scope: %v", factID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'poll', now(), now(), 'active')
`, generationID, scopeID); err != nil {
		t.Fatalf("seed suppression fixture %s scope generation: %v", factID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, 'vulnerability.suppression', $4, 'vex', $5, now(), now(), FALSE, $6::jsonb)
`, factID, scopeID, generationID, "stable-"+scopeSuffix, scopeSuffix, payloadJSON); err != nil {
		t.Fatalf("seed suppression fixture %s fact record: %v", factID, err)
	}
}
