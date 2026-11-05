// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10054 before and after EXPLAIN of the retirement statements at 100,000 scopes on the real schema; a measurement that also needs ESHU_RETIREMENT_EXPLAIN=1, so it skips in the ordinary lane
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// retirementPlanBudgetMS fails a statement above the fenced-section bar's order
// of magnitude. The number that matters is the reported before and after.
const retirementPlanBudgetMS = 500.0

var executionTimePattern = regexp.MustCompile(`Execution Time: ([0-9.]+) ms`)

// seedRetirementPlanCorpus writes 100,000 scopes on the real schema: repository
// scopes, ref scopes, and derived scopes sharing one fairness key. The target
// repository also holds 50 generations and 5,000 derived facts.
func seedRetirementPlanCorpus(t *testing.T, ctx context.Context, database *sql.DB, target string) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
			observed_at, ingested_at, status, payload)
		SELECT 'git-scope:repository:r_'||lpad(to_hex(i),8,'0'), 'repository', 'git', 'k'||i, 'git',
			'repository:r_'||lpad(to_hex(i),8,'0'), NOW(), NOW(), 'active', '{}'::jsonb FROM generate_series(1, 50000) AS i`,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
			observed_at, ingested_at, status, payload)
		SELECT 'git-scope:repository:r_'||lpad(to_hex(i),8,'0')||'@refs/x', 'repository', 'git', 'r'||i, 'git',
			'repository:r_'||lpad(to_hex(i),8,'0'), NOW(), NOW(), 'active', '{}'::jsonb FROM generate_series(1, 25000) AS i`,
		`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
			observed_at, ingested_at, status, payload)
		SELECT 'go-reachability://repository:r_'||lpad(to_hex(i),8,'0'), 'scanner_worker', 'scanner_worker', 'd'||i,
			'scanner_worker', 'scanner_worker:inst:go_reachability', NOW(), NOW(), 'active', '{}'::jsonb
		FROM generate_series(1, 25000) AS i`,
		`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, payload)
		SELECT 'plan-gen-'||i, '` + goReachabilityScopePrefix + target + `', 'snapshot', NOW(), NOW(), 'superseded', '{}'::jsonb
		FROM generate_series(1, 50) AS i`,
		`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
			source_fact_key, observed_at, ingested_at, payload)
		SELECT 'plan-fact-'||i, '` + goReachabilityScopePrefix + target + `', 'plan-gen-'||(1 + i % 50),
			'vulnerability.go_module_evidence', 'k'||i, 'scanner_worker', 'k'||i, NOW(), NOW(), '{}'::jsonb
		FROM generate_series(1, 5000) AS i`,
		`ANALYZE ingestion_scopes`, `ANALYZE scope_generations`, `ANALYZE fact_records`,
	} {
		if _, err := database.ExecContext(ctx, statement); err != nil {
			t.Fatalf("seed plan corpus: %v\n%s", err, statement)
		}
	}
}

// explainRetirementStatement runs EXPLAIN ANALYZE in a transaction it rolls back,
// because the locking statements take real row locks. It returns the plan text.
func explainRetirementStatement(t *testing.T, ctx context.Context, database *sql.DB, statement string, args ...any) string {
	t.Helper()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin explain: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+statement, args...)
	if err != nil {
		t.Fatalf("explain: %v\n%s", err, statement)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("explain scan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain rows: %v", err)
	}
	return plan.String()
}

// TestRetirementDerivedStatementPlansLive reports before and after plans for each
// statement #10054 changed, on the real schema at 100,000 scopes. The "before"
// texts are the shipped statements' previous form, kept here as the baseline.
// Enable with ESHU_RETIREMENT_EXPLAIN=1: the seed costs tens of seconds, so it does
// not run with the ordinary live suite.
func TestRetirementDerivedStatementPlansLive(t *testing.T) {
	if os.Getenv("ESHU_RETIREMENT_EXPLAIN") == "" {
		t.Skip("set ESHU_RETIREMENT_EXPLAIN=1 to run the derived-scope plan proof")
	}
	database, ctx := openIsolatedLiveDB(t, "retire_derived_plan",
		"set ESHU_POSTGRES_DSN to run the derived-scope plan proof")
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	const target = "repository:r_00001000"
	seedRetirementPlanCorpus(t, ctx, database, target)
	repos := []string{target}

	for _, tt := range []struct {
		name      string
		statement string
		args      []any
	}{
		{"fence resolve before", `SELECT scope_id, partition_key FROM ingestion_scopes WHERE partition_key = ANY($1) ORDER BY scope_id`, []any{repos}},
		{"fence resolve after", fenceResolveScopesQuery, []any{repos}},
		{"fence lock before", `SELECT scope_id FROM ingestion_scopes WHERE partition_key = ANY($1) ORDER BY scope_id FOR NO KEY UPDATE`, []any{repos}},
		{"fence lock after", fenceLockScopesQuery, []any{repos}},
		{"precheck generations after", fencePrecheckGenerationsQuery, []any{repos}},
		{"precheck reducer rows after", fencePrecheckReducerRowsQuery, []any{repos}},
		{"fence null pointer after", fenceNullActivePointerQuery, []any{repos}},
		{"purge lock before", `SELECT scope_id FROM ingestion_scopes WHERE partition_key = $1 ORDER BY scope_id FOR NO KEY UPDATE NOWAIT`, []any{target}},
		{"purge lock after", purgeLockScopesQuery, []any{target}},
		{"capture after", retirementCaptureScopesSQL, []any{target}},
		{"derived fact count", purgeDerivedFactCountQuery, []any{[]string{goReachabilityScopePrefix + target}}},
		{"commit gate", derivedParentOpenRetirementQuery, []any{target}},
	} {
		plan := explainRetirementStatement(t, ctx, database, tt.statement, tt.args...)
		t.Logf("=== %s\n%s", tt.name, plan)
		for _, forbidden := range []string{"Seq Scan on fact_records", "Seq Scan on scope_generations"} {
			if strings.Contains(plan, forbidden) {
				t.Errorf("%s plans %q:\n%s", tt.name, forbidden, plan)
			}
		}
		match := executionTimePattern.FindStringSubmatch(plan)
		if match == nil {
			t.Fatalf("%s: no execution time in plan:\n%s", tt.name, plan)
		}
		executionMS, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			t.Fatalf("%s: parse execution time: %v", tt.name, err)
		}
		t.Logf("PLAN_MS %s %.3f", tt.name, executionMS)
		if executionMS > retirementPlanBudgetMS {
			t.Errorf("%s took %.1f ms, over the %.0f ms bar", tt.name, executionMS, retirementPlanBudgetMS)
		}
	}
}
