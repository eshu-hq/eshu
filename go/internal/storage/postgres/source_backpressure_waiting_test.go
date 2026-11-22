// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// waitingCensusQuery classifies each source's visible claimable reducer rows
// (active generation, pending or retrying, visible now) by wait state, the
// classes the #11187 R4-3 ruling names:
//
//   - visible_retrying: status retrying. Only these rows can be parked, so only
//     these pay a wait-table lookup (the peak is the K of the cost proof).
//   - parked: retrying with a reducer_waiting_items row. Not counted by the
//     signal; these are rows whose 10-minute backstop expired with the wake
//     still outstanding.
//   - dangling: not retrying but a wait row still exists (an item left the
//     parked state through another path; the audit reconciles it). Counted.
//   - non_counting_no_wait: a non-counting readiness failure class with no wait
//     row and no wake mark. It takes the timer path and stays counted.
//   - woken: reopened_at set and no wait row. Counted, ages from the wake.
//   - woken_with_wait: reopened_at set and a wait row for another key still
//     exists. Not counted until re-claimed (an under-count: it cannot hold a
//     latch); the replay reports it and a material count returns to the arbiter.
//
// It is a census, not production SQL.
var waitingCensusQuery = `
WITH ` + activeFactWorkItemsPerRowCTE + `,
waitable AS (
    SELECT COALESCE(NULLIF(BTRIM(work.payload->>'source_system'), ''), NULLIF(BTRIM(scope.source_system), ''), 'unknown') AS source_system,
           work.status = 'retrying' AS retrying,
           EXISTS (SELECT 1 FROM reducer_waiting_items AS wait WHERE wait.work_item_id = work.work_item_id) AS has_wait,
           work.reopened_at IS NOT NULL AS reopened,
           ` + reducerNonCountingFailureClassPredicateSQL("work") + ` AS non_counting
    FROM active_fact_work_items AS work
    JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id
    WHERE work.stage = 'reducer'
      AND work.status IN ('pending', 'retrying')
      AND (work.visible_at IS NULL OR work.visible_at <= $1)
)
SELECT source_system,
       COUNT(*) FILTER (WHERE retrying) AS visible_retrying,
       COUNT(*) FILTER (WHERE retrying AND has_wait) AS parked,
       COUNT(*) FILTER (WHERE NOT retrying AND has_wait) AS dangling,
       COUNT(*) FILTER (WHERE NOT has_wait AND NOT reopened AND non_counting) AS non_counting_no_wait,
       COUNT(*) FILTER (WHERE NOT has_wait AND reopened) AS woken,
       COUNT(*) FILTER (WHERE has_wait AND reopened) AS woken_with_wait
FROM waitable
GROUP BY source_system
ORDER BY source_system
`

// waitingCensusRow is one source's wait-state census (see waitingCensusQuery).
type waitingCensusRow struct {
	visibleRetrying, parked, dangling, nonCountingNoWait, woken, wokenWithWait int64
}

func (r waitingCensusRow) String() string {
	return fmt.Sprintf("visible_retrying=%d parked=%d dangling=%d non_counting_no_wait=%d woken=%d woken_with_wait=%d",
		r.visibleRetrying, r.parked, r.dangling, r.nonCountingNoWait, r.woken, r.wokenWithWait)
}

func readWaitingCensus(t *testing.T, ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time) map[string]waitingCensusRow {
	t.Helper()
	rows, err := conn.QueryContext(ctx, waitingCensusQuery, now.UTC())
	if err != nil {
		t.Fatalf("waiting census: %v", err)
	}
	defer func() { _ = rows.Close() }()
	census := map[string]waitingCensusRow{}
	for rows.Next() {
		var source string
		var r waitingCensusRow
		if err := rows.Scan(&source, &r.visibleRetrying, &r.parked, &r.dangling, &r.nonCountingNoWait, &r.woken, &r.wokenWithWait); err != nil {
			t.Fatalf("scan waiting census: %v", err)
		}
		census[source] = r
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("waiting census rows: %v", err)
	}
	return census
}

var waitTableLoopsRe = regexp.MustCompile(`loops=([0-9]+)`)

// waitTableScanLoops returns, for each plan node that reads reducer_waiting_items,
// how many times it ran: 0 for a node EXPLAIN marks "never executed".
func waitTableScanLoops(plan string) []int {
	var loops []int
	for _, line := range strings.Split(plan, "\n") {
		if !strings.Contains(line, "reducer_waiting_items") {
			continue
		}
		if strings.Contains(line, "never executed") {
			loops = append(loops, 0)
			continue
		}
		if m := waitTableLoopsRe.FindStringSubmatch(line); m != nil {
			n, _ := strconv.Atoi(m[1])
			loops = append(loops, n)
		}
	}
	return loops
}

