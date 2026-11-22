// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestSourceBackpressureSignalSurvivesAnEmptyThenFilledWaitTable is the live
// proof of the #11187 R4-3 residual fix (the consult reads run unprepared).
// The wait table is analyzed while empty, the production consult runs eight
// times (enough for pgx's cached prepared statement to adopt a generic plan
// under plan_cache_mode=auto), then 50,000 wait rows arrive with no ANALYZE,
// the state an autovacuum that has not yet run leaves behind. A generic plan
// made against the empty table keeps its sequential scan of the wait table once
// per visible retrying row (measured on the 10x seed: the bounded probe went
// from 95 ms to 3.9 s at 50,000 wait rows). With the consult unprepared every
// run plans against the current table, so the plan is the index form and the
// consult stays fast. The 2,000 retrying rows make a stuck plan cost seconds, so
// the time bound has a wide margin on any host.
func TestSourceBackpressureSignalSurvivesAnEmptyThenFilledWaitTable(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the stale-plan proof")
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
	schemaName := fmt.Sprintf("source_bp_sp_%d", time.Now().UnixNano())
	defer func() { _, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set search_path: %v", err)
	}
	// No autovacuum between the fill and the measurement.
	if _, err := conn.ExecContext(ctx, "ALTER TABLE reducer_waiting_items SET (autovacuum_enabled = false)"); err != nil {
		t.Fatalf("disable autovacuum: %v", err)
	}

	now := time.Now().UTC()
	const retrying = 2000
	seedTakeableShape(t, ctx, conn, now, "sp-stuck", "stuck", "supply_chain_impact", "retrying", "", retrying, 2*time.Hour)
	if _, err := conn.ExecContext(ctx, "ANALYZE fact_work_items"); err != nil {
		t.Fatalf("analyze fact_work_items: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "ANALYZE reducer_waiting_items"); err != nil { // analyzed while empty
		t.Fatalf("analyze empty wait table: %v", err)
	}
	store := NewWorkflowControlStore(conn)
	cfg := sourceBackpressureTestConfig()
	for i := 0; i < 8; i++ {
		if _, err := store.sourceClaimableSignalForSource(ctx, cfg, "stuck", now); err != nil {
			t.Fatalf("prime consult %d: %v", i, err)
		}
	}

	// Fill the wait table with rows that match no queue row, with no ANALYZE.
	if _, err := conn.ExecContext(ctx, `INSERT INTO reducer_waiting_items (
work_item_id, waits_for_kind, waits_for_key, scope_id, generation_id, domain,
failure_class, first_deferred_at, updated_at)
SELECT 'ghost-' || g, 'phase_committed', jsonb_build_object('g', g), 'ghost-scope', 'ghost-gen',
       'supply_chain_impact', 'readiness_wait', $1, $1
FROM generate_series(1, 50000) AS g`, now); err != nil {
		t.Fatalf("fill wait table: %v", err)
	}

	start := time.Now()
	signal, err := store.sourceClaimableSignalForSource(ctx, cfg, "stuck", now)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("consult after the fill: %v", err)
	}
	if signal.ClaimableDepth != retrying {
		t.Fatalf("consult depth = %d, want %d visible retrying rows with no wait row", signal.ClaimableDepth, retrying)
	}
	if took > 2*time.Second {
		t.Fatalf("consult took %s after the wait table filled with stale statistics: the signal read reuses a cached generic plan that seq-scans the wait table per retrying row", took)
	}

	// The plan the unprepared read gets is the index form.
	rows, err := sqlConn.QueryContext(ctx, "EXPLAIN (ANALYZE, COSTS OFF, BUFFERS OFF, TIMING OFF) "+boundedSourceSignalQuery(),
		unpreparedSignalArgs(now.UTC(), now.UTC().Add(-cfg.EngageAge), now.UTC().Add(-cfg.ReleaseAge), "stuck")...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan explain: %v", err)
		}
		lines = append(lines, line)
	}
	_ = rows.Close()
	plan := strings.Join(lines, "\n")
	assertSingleGuardedProbe(t, "unprepared bounded probe after the fill", plan, retrying)
}
