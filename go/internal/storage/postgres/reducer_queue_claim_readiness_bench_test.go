// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerClaimReadinessBenchmarkCasesDefaultToGrowthShape(t *testing.T) {
	t.Setenv("ESHU_REDUCER_CLAIM_READINESS_BENCH_CASES", "")

	got := reducerClaimReadinessBenchmarkCases()
	want := []reducerClaimReadinessBenchmarkCase{
		{queueDepth: 100_000, phaseRows: 100_000, gatedDomainCount: 1},
		{queueDepth: 100_000, phaseRows: 500_000, gatedDomainCount: 4},
		{queueDepth: 1_000_000, phaseRows: 1_000_000, gatedDomainCount: 10},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reducerClaimReadinessBenchmarkCases() = %+v, want %+v", got, want)
	}
}

func TestReducerClaimReadinessBenchmarkCasesParseOverrides(t *testing.T) {
	t.Setenv("ESHU_REDUCER_CLAIM_READINESS_BENCH_CASES", "10:20:1, bad, 30:40:99, 0:10:1, 50:60:2")

	got := reducerClaimReadinessBenchmarkCases()
	want := []reducerClaimReadinessBenchmarkCase{
		{queueDepth: 10, phaseRows: 20, gatedDomainCount: 1},
		{queueDepth: 30, phaseRows: 40, gatedDomainCount: len(reducerClaimReadinessBenchmarkDomains)},
		{queueDepth: 50, phaseRows: 60, gatedDomainCount: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reducerClaimReadinessBenchmarkCases() = %+v, want %+v", got, want)
	}
}

func TestReducerClaimReadinessBenchmarkSeedUsesReadinessGates(t *testing.T) {
	recorder := &reducerClaimBenchmarkRecordingExecutor{}
	benchCase := reducerClaimReadinessBenchmarkCase{
		queueDepth:       20,
		phaseRows:        30,
		gatedDomainCount: 3,
	}
	if err := seedReducerClaimReadinessBenchmark(context.Background(), recorder, benchCase); err != nil {
		t.Fatalf("seedReducerClaimReadinessBenchmark() error = %v, want nil", err)
	}
	if got, want := len(recorder.calls), 4; got != want {
		t.Fatalf("recorded exec calls = %d, want %d", got, want)
	}

	workCall := recorder.calls[2]
	if got, want := workCall.args[2], 3; got != want {
		t.Fatalf("work insert gated-domain-count arg = %v, want %v", got, want)
	}
	for _, want := range []string{
		"WITH benchmark_rows AS",
		"CASE ((benchmark_rows.i - 1) % $3)",
		"aws_relationship_materialization",
		"iam_can_assume_materialization",
		"payload, created_at, updated_at",
	} {
		if !strings.Contains(workCall.query, want) {
			t.Fatalf("work insert query missing %q:\n%s", want, workCall.query)
		}
	}

	phaseCall := recorder.calls[3]
	if got, want := phaseCall.args[1], 30; got != want {
		t.Fatalf("phase insert row-count arg = %v, want %v", got, want)
	}
	for _, want := range []string{
		"INSERT INTO graph_projection_phase_state",
		"'bench-entity'",
		"'cloud_resource_uid'",
		"'canonical_nodes_committed'",
		"'filler-entity-' || benchmark_rows.i::text",
	} {
		if !strings.Contains(phaseCall.query, want) {
			t.Fatalf("phase insert query missing %q:\n%s", want, phaseCall.query)
		}
	}
}

type reducerClaimReadinessBenchmarkCase struct {
	queueDepth       int
	phaseRows        int
	gatedDomainCount int
}

var reducerClaimReadinessBenchmarkDomains = []reducer.Domain{
	reducer.DomainAWSRelationshipMaterialization,
	reducer.DomainIAMCanAssumeMaterialization,
	reducer.DomainS3LogsToMaterialization,
	reducer.DomainRDSPostureMaterialization,
	reducer.DomainIAMInstanceProfileRoleMaterialization,
	reducer.DomainEC2InternetExposureMaterialization,
	reducer.DomainS3InternetExposureMaterialization,
	reducer.DomainIAMCanPerformMaterialization,
	reducer.DomainIAMEscalationMaterialization,
	reducer.DomainWorkloadCloudRelationshipMaterialization,
}

// BenchmarkReducerQueueClaimReadinessGateGrowth measures the current reducer
// claim query as readiness-gated domains and graph_projection_phase_state rows
// grow. It is skipped unless a live Postgres DSN is provided.
func BenchmarkReducerQueueClaimReadinessGateGrowth(b *testing.B) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		b.Skip("set ESHU_REDUCER_CLAIM_BENCH_DSN or ESHU_POSTGRES_DSN to run the reducer claim benchmark")
	}

	for _, benchCase := range reducerClaimReadinessBenchmarkCases() {
		b.Run(benchCase.name(), func(b *testing.B) {
			benchmarkReducerQueueClaimReadinessGate(b, dsn, benchCase)
		})
	}
}

