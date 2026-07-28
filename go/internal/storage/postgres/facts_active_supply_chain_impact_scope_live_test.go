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

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedOnlyByDeploymentContextLive
// is the #5466 P0 follow-up: proves the REAL Postgres active-evidence query
// (not a query-text assertion, not a hand-built envelope handed straight to
// the evaluator) actually SELECTS a vulnerability.suppression fact whose
// scope names only environment/workload_id/service_id -- no cve_id,
// advisory_id, package_id, purl, subject_digest, or repository_id. Before
// this fix, such a suppression could never enter the reducer's working set
// in production: FactStore.ListActiveSupplyChainImpactFacts's WHERE clause
// had no predicate that could ever match it, so the operator's suppression
// silently never applied -- wired in the scope struct and matcher, dead on
// the real load path.
func TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedOnlyByDeploymentContextLive(t *testing.T) {
	const schema = "eshu_5466_suppression_scope_prefilter_live"

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

-- The suppression fact lives in its OWN ingestion scope, deliberately
-- separate from wherever the reducer's own vulnerability-intelligence scope
-- would be -- exactly the cross-scope active-evidence expansion this query
-- exists to serve (VEX documents are typically ingested as their own scope,
-- independent of the scanner/SBOM scope that produced the findings).
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES
    ('vex-scope:staging-only', 'vex_document', 'vex', 'staging-only', 'vulnerability_intelligence', 'p0', now(), now(), 'active', 'gen-staging-only'),
    ('vex-scope:prod-noise', 'vex_document', 'vex', 'prod-noise', 'vulnerability_intelligence', 'p0', now(), now(), 'active', 'gen-prod-noise');

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
    ('gen-staging-only', 'vex-scope:staging-only', 'poll', now(), now(), 'active'),
    ('gen-prod-noise', 'vex-scope:prod-noise', 'poll', now(), now(), 'active');

-- SUP-STAGING-ONLY: scoped PURELY by environment ("stage") -- no cve_id,
-- advisory_id, package_id, purl, subject_digest, or repository_id at all.
-- This is the exact shape the #5466 issue's headline scenario names ("not
-- exploitable in staging") when an operator narrows by environment alone.
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES
    ('vuln-suppression:staging-only', 'vex-scope:staging-only', 'gen-staging-only', 'vulnerability.suppression', 'stable-staging-only', 'vex', 'staging-only', now(), now(), FALSE,
     '{"suppression_id":"SUP-STAGING-ONLY","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"environment":"stage"}}'::jsonb),
    ('vuln-suppression:prod-noise', 'vex-scope:prod-noise', 'gen-prod-noise', 'vulnerability.suppression', 'stable-prod-noise', 'vex', 'prod-noise', now(), now(), FALSE,
     '{"suppression_id":"SUP-PROD-NOISE","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"environment":"prod"}}'::jsonb);
`); err != nil {
		t.Fatalf("seed environment-only-scoped suppression fixture: %v", err)
	}

	store := NewFactStore(SQLDB{DB: db})

	// The reducer would have derived Environments:["stage"] from an
	// already-loaded reducer_ci_cd_run_correlation fact naming that
	// environment for the repository under evaluation (supplyChainImpactFilter
	// in go/internal/reducer/supply_chain_impact_active_filter.go); this test
	// exercises FactStore.ListActiveSupplyChainImpactFacts directly with that
	// filter shape to isolate the SQL prefilter itself.
	loaded, err := store.ListActiveSupplyChainImpactFacts(ctx, reducer.SupplyChainImpactFactFilter{
		Environments: []string{"stage"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if got, want := len(loaded), 1; got != want {
		t.Fatalf("len = %d, want %d (the stage-scoped suppression, and only it): %#v", got, want, loaded)
	}
	if loaded[0].FactID != "vuln-suppression:staging-only" {
		t.Fatalf("FactID = %q, want vuln-suppression:staging-only: %#v", loaded[0].FactID, loaded[0])
	}
}
