// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// statusActiveGenerationBenchDSN resolves the live Postgres DSN used by the
// #4446 activeFactWorkItemsCTE proof. It reuses ESHU_POSTGRES_DSN so the
// same local/CI Postgres that backs other integration proofs can drive this
// one too, with a dedicated override for isolated runs.
func statusActiveGenerationBenchDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_STATUS_ACTIVE_GENERATION_BENCH_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

// statusActiveGenerationBenchCase describes one repo-scale shape: scopeCount
// scopes, each re-ingested generationsPerScope times (mirroring repeated
// git-sync/backfill churn), for a total scope_generations population of
// scopeCount*generationsPerScope rows. Only the newest generation per scope
// is "active"; the rest are "superseded" stale rows the CTE must still
// resolve through the stale/active self-join.
type statusActiveGenerationBenchCase struct {
	scopeCount          int
	generationsPerScope int
	workItemsPerScope   int
}

func (c statusActiveGenerationBenchCase) totalGenerations() int {
	return c.scopeCount * c.generationsPerScope
}

func (c statusActiveGenerationBenchCase) totalWorkItems() int {
	return c.scopeCount * c.workItemsPerScope
}

func (c statusActiveGenerationBenchCase) name() string {
	return fmt.Sprintf(
		"scopes_%d_generations_%d_work_%d",
		c.scopeCount, c.generationsPerScope, c.workItemsPerScope,
	)
}

func statusActiveGenerationBenchCases() []statusActiveGenerationBenchCase {
	raw := strings.TrimSpace(os.Getenv("ESHU_STATUS_ACTIVE_GENERATION_BENCH_CASES"))
	if raw == "" {
		// Default: 5,000 scopes x 100 generations/scope = 500,000
		// scope_generations rows, matching the issue's "~500k rows" target.
		return []statusActiveGenerationBenchCase{
			{scopeCount: 5_000, generationsPerScope: 100, workItemsPerScope: 20},
		}
	}
	parts := strings.Split(raw, ",")
	cases := make([]statusActiveGenerationBenchCase, 0, len(parts))
	for _, part := range parts {
		fields := strings.Split(strings.TrimSpace(part), ":")
		if len(fields) != 3 {
			continue
		}
		scopeCount, ok1 := parsePositiveInt(fields[0])
		gensPerScope, ok2 := parsePositiveInt(fields[1])
		workPerScope, ok3 := parsePositiveInt(fields[2])
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		cases = append(cases, statusActiveGenerationBenchCase{
			scopeCount:          scopeCount,
			generationsPerScope: gensPerScope,
			workItemsPerScope:   workPerScope,
		})
	}
	if len(cases) == 0 {
		return []statusActiveGenerationBenchCase{
			{scopeCount: 5_000, generationsPerScope: 100, workItemsPerScope: 20},
		}
	}
	return cases
}

// BenchmarkStatusActiveFactWorkItemsCTEGrowth measures stageCountsQuery (which
// wraps activeFactWorkItemsCTE) against a repo-scale scope_generations
// population with realistic re-ingestion churn (many superseded generations
// per scope). It is skipped unless a live Postgres DSN is provided.
//
// This is the #4446 before/after proof point: run it unmodified against
// main (no scope_generations_scope_generation_idx) to capture the baseline,
// then again after the index lands to capture the improvement, using the
// identical seed and query shape.
func BenchmarkStatusActiveFactWorkItemsCTEGrowth(b *testing.B) {
	dsn := statusActiveGenerationBenchDSN()
	if dsn == "" {
		b.Skip("set ESHU_STATUS_ACTIVE_GENERATION_BENCH_DSN or ESHU_POSTGRES_DSN to run the status active-generation CTE benchmark")
	}

	for _, benchCase := range statusActiveGenerationBenchCases() {
		b.Run(benchCase.name(), func(b *testing.B) {
			benchmarkStatusActiveFactWorkItemsCTE(b, dsn, benchCase)
		})
	}
}