func reducerClaimReadinessBenchmarkCases() []reducerClaimReadinessBenchmarkCase {
	raw := strings.TrimSpace(os.Getenv("ESHU_REDUCER_CLAIM_READINESS_BENCH_CASES"))
	if raw == "" {
		return []reducerClaimReadinessBenchmarkCase{
			{queueDepth: 100_000, phaseRows: 100_000, gatedDomainCount: 1},
			{queueDepth: 100_000, phaseRows: 500_000, gatedDomainCount: 4},
			{queueDepth: 1_000_000, phaseRows: 1_000_000, gatedDomainCount: 10},
		}
	}

	parts := strings.Split(raw, ",")
	cases := make([]reducerClaimReadinessBenchmarkCase, 0, len(parts))
	for _, part := range parts {
		parsed, ok := parseReducerClaimReadinessBenchmarkCase(part)
		if ok {
			cases = append(cases, parsed)
		}
	}
	if len(cases) == 0 {
		return []reducerClaimReadinessBenchmarkCase{{queueDepth: 100_000, phaseRows: 100_000, gatedDomainCount: 1}}
	}
	return cases
}

func parseReducerClaimReadinessBenchmarkCase(raw string) (reducerClaimReadinessBenchmarkCase, bool) {
	fields := strings.Split(strings.TrimSpace(raw), ":")
	if len(fields) != 3 {
		return reducerClaimReadinessBenchmarkCase{}, false
	}
	queueDepth, queueOK := parsePositiveInt(fields[0])
	phaseRows, phaseOK := parsePositiveInt(fields[1])
	gatedDomains, domainsOK := parsePositiveInt(fields[2])
	if !queueOK || !phaseOK || !domainsOK {
		return reducerClaimReadinessBenchmarkCase{}, false
	}
	return reducerClaimReadinessBenchmarkCase{
		queueDepth:       queueDepth,
		phaseRows:        phaseRows,
		gatedDomainCount: clampReducerClaimReadinessDomainCount(gatedDomains),
	}, true
}

func parsePositiveInt(raw string) (int, bool) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	return value, err == nil && value > 0
}

func clampReducerClaimReadinessDomainCount(count int) int {
	if count < 1 {
		return 1
	}
	if count > len(reducerClaimReadinessBenchmarkDomains) {
		return len(reducerClaimReadinessBenchmarkDomains)
	}
	return count
}

func (c reducerClaimReadinessBenchmarkCase) name() string {
	return fmt.Sprintf("queue_%d_phase_%d_domains_%d", c.queueDepth, c.phaseRows, c.gatedDomainCount)
}

func benchmarkReducerQueueClaimReadinessGate(b *testing.B, dsn string, benchCase reducerClaimReadinessBenchmarkCase) {
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

	schemaName := fmt.Sprintf("reducer_claim_readiness_bench_%d", time.Now().UnixNano())
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
	if err := seedReducerClaimReadinessBenchmark(ctx, conn, benchCase); err != nil {
		b.Fatalf("seed readiness benchmark %s: %v", benchCase.name(), err)
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		b.Fatalf("analyze readiness benchmark %s: %v", benchCase.name(), err)
	}

	now := time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		db:            conn,
		LeaseOwner:    "bench-reducer",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	b.ReportAllocs()
	b.ReportMetric(float64(benchCase.queueDepth), "queue_depth")
	b.ReportMetric(float64(benchCase.phaseRows), "phase_rows")
	b.ReportMetric(float64(benchCase.gatedDomainCount), "gated_domains")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		intent, claimed, err := queue.Claim(ctx)
		if err != nil {
			b.Fatalf("Claim() %s: %v", benchCase.name(), err)
		}
		if !claimed {
			b.Fatalf("Claim() %s claimed = false, want true", benchCase.name())
		}

		b.StopTimer()
		if err := resetReducerClaimBenchmarkWork(ctx, conn, intent.IntentID, now); err != nil {
			b.Fatalf("reset claimed work %q: %v", intent.IntentID, err)
		}
		b.StartTimer()
	}
}

func seedReducerClaimReadinessBenchmark(
	ctx context.Context,
	db Executor,
	benchCase reducerClaimReadinessBenchmarkCase,
) error {
	now := time.Date(2026, time.June, 13, 11, 0, 0, 0, time.UTC)
	scopeCount := reducerClaimBenchmarkScopeCount(max(benchCase.queueDepth, benchCase.phaseRows))
	if err := seedReducerClaimBenchmarkScopes(ctx, db, scopeCount, now); err != nil {
		return err
	}
	if err := seedReducerClaimReadinessWork(ctx, db, benchCase, scopeCount, now); err != nil {
		return err
	}
	if err := seedReducerClaimReadinessPhases(ctx, db, benchCase.phaseRows, scopeCount, now); err != nil {
		return err
	}
	return nil
}

