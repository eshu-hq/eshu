// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestReducerQueue10879UpdateCensusByCause splits fact_work_items updates by
// cause on the #10862 replay mix (#10879 acceptance criterion 1) and reports
// the in-place (HOT) share per cause (criterion 2, before side). Every phase
// drives the real ReducerQueue methods; per-phase pg_stat_user_tables deltas
// attribute each heap update to exactly one cause with no schema change.
//
// Expected lifecycle budget per item: 1 claim + H heartbeats + 1 terminal
// (ack / retry / dead-letter) + R reopens. The arbiter ruling on #10879
// proceeds with the lease split only if lease-only (heartbeat) writes are
// >= 30% of updates, and kills the theory below 10%.
func TestReducerQueue10879UpdateCensusByCause(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_CLAIM_BENCH_DSN or ESHU_POSTGRES_DSN to run the 10879 census")
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

	schemaName := fmt.Sprintf("reducer_queue_10879_%d", time.Now().UnixNano())
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE")
	}()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create census schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("fact_records")); err != nil {
		t.Fatalf("create census fact_records: %v", err)
	}
	if _, err := conn.ExecContext(ctx, MigrationSQL("cross_scope_completion_queue")); err != nil {
		t.Fatalf("create completion events table: %v", err)
	}
	// The post-ACK producer-terminal wake (#10876) reads this table on
	// every Ack; with no parked waits it matches zero rows and writes
	// nothing, so the finish phase stays exactly one heap update per Ack.
	if _, err := conn.ExecContext(ctx, MigrationSQL("reducer_waiting_items")); err != nil {
		t.Fatalf("create waiting items table: %v", err)
	}
	now := time.Now().UTC()
	seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now)
	if seeded < 8000 {
		t.Fatalf("census seed = %d, want >= 8000 (issue acceptance)", seeded)
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze census tables: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set census search_path: %v", err)
	}

	queue := ReducerQueue{
		database:      conn,
		LeaseOwner:    "census-10879",
		LeaseDuration: time.Minute,
		ClaimDomains:  []reducercontract.Domain{reducercontract.DomainSupplyChainImpact},
		MaxAttempts:   3,
	}

	const (
		nClaim  = 40
		nBeat   = 2
		nAck    = 15
		nRetry  = 15
		nFail   = 10
		nReopen = nAck
	)
	statements := nClaim + nClaim*nBeat + nAck + nRetry + nFail + nReopen

	snap := func() (upd, hot uint64) {
		upd, hot, err := snapshotFactWorkItemsStats(ctx, conn)
		if err != nil {
			t.Fatalf("snapshot queue stats: %v", err)
		}
		return upd, hot
	}
	delta := func(beforeUpd, beforeHot uint64) (upd, hot uint64) {
		afterUpd, afterHot := snap()
		return afterUpd - beforeUpd, afterHot - beforeHot
	}

	bu, bh := snap()
	intents := make([]reducercontract.Intent, 0, nClaim)
	for i := 0; i < nClaim; i++ {
		intent, claimed, err := queue.Claim(ctx)
		if err != nil {
			t.Fatalf("census claim %d: %v", i, err)
		}
		if !claimed {
			t.Fatalf("census claim %d: no work left, want %d supply_chain rows", i, nClaim)
		}
		intents = append(intents, intent)
	}
	claimUpd, claimHot := delta(bu, bh)

	bu, bh = snap()
	for r := 0; r < nBeat; r++ {
		for _, intent := range intents {
			if err := queue.Heartbeat(ctx, intent); err != nil {
				t.Fatalf("census heartbeat %s: %v", intent.IntentID, err)
			}
		}
	}
	beatUpd, beatHot := delta(bu, bh)

	bu, bh = snap()
	for _, intent := range intents[:nAck] {
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			t.Fatalf("census ack %s: %v", intent.IntentID, err)
		}
	}
	ackUpd, ackHot := delta(bu, bh)

	bu, bh = snap()
	for _, intent := range intents[nAck : nAck+nRetry] {
		if err := queue.Fail(ctx, intent, retryableReducerTestError{message: "census transient"}); err != nil {
			t.Fatalf("census retry %s: %v", intent.IntentID, err)
		}
	}
	retryUpd, retryHot := delta(bu, bh)

	bu, bh = snap()
	for _, intent := range intents[nAck+nRetry : nAck+nRetry+nFail] {
		if err := queue.Fail(ctx, intent, errors.New("census terminal")); err != nil {
			t.Fatalf("census dead-letter %s: %v", intent.IntentID, err)
		}
	}
	failUpd, failHot := delta(bu, bh)

	bu, bh = snap()
	for _, intent := range intents[:nReopen] {
		ok, err := queue.ReopenSucceeded(ctx, intent.IntentID, telemetry.RequeueReasonMaintenanceReopen)
		if err != nil {
			t.Fatalf("census reopen %s: %v", intent.IntentID, err)
		}
		if !ok {
			t.Fatalf("census reopen %s: no succeeded row transitioned", intent.IntentID)
		}
	}
	reopenUpd, reopenHot := delta(bu, bh)

	totalUpd := claimUpd + beatUpd + ackUpd + retryUpd + failUpd + reopenUpd
	totalHot := claimHot + beatHot + ackHot + retryHot + failHot + reopenHot
	t.Logf("10879 census: statements=%d heap_updates=%d heap_hot=%d", statements, totalUpd, totalHot)
	t.Logf("cause      updates  hot")
	t.Logf("claim      %d  %d", claimUpd, claimHot)
	t.Logf("renew      %d  %d", beatUpd, beatHot)
	t.Logf("finish     %d  %d", ackUpd, ackHot)
	t.Logf("retry      %d  %d", retryUpd, retryHot)
	t.Logf("deadletter %d  %d", failUpd, failHot)
	t.Logf("reopen     %d  %d", reopenUpd, reopenHot)

	// Trigger attribution (#10879 cause "triggers"): every DB trigger on
	// fact_work_items either rewrites NEW in a BEFORE ROW trigger (no extra
	// heap update) or writes another table (completion events). If any
	// trigger issued an extra UPDATE on this table, the heap delta would
	// exceed the statements the harness issued.
	if totalUpd != uint64(statements) {
		t.Fatalf("heap updates = %d, want %d issued statements (excess = trigger-driven rewrites)", totalUpd, statements)
	}
	// In-place share per cause: every cause writes updated_at, which the
	// fairness/order key indexes, so no cause can be HOT today. A nonzero
	// HOT delta anywhere below means the theory's premise changed.
	if totalHot != 0 {
		t.Fatalf("HOT updates = %d, want 0 (every cause writes indexed updated_at)", totalHot)
	}

	indexed := listFactWorkItemsIndexedColumns(ctx, conn, t)
	t.Logf("indexed fact_work_items columns: %v", indexed)
	seen := map[string]bool{}
	for _, col := range indexed {
		seen[col] = true
	}
	for _, key := range []string{"updated_at", "status"} {
		if !seen[key] {
			t.Fatalf("column %s not indexed, want indexed (HOT-blocker premise)", key)
		}
	}

	// Verdict math against the arbiter thresholds: per lifecycle of 1 claim
	// + H heartbeats + 1 terminal (+ R reopens), the heartbeat share is
	// H/(2+H+R). The split proceeds at >= 30% (H >= 1 with R = 0) and is
	// dead below 10%.
	for _, h := range []int{1, 2, 5} {
		share := float64(h) / float64(2+h)
		t.Logf("modeled lifecycle H=%d: heartbeat share=%.1f%%", h, 100*share)
	}
}