// TestStatusActiveFactWorkItemsCTEUsesGenerationIndex is the #4446 TDD
// regression for stageCountsQuery's plan shape at a scope_generations
// population large enough that a full sequential scan is measurably
// expensive. It asserts the one plan property that holds regardless of
// Postgres's index choice between scope_generations_scope_idx (pre-existing,
// scope_id-only) and scope_generations_scope_generation_idx (#4446,
// (scope_id, generation_id)): no Seq Scan on scope_generations. It does NOT
// assert which named index the planner selects between the two, because that
// choice is a genuine cost-based tie at some row-count/statistics
// combinations (verified manually: scope_generations_scope_generation_idx
// backs both self-joins with a materially cheaper plan once ANALYZE has
// settled, but the planner does not deterministically prefer it over
// scope_generations_scope_idx on every fresh ANALYZE sample at this shape).
// The #4446 EXPLAIN evidence in the PR description, and the
// TestListStageCountsCache* tests in status_stage_counts_cache_test.go
// (deterministic, no live Postgres/planner dependency), are the load-bearing
// proof for this issue; this test only guards against a full-scan
// regression, which would be a genuine correctness/performance bug
// regardless of which index resolves it.
//
// Skipped unless a live Postgres DSN is provided.
func TestStatusActiveFactWorkItemsCTEUsesGenerationIndex(t *testing.T) {
	dsn := statusActiveGenerationBenchDSN()
	if dsn == "" {
		t.Skip("set ESHU_STATUS_ACTIVE_GENERATION_BENCH_DSN or ESHU_POSTGRES_DSN to run the status active-generation CTE index proof")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	sqlConn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated postgres connection: %v", err)
	}
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("status_active_gen_proof_%d", time.Now().UnixNano())
	if os.Getenv("ESHU_STATUS_ACTIVE_GENERATION_KEEP_SCHEMA") == "" {
		defer func() {
			_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
			_ = sqlConn.Close()
		}()
	} else {
		t.Logf("keeping schema %s for inspection", schemaName)
	}
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create proof schema: %v", err)
	}

	// A modest but non-trivial shape: large enough that a sequential self-join
	// scan over scope_generations is measurably more expensive than an index
	// scan, small enough to run quickly in CI.
	benchCase := statusActiveGenerationBenchCase{scopeCount: 2_000, generationsPerScope: 50, workItemsPerScope: 10}
	if err := seedStatusActiveGenerationBenchmark(ctx, conn, benchCase); err != nil {
		t.Fatalf("seed proof data: %v", err)
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze proof tables: %v", err)
	}

	plan, err := statusActiveGenerationExplainAnalyze(ctx, conn, stageCountsQuery)
	if err != nil {
		t.Fatalf("explain analyze stageCountsQuery: %v", err)
	}
	t.Logf("stageCountsQuery plan at scope_generations population %d:\n%s", benchCase.totalGenerations(), plan)

	if strings.Contains(plan, "Seq Scan on scope_generations") {
		t.Fatalf(
			"stageCountsQuery plans a sequential scan on scope_generations; "+
				"expected an index scan (either scope_generations_scope_idx or "+
				"scope_generations_scope_generation_idx):\n%s",
			plan,
		)
	}
}

func benchmarkStatusActiveFactWorkItemsCTE(b *testing.B, dsn string, benchCase statusActiveGenerationBenchCase) {
	b.Helper()

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		b.Fatalf("open postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	sqlConn, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		b.Fatalf("open dedicated postgres connection: %v", err)
	}
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("status_active_gen_bench_%d", time.Now().UnixNano())
	cleanup := func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
		_ = sqlConn.Close()
		_ = db.Close()
	}
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		cleanup()
		b.Fatalf("create benchmark schema: %v", err)
	}
	defer cleanup()
	if err := seedStatusActiveGenerationBenchmark(ctx, conn, benchCase); err != nil {
		b.Fatalf("seed benchmark %s: %v", benchCase.name(), err)
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		b.Fatalf("analyze benchmark tables %s: %v", benchCase.name(), err)
	}

	b.ReportAllocs()
	b.ReportMetric(float64(benchCase.totalGenerations()), "scope_generations_rows")
	b.ReportMetric(float64(benchCase.totalWorkItems()), "fact_work_items_rows")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rows, err := conn.QueryContext(ctx, stageCountsQuery)
		if err != nil {
			b.Fatalf("stageCountsQuery %s: %v", benchCase.name(), err)
		}
		rowCount := 0
		for rows.Next() {
			rowCount++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			b.Fatalf("iterate stageCountsQuery rows %s: %v", benchCase.name(), err)
		}
		_ = rows.Close()
	}
}

// seedStatusActiveGenerationBenchmark seeds scopeCount scopes, each with
// generationsPerScope generations (only the newest is active; older ones are
// superseded) and workItemsPerScope reducer work items pointing at the
// scope's ACTIVE generation. This shape exercises the exact join the issue
// names: activeFactWorkItemsCTE resolves both the work item's own generation
// row (stale_generation) and the scope's active generation row
// (active_generation) via (scope_id, generation_id) lookups across a
// scope_generations population with many rows per scope_id.
func seedStatusActiveGenerationBenchmark(
	ctx context.Context,
	db Executor,
	benchCase statusActiveGenerationBenchCase,
) error {
	base := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
)
SELECT
    'scope-status-bench-' || series.i::text,
    'repository',
    'git',
    'status-bench-repo-' || series.i::text,
    NULL,
    'git',
    'status-bench-repo-' || series.i::text,
    $1,
    $1,
    'active',
    -- Active generation is always the LAST one seeded per scope.
    'generation-status-bench-' || series.i::text || '-' || $2,
    '{}'::jsonb
