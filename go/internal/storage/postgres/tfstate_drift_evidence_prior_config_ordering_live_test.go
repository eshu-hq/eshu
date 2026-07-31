// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres
// is the permanent, re-runnable real-Postgres proof for the P2 finding in
// docs/internal/evidence/5572-drift-derived-outcome-module-resolution-confidence.md's
// "Follow-up: two independent review findings" section. The throwaway
// EXPLAIN run pasted there was disclosed honestly as prose, not committed
// evidence — this test IS the committed evidence it points at.
//
// TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending
// (tfstate_drift_evidence_prior_config_ordering_test.go) is a cheap,
// credential-free substring assertion on the SQL constant text. It cannot
// catch a syntactically different ORDER BY that still contains the
// substring but produces the wrong order, and it cannot catch a
// planner-level regression at all — only a real Postgres planner can prove
// or disprove a row-ordering claim. This test complements it; it does not
// replace it.
//
// Seeds THREE prior generations whose generation_id lexical order
// (gen-alpha, gen-charlie, gen-omega) is deliberately scrambled relative to
// their true ingested_at order (gen-charlie newest, gen-alpha middle,
// gen-omega oldest) — the same shape used in the throwaway proof, because
// that scramble is exactly what distinguishes "ORDER BY generation_id ASC"
// from "ORDER BY ingested_at DESC": gen-charlie (the most recent) carries a
// registry-shorthand module-resolution failure
// (ModuleResolutionReason="external_registry"); the other two resolve
// cleanly. Only the correctly-ordered query lets first-write-wins pick
// gen-charlie's flagged confidence as the winner for "aws_instance.web";
// the pre-fix ordering lets an OLDER, clean generation win first and
// silently discards the more recent, actionable signal.
//
// Run with: ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu
//
//	go test ./internal/storage/postgres -run 'PrefersMostRecentGenerationAgainstRealPostgres' -count=1
func TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres(t *testing.T) {
	dsn := priorConfigOrderingLiveProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the real-Postgres prior-config ordering proof")
	}

	ctx := context.Background()
	db := openPriorConfigOrderingLiveSchema(t, ctx, dsn)
	seedPriorConfigOrderingLiveFixture(t, ctx, db)

	loader := PostgresDriftEvidenceLoader{DB: SQLDB{DB: db}}
	out, err := loader.loadPriorConfigAddresses(ctx, "repository:repo-a", "gen-current", modulePrefixMap{}, nil)
	if err != nil {
		t.Fatalf("loadPriorConfigAddresses() error = %v, want nil", err)
	}

	got, ok := out["aws_instance.web"]
	if !ok {
		t.Fatalf(`out["aws_instance.web"] missing entirely; want present with the most-recent generation's confidence`)
	}
	if got != "external_registry" {
		t.Fatalf(
			`out["aws_instance.web"] = %q, want "external_registry" — `+
				`the most recently ingested prior generation (gen-charlie) carries the flagged confidence; `+
				`getting anything else (typically "" from an older, clean generation winning first) means `+
				`listPriorConfigAddressesQuery's row order is not genuinely most-recent-first`,
			got,
		)
	}

	// Optional, cheap plan-shape corroboration (mirrors
	// latest_generation_cte_integration_test.go's SubPlan assertion, the
	// established fixture-plan pattern in this package): the prior_generations
	// CTE must still be inlined, not materialized as a separate CTE Scan node.
	// This is NOT an ordering proof by itself (both the broken and fixed query
	// text inline identically on Postgres 18+) — it only guards against a
	// future edit that accidentally forces materialization (e.g. an added
	// MATERIALIZED keyword or a second CTE reference), which would change the
	// query's cost profile without changing its correctness.
	plan := explainQueryPlanWithArgs(t, ctx, db, listPriorConfigAddressesQuery,
		"repository:repo-a", "gen-current", 10)
	if strings.Contains(plan, "CTE Scan") {
		t.Fatalf("listPriorConfigAddressesQuery plan unexpectedly contains a CTE Scan node (prior_generations is no longer inlined):\n%s", plan)
	}
}

func priorConfigOrderingLiveProofDSN() string {
	return os.Getenv("ESHU_POSTGRES_DSN")
}

