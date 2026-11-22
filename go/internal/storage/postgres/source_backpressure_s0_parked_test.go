// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// s0InsertOtherSourceWaitRows fills reducer_waiting_items with limit rows that
// belong to sources other than the probed one: each other-source reducer row
// gets one row per wait kind, so 200,000 wait rows need only ~30k queue rows.
// Wait rows on other sources make the table big without touching the probed
// source's rows, which is the "wait-table size" axis of the R4-3 cost proof.
func s0InsertOtherSourceWaitRows(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time, probed string, limit int) {
	t.Helper()
	if _, err := conn.ExecContext(ctx, "TRUNCATE reducer_waiting_items"); err != nil {
		t.Fatalf("truncate wait rows: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO reducer_waiting_items (
work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id, domain,
failure_class, first_deferred_at, updated_at)
SELECT w.work_item_id, k.kind, jsonb_build_object('k', w.work_item_id), w.scope_id, w.generation_id, w.domain,
       'readiness_wait', $2, $2
FROM fact_work_items AS w
CROSS JOIN (VALUES ('phase_committed'), ('generation_active'), ('fence_clear'), ('target_committed'),
                   ('winners_watermark'), ('producer_domain_terminal'), ('relationship_generation_active')) AS k(kind)
WHERE w.stage = 'reducer' AND COALESCE(w.payload->>'source_system', '') <> $3
LIMIT $1`, limit, now, probed); err != nil {
		t.Fatalf("insert other-source wait rows: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "ANALYZE reducer_waiting_items"); err != nil {
		t.Fatalf("analyze wait rows: %v", err)
	}
}

// s0RetryAndParkProbedRows turns the first k rows of the probed source into
// visible retrying rows that each hold a wait row, the K of the cost proof.
// A negative k means every row of the source.
func s0RetryAndParkProbedRows(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time, probed string, k int) int {
	t.Helper()
	// Start from a known state: no probed row is retrying, then exactly k are.
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET status = 'pending', visible_at = NULL, claim_until = NULL, lease_owner = NULL
WHERE stage = 'reducer' AND payload->>'source_system' = $1 AND status <> 'pending'`, probed); err != nil {
		t.Fatalf("reset probed rows: %v", err)
	}
	limit := "ALL"
	args := []any{probed}
	if k >= 0 {
		limit = "$2"
		args = append(args, k)
	}
	res, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET status = 'retrying'
WHERE work_item_id IN (SELECT work_item_id FROM fact_work_items
  WHERE stage = 'reducer' AND payload->>'source_system' = $1 ORDER BY work_item_id LIMIT `+limit+`)`, args...)
	if err != nil {
		t.Fatalf("retry probed rows: %v", err)
	}
	n, _ := res.RowsAffected()
	if _, err := conn.ExecContext(ctx, `INSERT INTO reducer_waiting_items (
work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id, domain,
failure_class, first_deferred_at, updated_at)
SELECT work_item_id, 'phase_committed', jsonb_build_object('own', work_item_id), scope_id, generation_id, domain,
       'readiness_wait', $2, $2
FROM fact_work_items
WHERE stage = 'reducer' AND payload->>'source_system' = $1 AND status = 'retrying'`, probed, now); err != nil {
		t.Fatalf("park probed rows: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "ANALYZE fact_work_items"); err != nil {
		t.Fatalf("analyze fact_work_items: %v", err)
	}
	return int(n)
}

// runS0ParkedCells is the populated-wait-table part of the S0 cost proof
// (#11187 R4-3 ruling, section 3). It runs after the empty-table assertion, on
// the same 10x seed:
//
//   - asserted cell: 50,000 wait rows on other sources and 1,000 visible retrying
//     rows on the probed source that each hold a wait row; the bounded probe's
//     p50 stays within the same 100 ms budget;
//   - logged cell: every row of the probed source retrying and parked, 200,000
//     wait rows; the wall time is logged and the PLAN is asserted instead (the
//     wait-table probe ran once per scanned row as an index lookup, never a
//     sequential scan), because wall time at that K is load-dependent evidence.
func runS0ParkedCells(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, sqlConn *sql.Conn, now time.Time, probed string, budget time.Duration) {
	t.Helper()
	probe := boundedSourceSignalQuery()
	args := s0ProbeParams(now, probed)[1:]

	s0InsertOtherSourceWaitRows(t, ctx, conn, now, probed, 50_000)
	const k = 1_000
	retried := s0RetryAndParkProbedRows(t, ctx, conn, now, probed, k)
	p50 := timeQuerySingle(t, ctx, sqlConn, probe, now, probed, 25)
	plan := explainOne(t, ctx, sqlConn, probe, now, args...)
	t.Logf("S0 parked cell (50,000 other-source wait rows, K=%d visible retrying rows parked): bounded p50=%s plan:\n%s", retried, p50, plan)
	if p50 > budget {
		t.Fatalf("S0 populated cell bounded p50 = %s, exceeds %s", p50, budget)
	}

	s0InsertOtherSourceWaitRows(t, ctx, conn, now, probed, 200_000)
	all := s0RetryAndParkProbedRows(t, ctx, conn, now, probed, -1)
	worstP50 := timeQuerySingle(t, ctx, sqlConn, probe, now, probed, 15)
	worstPlan := explainOne(t, ctx, sqlConn, probe, now, args...)
	t.Logf("S0 parked worst cell (200,000 other-source wait rows, K=%d, every probed row retrying and parked): bounded p50=%s (logged, not asserted) plan:\n%s", all, worstP50, worstPlan)
	loops := waitTableScanLoops(worstPlan)
	probes := 0
	for _, n := range loops {
		probes += n
	}
	if probes != all {
		t.Fatalf("worst-cell plan ran the wait-table probe %d times (loops %v), want once per scanned row (%d)", probes, loops, all)
	}
	if strings.Contains(worstPlan, "Seq Scan on reducer_waiting_items") {
		t.Fatalf("worst-cell plan seq-scans the wait table; the probe must be one index lookup per row:\n%s", worstPlan)
	}
}
