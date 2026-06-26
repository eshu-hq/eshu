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

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const reducerClaimBenchmarkConflictScopeCount = 1_024

func TestReducerClaimBenchmarkDepthsDefaultToIssue2253Targets(t *testing.T) {
	t.Setenv("ESHU_REDUCER_CLAIM_BENCH_DEPTHS", "")

	got := reducerClaimBenchmarkDepths()
	want := []int{100_000, 1_000_000}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reducerClaimBenchmarkDepths() = %v, want %v", got, want)
	}
}

func TestReducerClaimBenchmarkDepthsParseOverrides(t *testing.T) {
	t.Setenv("ESHU_REDUCER_CLAIM_BENCH_DEPTHS", "10, 2000,invalid,0,-5,3000")

	got := reducerClaimBenchmarkDepths()
	want := []int{10, 2_000, 3_000}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reducerClaimBenchmarkDepths() = %v, want %v", got, want)
	}
}

func TestReducerClaimBenchmarkWorkShapeMatchesReducerConflictDerivation(t *testing.T) {
	t.Parallel()

	for _, rowNumber := range []int{1, 2, 1_024, 1_025} {
		shape := reducerClaimBenchmarkWorkShape(rowNumber)
		wantDomain, wantKey := reducerConflictDomainKey(projector.ReducerIntent{
			ScopeID: shape.scopeID,
			Domain:  reducer.DomainWorkloadIdentity,
		})
		if shape.domain != string(reducer.DomainWorkloadIdentity) {
			t.Fatalf("row %d domain = %q, want %q", rowNumber, shape.domain, reducer.DomainWorkloadIdentity)
		}
		if shape.conflictDomain != wantDomain {
			t.Fatalf("row %d conflict domain = %q, want %q", rowNumber, shape.conflictDomain, wantDomain)
		}
		if shape.conflictKey != wantKey {
			t.Fatalf("row %d conflict key = %q, want %q", rowNumber, shape.conflictKey, wantKey)
		}
	}
}

func TestReducerClaimBenchmarkSeedUsesProductionConflictColumns(t *testing.T) {
	t.Parallel()

	recorder := &reducerClaimBenchmarkRecordingExecutor{}
	if err := seedReducerClaimBenchmarkQueue(context.Background(), recorder, 2); err != nil {
		t.Fatalf("seedReducerClaimBenchmarkQueue() error = %v, want nil", err)
	}
	if got, want := len(recorder.calls), 3; got != want {
		t.Fatalf("recorded exec calls = %d, want %d", got, want)
	}

	scopeCall := recorder.calls[0]
	if got, want := scopeCall.args[1], 2; got != want {
		t.Fatalf("scope insert scope-count arg = %v, want %v", got, want)
	}

	workCall := recorder.calls[2]
	if got, want := workCall.args[2], string(reducer.DomainWorkloadIdentity); got != want {
		t.Fatalf("work insert domain arg = %v, want %v", got, want)
	}
	if got, want := workCall.args[3], reducerConflictDomainPlatformGraph; got != want {
		t.Fatalf("work insert conflict-domain arg = %v, want %v", got, want)
	}
	for _, want := range []string{
		// scope_conflict_keys CTE carries the Go-computed domain-partitioned hashed
		// conflict keys (#3672) so the benchmark seed uses production conflict columns.
		"WITH scope_conflict_keys",
		"benchmark_rows AS",
		"((series.i - 1) % $2) + 1 AS scope_ordinal",
		"'scope-bench-' || benchmark_rows.scope_ordinal::text",
		"domain, conflict_domain",
		"conflict_key, status",
	} {
		if !strings.Contains(workCall.query, want) {
			t.Fatalf("work insert query missing %q:\n%s", want, workCall.query)
		}
	}
}

type reducerClaimBenchmarkWorkFixture struct {
	scopeID        string
	generationID   string
	domain         string
	conflictDomain string
	conflictKey    string
}