// openPriorConfigOrderingLiveSchema creates an isolated throwaway schema and
// applies exactly the three migrations listPriorConfigAddressesQuery depends
// on, mirroring openStaticGrantPolicyHashLiveSchema and
// openProviderConfigLiveSchema — this package's established live-Postgres
// proof pattern.
//
// The handle is capped at one connection (SetMaxOpenConns(1) /
// SetMaxIdleConns(1)), matching TestLatestGenerationCTETruthEquivalenceAndPlan's
// own db.SetMaxOpenConns(1) call: SET search_path is connection-local, and a
// *sql.DB is a pool that can silently hand a later query a DIFFERENT
// connection than the one search_path was set on — capping at one connection
// is what pins every subsequent query to the schema this test created.
func openPriorConfigOrderingLiveSchema(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	schemaName := fmt.Sprintf("prior_config_ordering_live_%d", time.Now().UnixNano())
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatalf("create prior config ordering live schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	})
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	for _, defName := range []string{"ingestion_scopes", "scope_generations", "fact_records"} {
		if _, err := db.ExecContext(ctx, MigrationSQL(defName)); err != nil {
			t.Fatalf("apply migration %q: %v", defName, err)
		}
	}
	return db
}

// seedPriorConfigOrderingLiveFixture seeds one repository scope, a current
// generation (excluded from the prior-config walk), and three prior
// generations whose generation_id lexical order (gen-alpha, gen-charlie,
// gen-omega) is deliberately scrambled relative to their true ingested_at
// order (gen-charlie newest, gen-alpha middle, gen-omega oldest) -- see the
// test's own doc comment for why this scramble is load-bearing.
//
// gen-charlie (the most recent) declares "aws_instance.web" under a
// directory a real terraform_modules call misclassifies as external
// registry shorthand (the same fixture shape
// TestLoadDriftEvidenceMarksLowConfidenceForRegistryHeuristicMisclassifiedLocalModule
// uses), so buildModulePrefixMap flags it ModuleResolutionReason
// "external_registry". gen-alpha and gen-omega declare the SAME address at
// a clean root path with no module call at all, so their confidence is "".
func seedPriorConfigOrderingLiveFixture(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	now := time.Now().UTC()

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repository', 'git', 'repo-a', 'git', 'repo-a', $2, $2, 'active', 'gen-current')`,
		"repository:repo-a", now); err != nil {
		t.Fatalf("seed ingestion_scopes: %v", err)
	}

	generations := []struct {
		id         string
		ingestedAt time.Time
		status     string
	}{
		{"gen-current", now, "active"},
		// Lexical (ASC) order: gen-alpha, gen-charlie, gen-omega.
		// True recency (ingested_at DESC): gen-charlie, gen-alpha, gen-omega.
		{"gen-omega", now.Add(-3 * 24 * time.Hour), "superseded"},   // oldest
		{"gen-alpha", now.Add(-2 * 24 * time.Hour), "superseded"},   // middle
		{"gen-charlie", now.Add(-1 * 24 * time.Hour), "superseded"}, // most recent prior
	}
	for _, g := range generations {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, 'repository:repo-a', 'sync', $2, $2, $3)`,
			g.id, g.ingestedAt, g.status); err != nil {
			t.Fatalf("seed scope_generations %q: %v", g.id, err)
		}
	}

	insertFileFact := func(factID, generationID, payload string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, 'repository:repo-a', $2, 'file', $3, 'git', $3, $4, $4, $5::jsonb)`,
			factID, generationID, "file:"+factID, now, payload); err != nil {
			t.Fatalf("seed fact_records %q: %v", factID, err)
		}
	}

	cleanResourcesPayload := `{"parsed_file_data":{"terraform_resources":[
		{"resource_type":"aws_instance","resource_name":"web","path":"main.tf"}
	]}}`
	insertFileFact("fact-omega-resources", "gen-omega", cleanResourcesPayload)
	insertFileFact("fact-alpha-resources", "gen-alpha", cleanResourcesPayload)

	// gen-charlie: the resource lives under the exact directory the
	// registry-shorthand misclassification fixture targets.
	flaggedResourcesPayload := `{"parsed_file_data":{"terraform_resources":[
		{"resource_type":"aws_instance","resource_name":"web","path":"terraform-aws-modules/vpc/aws/main.tf"}
	]}}`
	insertFileFact("fact-charlie-resources", "gen-charlie", flaggedResourcesPayload)

	flaggedModulesPayload := `{"parsed_file_data":{"terraform_modules":[
		{"name":"vpc","source":"terraform-aws-modules/vpc/aws","path":"main.tf","lang":"hcl","line_number":1}
	]}}`
	insertFileFact("fact-charlie-modules", "gen-charlie", flaggedModulesPayload)
}

// explainQueryPlanWithArgs runs `EXPLAIN <query>` with bound parameters and
// returns the plan text joined by newlines. Companion to
// latest_generation_cte_integration_test.go's explainText, which only
// supports argument-free queries; listPriorConfigAddressesQuery is
// parameterized ($1/$2/$3), so this variant threads args through to
// QueryContext the same way any other parameterized statement would.
func explainQueryPlanWithArgs(t *testing.T, ctx context.Context, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.QueryContext(ctx, "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("EXPLAIN query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate EXPLAIN rows: %v", err)
	}
	return strings.Join(lines, "\n")
}