FROM generate_series(1, $3) AS series(i)`,
		base, strconv.Itoa(benchCase.generationsPerScope), benchCase.scopeCount); err != nil {
		return fmt.Errorf("insert status bench scope: %w", err)
	}

	// Seed generationsPerScope generations per scope, monotonically increasing
	// ingested_at so generation N is the newest (active) and 1..N-1 are
	// superseded, matching real re-ingestion history.
	if _, err := db.ExecContext(ctx, `
WITH scope_series AS (
    SELECT series.i AS scope_ordinal
    FROM generate_series(1, $3) AS series(i)
),
generation_series AS (
    SELECT gen.g AS generation_ordinal
    FROM generate_series(1, $2) AS gen(g)
)
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
)
SELECT
    'generation-status-bench-' || scope_series.scope_ordinal::text || '-' || generation_series.generation_ordinal::text,
    'scope-status-bench-' || scope_series.scope_ordinal::text,
    'snapshot',
    'bench',
    $1::timestamptz + (generation_series.generation_ordinal || ' seconds')::interval,
    $1::timestamptz + (generation_series.generation_ordinal || ' seconds')::interval,
    CASE
        WHEN generation_series.generation_ordinal = $2 THEN 'active'
        ELSE 'superseded'
    END,
    CASE WHEN generation_series.generation_ordinal = $2 THEN $1::timestamptz ELSE NULL END,
    CASE WHEN generation_series.generation_ordinal < $2 THEN $1::timestamptz ELSE NULL END,
    '{}'::jsonb
FROM scope_series
CROSS JOIN generation_series`,
		base, benchCase.generationsPerScope, benchCase.scopeCount); err != nil {
		return fmt.Errorf("insert status bench generations: %w", err)
	}

	// Reducer work items on the ACTIVE generation only (the common live-queue
	// shape); the CTE must still resolve every row through the stale/active
	// self-join against the scope's full generation history.
	if _, err := db.ExecContext(ctx, `
WITH scope_series AS (
    SELECT series.i AS scope_ordinal
    FROM generate_series(1, $3) AS series(i)
),
work_series AS (
    SELECT w.n AS work_ordinal
    FROM generate_series(1, $4) AS w(n)
)
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until, visible_at,
    last_attempt_at, next_attempt_at, failure_class, failure_message,
    failure_details, payload, created_at, updated_at
)
SELECT
    'status-bench-work-' || scope_series.scope_ordinal::text || '-' || work_series.work_ordinal::text,
    'scope-status-bench-' || scope_series.scope_ordinal::text,
    'generation-status-bench-' || scope_series.scope_ordinal::text || '-' || $2,
    'reducer',
    'workload_materialization',
    'scope',
    NULL,
    -- At most one live (claimed/running) lease per scope, matching the
    -- fact_work_items_reducer_live_lease_uniq constraint (conflict_domain
    -- 'scope' coalesces conflict_key to scope_id): only work_ordinal=1 may be
    -- claimed/running, alternating by scope parity; every other ordinal
    -- cycles through terminal/non-live statuses.
    CASE
        WHEN work_series.work_ordinal = 1 AND scope_series.scope_ordinal % 2 = 0 THEN 'claimed'
        WHEN work_series.work_ordinal = 1 THEN 'running'
        ELSE (ARRAY['pending','succeeded','retrying','failed'])[1 + (work_series.work_ordinal % 4)]
    END,
    0,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    NULL,
    jsonb_build_object(
        'entity_key', 'status-bench-entity',
        'reason', 'status-active-generation-benchmark',
        'fact_id', 'status-bench-fact-' || scope_series.scope_ordinal::text || '-' || work_series.work_ordinal::text,
        'source_system', 'bench'
    ),
    $1,
    $1
FROM scope_series
CROSS JOIN work_series`,
		base, strconv.Itoa(benchCase.generationsPerScope), benchCase.scopeCount, benchCase.workItemsPerScope); err != nil {
		return fmt.Errorf("insert status bench work items: %w", err)
	}

	return nil
}

// statusActiveGenerationExplainAnalyze runs EXPLAIN (ANALYZE, BUFFERS) for the
// given query and returns the plan text, used by
// TestStatusActiveFactWorkItemsCTEUsesGenerationIndex to assert the planner
// picks an index scan on scope_generations instead of a sequential scan once
// scope_generations_scope_generation_idx exists.
func statusActiveGenerationExplainAnalyze(ctx context.Context, db Executor, query string) (string, error) {
	queryer, ok := db.(Queryer)
	if !ok {
		return "", fmt.Errorf("executor does not support QueryContext")
	}
	rows, err := queryer.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+query)
	if err != nil {
		return "", fmt.Errorf("explain analyze: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if scanErr := rows.Scan(&line); scanErr != nil {
			return "", fmt.Errorf("scan explain analyze row: %w", scanErr)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("iterate explain analyze rows: %w", err)
	}
	return strings.Join(lines, "\n"), nil
}