func seedReducerClaimBenchmarkScopes(ctx context.Context, db Executor, scopeCount int, now time.Time) error {
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, parent_scope_id,
    collector_kind, partition_key, observed_at, ingested_at, status,
    active_generation_id, payload
)
SELECT
    'scope-bench-' || series.i::text,
    'repository',
    'git',
    'bench-repo-' || series.i::text,
    NULL,
    'git',
    'bench-repo-' || series.i::text,
    $1,
    $1,
    'active',
    'generation-bench-' || series.i::text,
    '{}'::jsonb
FROM generate_series(1, $2) AS series(i)`, now, scopeCount); err != nil {
		return fmt.Errorf("insert benchmark scope: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
    ingested_at, status, activated_at, superseded_at, payload
)
SELECT
    'generation-bench-' || series.i::text,
    'scope-bench-' || series.i::text,
    'snapshot',
    'bench',
    $1,
    $1,
    'active',
    $1,
    NULL,
    '{}'::jsonb
FROM generate_series(1, $2) AS series(i)`, now, scopeCount); err != nil {
		return fmt.Errorf("insert benchmark generation: %w", err)
	}
	return nil
}

func seedReducerClaimReadinessWork(
	ctx context.Context,
	db Executor,
	benchCase reducerClaimReadinessBenchmarkCase,
	scopeCount int,
	now time.Time,
) error {
	query := `
WITH benchmark_rows AS (
    SELECT series.i, ((series.i - 1) % $2) + 1 AS scope_ordinal
    FROM generate_series(1, $1) AS series(i)
)
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
    conflict_key, status, attempt_count, lease_owner, claim_until, visible_at,
    last_attempt_at, next_attempt_at, failure_class, failure_message,
    failure_details, payload, created_at, updated_at
)
SELECT
    'bench-readiness-work-' || benchmark_rows.i::text,
    'scope-bench-' || benchmark_rows.scope_ordinal::text,
    'generation-bench-' || benchmark_rows.scope_ordinal::text,
    'reducer',
    ` + reducerClaimReadinessBenchmarkDomainCaseSQL() + `,
    $4,
    'scope-bench-' || benchmark_rows.scope_ordinal::text,
    'pending',
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
        'entity_key', 'bench-entity',
        'reason', 'claim-readiness-benchmark',
        'fact_id', 'bench-readiness-fact-' || benchmark_rows.i::text,
        'source_system', 'bench'
    ),
    $5,
    $5
FROM benchmark_rows`
	if _, err := db.ExecContext(
		ctx,
		query,
		benchCase.queueDepth,
		scopeCount,
		clampReducerClaimReadinessDomainCount(benchCase.gatedDomainCount),
		reducerConflictDomainPlatformGraph,
		now,
	); err != nil {
		return fmt.Errorf("insert readiness benchmark work items: %w", err)
	}
	return nil
}

func reducerClaimReadinessBenchmarkDomainCaseSQL() string {
	var builder strings.Builder
	builder.WriteString("CASE ((benchmark_rows.i - 1) % $3)")
	for i, domain := range reducerClaimReadinessBenchmarkDomains {
		_, _ = fmt.Fprintf(&builder, " WHEN %d THEN '%s'", i, domain)
	}
	builder.WriteString(" END")
	return builder.String()
}

func seedReducerClaimReadinessPhases(
	ctx context.Context,
	db Executor,
	phaseRows int,
	scopeCount int,
	now time.Time,
) error {
	_, err := db.ExecContext(ctx, `
WITH benchmark_rows AS (
    SELECT series.i, ((series.i - 1) % $2) + 1 AS scope_ordinal
    FROM generate_series(1, $1) AS series(i)
)
INSERT INTO graph_projection_phase_state (
    scope_id, acceptance_unit_id, source_run_id, generation_id,
    keyspace, phase, committed_at, updated_at
)
SELECT
    'scope-bench-' || benchmark_rows.scope_ordinal::text,
    CASE
        WHEN benchmark_rows.i <= $2 THEN 'bench-entity'
        ELSE 'filler-entity-' || benchmark_rows.i::text
    END,
    'generation-bench-' || benchmark_rows.scope_ordinal::text,
    'generation-bench-' || benchmark_rows.scope_ordinal::text,
    'cloud_resource_uid',
    'canonical_nodes_committed',
    $3,
    $3
FROM benchmark_rows`, phaseRows, scopeCount, now)
	if err != nil {
		return fmt.Errorf("insert readiness benchmark phase rows: %w", err)
	}
	return nil
}