// TestSourceBackpressureParkedProbeIsGuardedByRetrying is the live plan proof
// for the R4-3 cost ruling. The wait-table probe sits behind work.status =
// 'retrying', so a probed source with no visible retrying rows never reads the
// wait table, however large it is: the bounded probe's EXPLAIN shows the
// wait-table node "never executed". A retrying row runs the probe once, and
// the probe stays a single lateral lookup per scanned row. Dropping the guard
// turns the first assertion RED (the node runs once per pending row).
func TestSourceBackpressureParkedProbeIsGuardedByRetrying(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the parked-probe plan proof")
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
	schemaName := fmt.Sprintf("source_bp_pg_%d", time.Now().UnixNano())
	defer func() { _, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}

	now := time.Now().UTC()
	const rows = 40
	seedTakeableShape(t, ctx, conn, now, "pp-plain", "plain", "supply_chain_impact", "pending", "", rows, 2*time.Hour)
	// Another source holds wait rows (dangling: its rows are pending), so the
	// wait table is populated while no retrying row exists anywhere yet.
	seedTakeableShape(t, ctx, conn, now, "pp-other", "other", "supply_chain_impact", "pending", "", rows, 2*time.Hour)
	parkTakeableRows(t, ctx, conn, "pp-other", now)
	if _, err := conn.ExecContext(ctx, "ANALYZE fact_work_items"); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	explain := func(source string) string {
		t.Helper()
		cfg := sourceBackpressureTestConfig()
		return explainOne(t, ctx, sqlConn, boundedSourceSignalQuery(), now,
			now.Add(-cfg.EngageAge), now.Add(-cfg.ReleaseAge), source)
	}

	plain := explain("plain")
	loops := waitTableScanLoops(plain)
	if len(loops) == 0 {
		t.Fatalf("the plan never mentions reducer_waiting_items: the parked probe is missing\n%s", plain)
	}
	for _, n := range loops {
		if n != 0 {
			t.Fatalf("pending-only source read the wait table (loops %v): the retrying guard is missing\n%s", loops, plain)
		}
	}

	// The grouped query has the same guard: with no retrying row anywhere its
	// wait-table node is never executed either.
	groupedExplain := func() string {
		t.Helper()
		cfg := sourceBackpressureTestConfig()
		return explainOne(t, ctx, sqlConn, sourceClaimableSignalQuery, now, now.Add(-cfg.EngageAge), now.Add(-cfg.ReleaseAge))
	}
	groupedPlain := groupedExplain()
	assertSingleGuardedProbe(t, "grouped, no retrying rows", groupedPlain, 0)

	// The same source turned retrying and parked probes once per row.
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET status = 'retrying' WHERE scope_id = 'pp-plain'`); err != nil {
		t.Fatalf("retry plain rows: %v", err)
	}
	parkTakeableRows(t, ctx, conn, "pp-plain", now)
	assertSingleGuardedProbe(t, "bounded, retrying rows", explain("plain"), rows)

	// The grouped query probes every visible retrying row it scans, once each.
	if _, err := conn.ExecContext(ctx, `UPDATE fact_work_items SET status = 'retrying' WHERE scope_id = 'pp-other'`); err != nil {
		t.Fatalf("retry other rows: %v", err)
	}
	assertSingleGuardedProbe(t, "grouped, retrying rows", groupedExplain(), 2*rows)
}

// assertSingleGuardedProbe checks one signal plan against the R4-3 cost shape:
// exactly one wait-table node, an index-only lookup (never a sequential scan)
// under a Limit and a Result, run wantLoops times: the number of visible
// retrying rows the plan scanned, 0 when it scanned none ("never executed").
func assertSingleGuardedProbe(t *testing.T, name, plan string, wantLoops int) {
	t.Helper()
	loops := waitTableScanLoops(plan)
	if len(loops) != 1 {
		t.Fatalf("%s: plan reads reducer_waiting_items %d times (loops %v), want exactly one probe node\n%s", name, len(loops), loops, plan)
	}
	if loops[0] != wantLoops {
		t.Fatalf("%s: the wait-table probe ran %d times, want %d (one per visible retrying row scanned; the retrying guard keeps every other row off it)\n%s", name, loops[0], wantLoops, plan)
	}
	var nodes []string
	for _, line := range strings.Split(plan, "\n") {
		if strings.Contains(line, "->") {
			nodes = append(nodes, strings.TrimSpace(line))
		}
	}
	for i, node := range nodes {
		if !strings.Contains(node, "reducer_waiting_items") {
			continue
		}
		if !strings.Contains(node, "Index Only Scan using reducer_waiting_items_pkey") {
			t.Fatalf("%s: the wait-table probe is %q, want an Index Only Scan on the primary key\n%s", name, node, plan)
		}
		if i < 2 || !strings.HasPrefix(nodes[i-1], "->  Result") || !strings.HasPrefix(nodes[i-2], "->  Limit") {
			t.Fatalf("%s: the wait-table node must sit under Limit -> Result, got %v\n%s", name, nodes[max(0, i-2):i+1], plan)
		}
	}
}
