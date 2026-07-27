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

// TestListActiveContainerImageCIFactsOwnerScopedQueryLive is the #5810 P1
// follow-up live proof, mirroring
// TestContentEntitiesK8sSelectPartialIndexApplyReapplyAndRollbackLive's
// isolated-schema pattern: migration 082
// (fact_records_active_container_image_ci_run_repository_idx) applies
// cleanly, reapplies as a no-op, and rolls back, on a store populated with
// TWO repositories sharing the CI fact family -- proving the owner-scoped
// query returns exactly one repository's ci.run/ci.artifact rows and never
// the other's, against a real Postgres planner and index, not only the
// query-text assertions the fake-DB unit tests in
// facts_active_container_image_ci_test.go check.
func TestListActiveContainerImageCIFactsOwnerScopedQueryLive(t *testing.T) {
	const schema = "eshu_5843_ci_owner_index_live"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #5810 P1 owner-scoped CI query proof")
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

-- Two repositories, each with one CI-run scope: owner-scope-1 names
-- repository:r_owner, sibling-scope-2 names a DIFFERENT repository. Both
-- produce a ci.run plus one container-image ci.artifact, so a query that
-- fails to bound by repository would return both repositories' rows.
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES
    ('ci-run-scope:owner', 'ci_cd_run', 'ci_cd_run', 'run-owner', 'ci_cd_run', 'p0', now(), now(), 'active', 'gen-owner'),
    ('ci-run-scope:sibling', 'ci_cd_run', 'ci_cd_run', 'run-sibling', 'ci_cd_run', 'p0', now(), now(), 'active', 'gen-sibling');

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
    ('gen-owner', 'ci-run-scope:owner', 'poll', now(), now(), 'active'),
    ('gen-sibling', 'ci-run-scope:sibling', 'poll', now(), now(), 'active');

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES
    ('ci.run:owner', 'ci-run-scope:owner', 'gen-owner', 'ci.run', 'stable-owner', 'ci_cd_run', 'run-owner', now(), now(), FALSE,
     '{"provider":"github_actions","run_id":"run-owner","run_attempt":"1","repository_id":"repository:r_owner"}'::jsonb),
    ('ci.artifact:owner', 'ci-run-scope:owner', 'gen-owner', 'ci.artifact', 'stable-artifact-owner', 'ci_cd_run', 'artifact-owner', now(), now(), FALSE,
     '{"provider":"github_actions","run_id":"run-owner","run_attempt":"1","artifact_type":"container_image","artifact_digest":"sha256:owner"}'::jsonb),
    ('ci.run:sibling', 'ci-run-scope:sibling', 'gen-sibling', 'ci.run', 'stable-sibling', 'ci_cd_run', 'run-sibling', now(), now(), FALSE,
     '{"provider":"github_actions","run_id":"run-sibling","run_attempt":"1","repository_id":"repository:r_sibling"}'::jsonb),
    ('ci.artifact:sibling', 'ci-run-scope:sibling', 'gen-sibling', 'ci.artifact', 'stable-artifact-sibling', 'ci_cd_run', 'artifact-sibling', now(), now(), FALSE,
     '{"provider":"github_actions","run_id":"run-sibling","run_attempt":"1","artifact_type":"container_image","artifact_digest":"sha256:sibling"}'::jsonb);
`); err != nil {
		t.Fatalf("seed two-repository CI fixture: %v", err)
	}

	definitions := filterDefinitionsByName(t, "fact_records_active_container_image_ci_run_repository_idx")

	// First application, then an identical reapplication: CONCURRENTLY IF NOT
	// EXISTS must be a clean no-op the second time.
	for pass := 1; pass <= 2; pass++ {
		if err := ApplyDefinitions(ctx, SQLDB{DB: db}, definitions); err != nil {
			t.Fatalf("apply fact_records_active_container_image_ci_run_repository_idx pass %d: %v", pass, err)
		}
	}
	assertIndexValidAndReady(ctx, t, db, schema, "fact_records_active_container_image_ci_run_repository_idx", true)

	store := NewFactStore(SQLDB{DB: db})

	loaded, err := store.ListActiveContainerImageCIFacts(ctx, "repository:r_owner")
	if err != nil {
		t.Fatalf("ListActiveContainerImageCIFacts: %v", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("len = %d, want %d (1 owned ci.run + 1 owned ci.artifact): %#v", got, want, loaded)
	}
	for _, envelope := range loaded {
		if envelope.ScopeID == "ci-run-scope:sibling" || strings.Contains(envelope.FactID, "sibling") {
			t.Fatalf("owner-scoped result leaked sibling repository evidence: %#v", envelope)
		}
	}

	// Rollback: DROP INDEX CONCURRENTLY IF EXISTS must remove the index
	// cleanly, and the query must still return correct (if now potentially
	// slower) results without it.
	if _, err := db.ExecContext(ctx, "DROP INDEX CONCURRENTLY IF EXISTS fact_records_active_container_image_ci_run_repository_idx"); err != nil {
		t.Fatalf("rollback fact_records_active_container_image_ci_run_repository_idx: %v", err)
	}
	assertIndexValidAndReady(ctx, t, db, schema, "fact_records_active_container_image_ci_run_repository_idx", false)

	loadedAfterRollback, err := store.ListActiveContainerImageCIFacts(ctx, "repository:r_owner")
	if err != nil {
		t.Fatalf("ListActiveContainerImageCIFacts after rollback: %v", err)
	}
	if got, want := len(loadedAfterRollback), 2; got != want {
		t.Fatalf("post-rollback len = %d, want %d", got, want)
	}
}

func filterDefinitionsByName(t *testing.T, name string) []Definition {
	t.Helper()
	for _, definition := range BootstrapDefinitions() {
		if definition.Name == name {
			return []Definition{definition}
		}
	}
	t.Fatalf("definition %q not found", name)
	return nil
}
