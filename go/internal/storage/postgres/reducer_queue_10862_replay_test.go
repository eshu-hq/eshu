// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// replay10862Row is one waiting reducer row in the #10862 replay seed:
// the QA waiting-queue mix measured 2026-11-18 (read-only), per domain
// and status. Counts below are the measured per-domain pending/retrying
// split (total 8,713 waiting across the two samples; live drift vs the
// 8,671 total measured minutes earlier is recorded in the evidence note).
type replay10862Row struct {
	domain   reducercontract.Domain
	status   string
	attempts int
}

// replay10862Mix is the measured QA waiting mix. The advisory rows are the
// vulnerability burst the issue requires (advisory_group_projection =
// vulnerability-advisory projection; 982 of its 1,707 rows retrying).
func replay10862Mix() []replay10862Row {
	mix := []struct {
		domain   reducercontract.Domain
		pending  int
		retrying int
	}{
		{reducercontract.DomainSupplyChainImpact, 5845, 46},
		{reducercontract.DomainAdvisoryGroupProjection, 725, 982},
		{reducercontract.DomainWorkloadMaterialization, 248, 54},
		{reducercontract.DomainDeploymentMapping, 216, 0},
		{reducercontract.DomainDeployableUnitCorrelation, 1, 156},
		{reducercontract.DomainContainerImageIdentity, 150, 0},
		{reducercontract.DomainPlatformInfraMaterialization, 125, 0},
		{reducercontract.DomainEshuSearchDocument, 81, 0},
		{reducercontract.DomainCodeImportRepoEdge, 51, 0},
		{reducercontract.DomainAWSCloudRuntimeDrift, 9, 0},
		{reducercontract.DomainRationaleMaterialization, 6, 0},
		{reducercontract.DomainRuntimeDeclarationProjection, 6, 0},
		{reducercontract.DomainSubmodulePin, 6, 0},
		{reducercontract.DomainCodeownersOwnership, 5, 0},
		{reducercontract.DomainSecurityAlertReconciliation, 1, 0},
	}
	var rows []replay10862Row
	for _, m := range mix {
		for i := 0; i < m.pending; i++ {
			rows = append(rows, replay10862Row{domain: m.domain, status: "pending"})
		}
		for i := 0; i < m.retrying; i++ {
			rows = append(rows, replay10862Row{domain: m.domain, status: "retrying", attempts: 1})
		}
	}
	return rows
}