func reducerClaimBenchmarkWorkShape(rowNumber int) reducerClaimBenchmarkWorkFixture {
	if rowNumber < 1 {
		rowNumber = 1
	}
	scopeOrdinal := ((rowNumber - 1) % reducerClaimBenchmarkConflictScopeCount) + 1
	scopeID := fmt.Sprintf("scope-bench-%d", scopeOrdinal)
	// Platform-graph conflict keys are now domain-partitioned hashed keys (#3672).
	// Use reducerPlatformGraphConflictKey so the benchmark seed stays aligned with
	// production conflict key derivation.
	return reducerClaimBenchmarkWorkFixture{
		scopeID:        scopeID,
		generationID:   fmt.Sprintf("generation-bench-%d", scopeOrdinal),
		domain:         string(reducer.DomainWorkloadIdentity),
		conflictDomain: reducerConflictDomainPlatformGraph,
		conflictKey:    reducerPlatformGraphConflictKey(reducer.DomainWorkloadIdentity, scopeID),
	}
}

func reducerClaimBenchmarkScopeCount(depth int) int {
	if depth < reducerClaimBenchmarkConflictScopeCount {
		return depth
	}
	return reducerClaimBenchmarkConflictScopeCount
}

// BenchmarkReducerQueueClaimDeepQueue measures the reducer claim query against
// deep fact_work_items queues. It is skipped unless a live Postgres DSN is
// provided through ESHU_REDUCER_CLAIM_BENCH_DSN or ESHU_POSTGRES_DSN.
func BenchmarkReducerQueueClaimDeepQueue(b *testing.B) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		b.Skip("set ESHU_REDUCER_CLAIM_BENCH_DSN or ESHU_POSTGRES_DSN to run the reducer claim benchmark")
	}

	for _, depth := range reducerClaimBenchmarkDepths() {
		b.Run(fmt.Sprintf("depth_%d", depth), func(b *testing.B) {
			benchmarkReducerQueueClaimDepth(b, dsn, depth)
		})
	}
}

func reducerClaimBenchmarkDepths() []int {
	const defaultDepths = "100000,1000000"
	raw := strings.TrimSpace(os.Getenv("ESHU_REDUCER_CLAIM_BENCH_DEPTHS"))
	if raw == "" {
		raw = defaultDepths
	}

	parts := strings.Split(raw, ",")
	depths := make([]int, 0, len(parts))
	for _, part := range parts {
		depth, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || depth <= 0 {
			continue
		}
		depths = append(depths, depth)
	}
	if len(depths) == 0 {
		return []int{100_000, 1_000_000}
	}
	return depths
}

func reducerClaimBenchmarkDSN() string {
	if dsn := strings.TrimSpace(os.Getenv("ESHU_REDUCER_CLAIM_BENCH_DSN")); dsn != "" {
		return dsn
	}
	return strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
}

func benchmarkReducerQueueClaimDepth(b *testing.B, dsn string, depth int) {
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

	schemaName := fmt.Sprintf("reducer_claim_bench_%d", time.Now().UnixNano())
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
	if err := seedReducerClaimBenchmarkQueue(ctx, conn, depth); err != nil {
		b.Fatalf("seed benchmark queue depth %d: %v", depth, err)
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		b.Fatalf("analyze benchmark tables depth %d: %v", depth, err)
	}

	now := time.Date(2026, time.June, 13, 12, 0, 0, 0, time.UTC)
	queue := ReducerQueue{
		db:            conn,
		LeaseOwner:    "bench-reducer",
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}

	b.ReportAllocs()
	b.ReportMetric(float64(depth), "queue_depth")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		intent, claimed, err := queue.Claim(ctx)
		if err != nil {
			b.Fatalf("Claim() depth %d: %v", depth, err)
		}
		if !claimed {
			b.Fatalf("Claim() depth %d claimed = false, want true", depth)
		}

		b.StopTimer()
		if err := resetReducerClaimBenchmarkWork(ctx, conn, intent.IntentID, now); err != nil {
			b.Fatalf("reset claimed work %q: %v", intent.IntentID, err)
		}
		b.StartTimer()
	}
}

type reducerClaimBenchmarkConn struct {
	conn *sql.Conn
}

func (c reducerClaimBenchmarkConn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.conn.ExecContext(ctx, query, args...)
}

func (c reducerClaimBenchmarkConn) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return c.conn.QueryContext(ctx, query, args...)
}