// snapshotFactWorkItemsStats reads the heap-update counters for the
// search_path-visible fact_work_items table. It forces a stats flush first
// so per-phase deltas are exact, not collector-lagged. Read-only.
func snapshotFactWorkItemsStats(ctx context.Context, conn reducerClaimBenchmarkConn) (upd, hot uint64, err error) {
	if _, err := conn.ExecContext(ctx, "SELECT pg_stat_force_next_flush()"); err != nil {
		return 0, 0, fmt.Errorf("flush stats collector: %w", err)
	}
	rows, err := conn.QueryContext(ctx, `
SELECT n_tup_upd, n_tup_hot_upd
FROM pg_stat_user_tables
WHERE schemaname = current_schema()
  AND relname = 'fact_work_items'`)
	if err != nil {
		return 0, 0, fmt.Errorf("read queue table stats: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, 0, fmt.Errorf("no pg_stat row for fact_work_items in schema %s", currentSchemaName(ctx, conn))
	}
	if err := rows.Scan(&upd, &hot); err != nil {
		return 0, 0, fmt.Errorf("scan queue table stats: %w", err)
	}
	return upd, hot, rows.Err()
}

func currentSchemaName(ctx context.Context, conn reducerClaimBenchmarkConn) string {
	rows, err := conn.QueryContext(ctx, "SELECT current_schema()")
	if err != nil {
		return "unknown: " + err.Error()
	}
	defer func() { _ = rows.Close() }()
	var name string
	if rows.Next() {
		if err := rows.Scan(&name); err != nil {
			return "unknown: " + err.Error()
		}
	}
	return name
}

// listFactWorkItemsIndexedColumns returns the distinct indexed column names
// of the search_path-visible fact_work_items table, from the catalog.
// Read-only: it documents which SET columns force a non-HOT update.
func listFactWorkItemsIndexedColumns(ctx context.Context, conn reducerClaimBenchmarkConn, t *testing.T) []string {
	t.Helper()
	rows, err := conn.QueryContext(ctx, `
SELECT DISTINCT a.attname
FROM pg_index i
JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY (i.indkey)
WHERE i.indrelid = to_regclass('fact_work_items')
ORDER BY 1`)
	if err != nil {
		t.Fatalf("read indexed columns: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var col string
		if err := rows.Scan(&col); err != nil {
			t.Fatalf("scan indexed column: %v", err)
		}
		cols = append(cols, col)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate indexed columns: %v", err)
	}
	return cols
}