// seedReducerQueue10862Replay plants the #10862 waiting mix on a disposable
// Postgres behind the benchmark schema. createdAt spread reproduces the
// measured queue age (oldest ~30d) deterministically: row i of n lands at
// oldest + i*span/n, so the replay carries both the old tail and fresh rows.
func seedReducerQueue10862Replay(t *testing.T, ctx context.Context, database db.Executor, schema string, now time.Time) int {
	t.Helper()
	rows := replay10862Mix()
	const scopeCount = 600
	oldest := time.Date(2026, time.October, 19, 5, 21, 48, 0, time.UTC)
	if _, err := database.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatalf("set replay search_path: %v", err)
	}
	scopeValue := make([]string, 0, scopeCount)
	for i := 1; i <= scopeCount; i++ {
		scopeValue = append(scopeValue, fmt.Sprintf(
			"('scope-replay-%d','repository','git','replay-repo-%d',NULL,'git','replay-repo-%d','active','generation-replay-%d','{}'::jsonb,'%s','%s')",
			i, i, i, i,
			oldest.Format(time.RFC3339), oldest.Format(time.RFC3339)))
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO ingestion_scopes (
scope_id, scope_kind, source_system, source_key, parent_scope_id,
collector_kind, partition_key, status, active_generation_id, payload,
observed_at, ingested_at) VALUES `+strings.Join(scopeValue, ",")); err != nil {
		t.Fatalf("insert replay scopes: %v", err)
	}
	genValue := make([]string, 0, scopeCount)
	for i := 1; i <= scopeCount; i++ {
		genValue = append(genValue, fmt.Sprintf(
			"('generation-replay-%d','scope-replay-%d','snapshot','replay','%s','%s','active','%s',NULL,'{}'::jsonb)",
			i, i,
			oldest.Format(time.RFC3339), oldest.Format(time.RFC3339), oldest.Format(time.RFC3339)))
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO scope_generations (
generation_id, scope_id, trigger_kind, freshness_hint, observed_at,
ingested_at, status, activated_at, superseded_at, payload) VALUES `+strings.Join(genValue, ",")); err != nil {
		t.Fatalf("insert replay generations: %v", err)
	}
	span := now.Sub(oldest)
	n := len(rows)
	for start := 0; start < n; start += 500 {
		end := start + 500
		if end > n {
			end = n
		}
		values := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			row := rows[i]
			created := oldest.Add(time.Duration(float64(i) / float64(n) * float64(span)))
			scopeOrdinal := (i % scopeCount) + 1
			visible := "NULL"
			nextAttempt := "NULL"
			failureClass := "NULL"
			if row.status == "retrying" {
				visible = "'" + created.Format(time.RFC3339) + "'"
				nextAttempt = "'" + created.Format(time.RFC3339) + "'"
				failureClass = "'reducer_retryable'"
			}
			ts := created.Format(time.RFC3339)
			values = append(values, fmt.Sprintf(
				"('replay-work-%d','scope-replay-%d','generation-replay-%d','reducer','%s','scope',NULL,'%s',%d,NULL,NULL,%s,NULL,%s,%s,NULL,NULL,"+
					"jsonb_build_object('entity_key','replay-entity','reason','10862-replay','fact_id','replay-fact-%d','source_system','replay'),'%s','%s')",
				i, scopeOrdinal, scopeOrdinal, string(row.domain), row.status, row.attempts,
				visible, nextAttempt, failureClass, i, ts, ts))

		}
		if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items (
work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
conflict_key, status, attempt_count, lease_owner, claim_until, visible_at,
last_attempt_at, next_attempt_at, failure_class, failure_message,
failure_details, payload, created_at, updated_at) VALUES `+strings.Join(values, ",")); err != nil {
			t.Fatalf("insert replay work items [%d:%d]: %v", start, end, err)
		}
	}
	return n
}

func percentileSortedDesc(samples []float64, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	idx := int(p * float64(len(sorted)-1))
	return sorted[idx]
}

// TestReducerQueue10862ReplayShapeAndDrain replays the #10862 QA waiting
// mix on a disposable Postgres and drains it through the shipped single
// Claim/Ack path. It asserts the replay reproduces the baseline shape
// (counts exact, age tail present, burst present) and that hand-out cost
// stays flat as the queue drains: second-half claim p95 within 1.10x of
// first-half p95, the same budget the claim-latency gate sets for
// same-shape comparisons.
//
// Operator-gated like BenchmarkReducerQueueClaimDeepQueue: it needs
// ESHU_REDUCER_CLAIM_BENCH_DSN (or ESHU_POSTGRES_DSN) and never touches QA.
// Batch-claim coverage stays NOT_CHECKED: the drain loop exercises the
// single-claim SQL, which ranks the same queue the batch path ranks.
func TestReducerQueue10862ReplayShapeAndDrain(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_CLAIM_BENCH_DSN or ESHU_POSTGRES_DSN to run the 10862 replay")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()
	sqlConn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("open dedicated postgres connection: %v", err)
	}
	defer func() { _ = sqlConn.Close() }()
	conn := reducerClaimBenchmarkConn{conn: sqlConn}

	schemaName := fmt.Sprintf("reducer_claim_10862_%d", time.Now().UnixNano())
	cleanup := func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	}
	defer cleanup()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create replay schema: %v", err)
	}
	// Container-image and value-flow-producer ACKs emit completion events,
	// and the workload ACK re-trigger join reads fact_records.
	if _, err := conn.ExecContext(ctx, MigrationSQL("fact_records")); err != nil {
		t.Fatalf("create replay fact_records: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("cross_scope_completion_queue")); err != nil {
		t.Fatalf("create completion events table: %v", err)
	}

	// Real clock: the Ack lease fence compares claim_until against
	// clock_timestamp(), so a fixed Now would expire every lease once the
	// wall clock passes it. The age tail is anchored on the QA-measured
	// oldest row, which stays >= 29 d behind any run in this window.
	now := time.Now().UTC()
	want := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now)
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze replay tables: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set replay search_path: %v", err)
	}

	var waiting, retrying, advisory int
	var oldest, newest time.Time
	if err := sqlConn.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE status IN ('pending','retrying')),
count(*) FILTER (WHERE status='retrying'),
count(*) FILTER (WHERE domain='advisory_group_projection'),
min(created_at), max(created_at) FROM fact_work_items WHERE stage='reducer'`,
	).Scan(&waiting, &retrying, &advisory, &oldest, &newest); err != nil {
		t.Fatalf("read replay shape: %v", err)
	}
	if waiting != want {
		t.Fatalf("replay waiting = %d, want %d (baseline mix)", waiting, want)
	}
	if waiting < 8000 {
		t.Fatalf("replay waiting = %d, want >= 8000 (issue acceptance)", waiting)
	}
	if retrying != 1238 {
		t.Fatalf("replay retrying = %d, want 1238 (baseline mix)", retrying)
	}
	if advisory != 1707 {
		t.Fatalf("replay advisory burst = %d, want 1707", advisory)
	}
	if now.Sub(oldest) < 29*24*time.Hour {
		t.Fatalf("replay oldest = %v, want tail >= 29d (baseline 29.8d)", oldest)
	}
	if now.Sub(newest) > 24*time.Hour {
		t.Fatalf("replay newest = %v, want fresh rows within 24h", newest)
	}

	queue := ReducerQueue{
		database:      conn,
		LeaseOwner:    "replay-10862",
		LeaseDuration: time.Minute,
	}
	var latencies []float64
	drained := 0
	start := time.Now()
	for i := 0; i < want+100; i++ {
		claimStart := time.Now()
		intent, claimed, err := queue.Claim(ctx)
		latencies = append(latencies, time.Since(claimStart).Seconds())
		if err != nil {
			t.Fatalf("replay claim %d: %v", i, err)
		}
		if !claimed {
			break
		}
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			t.Fatalf("replay ack %s: %v", intent.IntentID, err)
		}
		drained++
	}
	wall := time.Since(start).Seconds()
	if drained != want {
		t.Fatalf("replay drained = %d, want %d (claims stalled with work left)", drained, want)
	}
	half := len(latencies) / 2
	p95First := percentileSortedDesc(latencies[:half], 0.95)
	p95Second := percentileSortedDesc(latencies[half:], 0.95)
	t.Logf("10862 replay: depth=%d drained=%d wall=%.1fs items_per_sec=%.1f claim_p50=%.3fs claim_p95=%.3fs claim_max=%.3fs flatness_p95_second_over_first=%.3f",
		want, drained, wall, float64(drained)/wall,
		percentileSortedDesc(latencies, 0.50), percentileSortedDesc(latencies, 0.95),
		percentileSortedDesc(latencies, 1.0), p95Second/p95First)
	if p95First > 0 && p95Second > 1.10*p95First {
		t.Fatalf("hand-out not flat: second-half p95 %.3fs > 1.10x first-half p95 %.3fs",
			p95Second, p95First)
	}
}
