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

// newSupplyChainImpactScopeLiveTestDB creates an isolated schema on the
// ESHU_POSTGRES_TEST_DSN instance and the three tables
// FactStore.ListActiveSupplyChainImpactFacts reads, registering cleanup that
// drops the schema. It skips the test when no DSN is configured. Shared by
// every #5466 live suppression-scope-prefilter proof in this file so each
// test only has to seed its own fact rows.
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
	db := newSupplyChainImpactScopeLiveTestDB(t, "eshu_5466_suppression_scope_prefilter_live")

	// The suppression fact lives in its OWN ingestion scope, deliberately
	// separate from wherever the reducer's own vulnerability-intelligence
	// scope would be -- exactly the cross-scope active-evidence expansion
	// this query exists to serve (VEX documents are typically ingested as
	// their own scope, independent of the scanner/SBOM scope that produced
	// the findings).
	//
	// SUP-STAGING-ONLY: scoped PURELY by environment ("stage") -- no
	// cve_id, advisory_id, package_id, purl, subject_digest, or
	// repository_id at all. This is the exact shape the #5466 issue's
	// headline scenario names ("not exploitable in staging") when an
	// operator narrows by environment alone.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:staging-only", "staging-only",
		`{"suppression_id":"SUP-STAGING-ONLY","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"environment":"stage"}}`)
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:prod-noise", "prod-noise",
		`{"suppression_id":"SUP-PROD-NOISE","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"environment":"prod"}}`)

	store := NewFactStore(SQLDB{DB: db})

	// The reducer would have derived Environments:["stage"] from an
	// already-loaded reducer_ci_cd_run_correlation fact naming that
	// environment for the repository under evaluation (supplyChainImpactFilter
	// in go/internal/reducer/supply_chain_impact_active_filter.go); this test
	// exercises FactStore.ListActiveSupplyChainImpactFacts directly with that
	// filter shape to isolate the SQL prefilter itself.
	loaded, _, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
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

// TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByEnvironmentAliasLive
// is the #5466 P1-1 fix proof: a suppression payload authored with the alias
// spelling "production" (the literal shape
// TestBuildVulnerabilitySuppressionsDecodesEnvironmentWorkloadServiceScope
// already proves the decode seam canonicalizes to "prod") must still be
// SELECTED by the real Postgres prefilter when the reducer's derived filter
// carries the canonical form "prod". Before the fix, the SQL predicate did
// an exact-match comparison against the raw, uncanonicalized payload value,
// so "production" in the payload could never match "prod" in $14 -- the
// suppression was silently inert in production even though it decodes and
// matches correctly once loaded. This test must fail against the
// pre-#5466-P1-1 SQL and pass after the lower()+alias-expansion fix.
//
// It also covers the round-2 review F-1 finding: environment.Canonical is
// TrimSpace + ToLower + alias, so a payload of {"environment":" Production "}
// (leading/trailing whitespace AND alias casing) must decode to "prod" and
// be selected too -- the SUP-ALIAS-WHITESPACE fact below pins that the SQL
// predicate trims.
//
// It also covers the round-2 review F-4 finding: Postgres trim()/btrim()
// with a literal character class only strips the characters named in that
// class, not the full Unicode White_Space property Go's strings.TrimSpace
// strips. SUP-ALIAS-TAB below is padded with an actual tab and newline
// (via the JSON \t/\n escapes, which the Postgres jsonb parser resolves to
// real control characters, not the two-character "\t" text) -- proven live
// on Postgres 16 that lower(trim('...')) (space-only trim) fails to select
// this payload while lower(btrim('...', E' \t\n\v\f\r')) (the shipped
// ASCII-whitespace-class fix) succeeds.
func TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByEnvironmentAliasLive(t *testing.T) {
	db := newSupplyChainImpactScopeLiveTestDB(t, "eshu_5466_suppression_scope_alias_live")

	// SUP-ALIAS-PROD: payload names the ALIAS form "production", never the
	// canonical "prod" -- the exact literal shape used by
	// TestBuildVulnerabilitySuppressionsDecodesEnvironmentWorkloadServiceScope.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:alias-prod", "alias-prod",
		`{"suppression_id":"SUP-ALIAS-PROD","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"environment":"production"}}`)
	// SUP-ALIAS-WHITESPACE: payload names the alias form WITH leading and
	// trailing whitespace, " Production " -- environment.Canonical trims and
	// lowercases before alias lookup, so this must decode and match
	// identically to SUP-ALIAS-PROD above (#5466 round-2 review F-1).
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:alias-whitespace", "alias-whitespace",
		`{"suppression_id":"SUP-ALIAS-WHITESPACE","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"environment":" Production "}}`)
	// SUP-ALIAS-TAB: payload names the alias form padded with a tab and a
	// newline instead of plain ASCII spaces (#5466 round-2 review F-4) --
	// the JSON \t/\n escapes decode to real tab/newline bytes once the
	// jsonb column parses this literal.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:alias-tab", "alias-tab",
		"{\"suppression_id\":\"SUP-ALIAS-TAB\",\"source\":\"eshu_policy\",\"justification\":\"not_affected\",\"author\":\"security-bot\",\"authored_at\":\"2026-06-20T00:00:00Z\",\"scope\":{\"environment\":\"\\tProduction\\n\"}}")
	// SUP-ALIAS-NOISE: a different environment entirely, must never match a
	// ["prod"] filter regardless of alias expansion.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:alias-noise", "alias-noise",
		`{"suppression_id":"SUP-ALIAS-NOISE","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"environment":"qa"}}`)

	store := NewFactStore(SQLDB{DB: db})

	// The reducer derives Environments:["prod"] (canonical form) from
	// already-loaded deployment evidence, per environment.Canonical
	// (go/internal/environment). The literal "production" payload, the
	// space-padded " Production " payload, and the tab/newline-padded
	// payload above must all still be selected; SUP-ALIAS-NOISE must not.
	loaded, _, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		Environments: []string{"prod"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if got, want := len(loaded), 3; got != want {
		t.Fatalf("len = %d, want %d (the alias-\"production\", space-padded, and tab/newline-padded suppressions, and only those): %#v", got, want, loaded)
	}
	gotIDs := map[string]bool{}
	for _, envelope := range loaded {
		gotIDs[envelope.FactID] = true
	}
	for _, wantID := range []string{"vuln-suppression:alias-prod", "vuln-suppression:alias-whitespace", "vuln-suppression:alias-tab"} {
		if !gotIDs[wantID] {
			t.Fatalf("FactID %q missing from loaded set: %#v", wantID, loaded)
		}
	}
}

// TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByWorkloadAndServiceIDsLive
// is the #5466 P2-1 fix proof for $12 (WorkloadIDs) and $13 (ServiceIDs),
// which the environment-only live test above never exercises. It seeds two
// DISTINCT suppression facts -- one scoped only by workload_id, one scoped
// only by service_id -- and queries with both filter fields populated at
// once. This catches two separate real bugs: an exact-match predicate (the
// case/whitespace payload shape here would fail an exact-match comparison,
// same class as the environment fix) and a bind-order swap of
// filter.WorkloadIDs/filter.ServiceIDs in listActiveSupplyChainImpactFactsPage
// (swapping them would make the workload fact only reachable via the
// service filter value and vice versa, so both would silently disappear).
func TestListActiveSupplyChainImpactFactsLoadsSuppressionScopedByWorkloadAndServiceIDsLive(t *testing.T) {
	db := newSupplyChainImpactScopeLiveTestDB(t, "eshu_5466_suppression_scope_workload_service_live")

	// SUP-WORKLOAD-ONLY: payload workload_id has different case/whitespace
	// than the filter value below, proving lower(trim(...)) rather than an
	// exact-match comparison.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:workload-only", "workload-only",
		`{"suppression_id":"SUP-WORKLOAD-ONLY","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"workload_id":" Workload:X "}}`)
	// SUP-SERVICE-ONLY: same shape for service_id.
	seedSupplyChainImpactScopeLiveFact(t, db, "vuln-suppression:service-only", "service-only",
		`{"suppression_id":"SUP-SERVICE-ONLY","source":"eshu_policy","justification":"not_affected","author":"security-bot","authored_at":"2026-06-20T00:00:00Z","scope":{"service_id":"Service:Y"}}`)

	store := NewFactStore(SQLDB{DB: db})

	loaded, _, err := store.ListActiveSupplyChainImpactFacts(context.Background(), reducer.SupplyChainImpactFactFilter{
		WorkloadIDs: []string{"workload:x"},
		ServiceIDs:  []string{"service:y"},
	})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if got, want := len(loaded), 2; got != want {
		t.Fatalf("len = %d, want %d (the workload-only and service-only suppressions): %#v", got, want, loaded)
	}
	gotIDs := map[string]bool{}
	for _, envelope := range loaded {
		gotIDs[envelope.FactID] = true
	}
	for _, wantID := range []string{"vuln-suppression:workload-only", "vuln-suppression:service-only"} {
		if !gotIDs[wantID] {
			t.Fatalf("FactID %q missing from loaded set: %#v", wantID, loaded)
		}
	}
}