type reducerClaimBenchmarkExecCall struct {
	query string
	args  []any
}

type reducerClaimBenchmarkRecordingExecutor struct {
	calls []reducerClaimBenchmarkExecCall
}

func (r *reducerClaimBenchmarkRecordingExecutor) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	r.calls = append(r.calls, reducerClaimBenchmarkExecCall{
		query: query,
		args:  append([]any(nil), args...),
	})
	return reducerClaimBenchmarkResult{}, nil
}

type reducerClaimBenchmarkResult struct{}

func (reducerClaimBenchmarkResult) LastInsertId() (int64, error) { return 0, nil }

func (reducerClaimBenchmarkResult) RowsAffected() (int64, error) { return 1, nil }

func createReducerClaimBenchmarkSchema(ctx context.Context, db Executor, schemaName string) error {
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		return fmt.Errorf("set search_path: %w", err)
	}
	for _, stmt := range []string{
		MigrationSQL("ingestion_scopes"),
		MigrationSQL("scope_generations"),
		MigrationSQL("fact_work_items"),
		graphProjectionPhaseStateSchemaSQL,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply benchmark schema: %w", err)
		}
	}
	return nil
}

func seedReducerClaimBenchmarkQueue(ctx context.Context, db Executor, depth int) error {
	now := time.Date(2026, time.June, 13, 11, 0, 0, 0, time.UTC)
	scopeCount := reducerClaimBenchmarkScopeCount(depth)
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

	// Build Go-side conflict key mapping for each scope ordinal so the benchmark
	// seed uses the same domain-partitioned hashed key that reducerConflictDomainKey
	// produces (#3672). SQL cannot compute the SHA-256 hash, so we pass the mapping
	// as a VALUES list that the work-item insert can join on.
	conflictKeys := make([]string, scopeCount)
	for i := range scopeCount {
		scopeID := fmt.Sprintf("scope-bench-%d", i+1)
		conflictKeys[i] = reducerPlatformGraphConflictKey(reducer.DomainWorkloadIdentity, scopeID)
	}

	// Build a VALUES clause for the conflict-key lookup: (ordinal, key).
	keyValues := make([]string, scopeCount)
	for i, key := range conflictKeys {
		keyValues[i] = fmt.Sprintf("(%d, '%s')", i+1, key)
	}

	if _, err := db.ExecContext(ctx, `
WITH scope_conflict_keys (scope_ordinal, conflict_key) AS (
    VALUES `+strings.Join(keyValues, ", ")+`
),
benchmark_rows AS (
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
    'bench-work-' || benchmark_rows.i::text,
    'scope-bench-' || benchmark_rows.scope_ordinal::text,
    'generation-bench-' || benchmark_rows.scope_ordinal::text,
    'reducer',
    $3,
    $4,
    scope_conflict_keys.conflict_key,
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
        'reason', 'claim-benchmark',
        'fact_id', 'bench-fact-' || benchmark_rows.i::text,
        'source_system', 'bench'
    ),
    $5,
    $5
FROM benchmark_rows
JOIN scope_conflict_keys ON scope_conflict_keys.scope_ordinal = benchmark_rows.scope_ordinal`,
		depth, scopeCount, string(reducer.DomainWorkloadIdentity), reducerConflictDomainPlatformGraph, now); err != nil {
		return fmt.Errorf("insert benchmark work items: %w", err)
	}
	return nil
}

func analyzeReducerClaimBenchmarkTables(ctx context.Context, db Executor) error {
	for _, tableName := range []string{
		"ingestion_scopes",
		"scope_generations",
		"fact_work_items",
		"graph_projection_phase_state",
	} {
		if _, err := db.ExecContext(ctx, "ANALYZE "+tableName); err != nil {
			return fmt.Errorf("analyze %s: %w", tableName, err)
		}
	}
	return nil
}

func resetReducerClaimBenchmarkWork(ctx context.Context, db Executor, workItemID string, now time.Time) error {
	_, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    last_attempt_at = NULL,
    updated_at = $2
WHERE work_item_id = $1`, workItemID, now)
	if err != nil {
		return fmt.Errorf("reset benchmark work item: %w", err)
	}
	return nil
}
