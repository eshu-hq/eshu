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

// TestSourceBackpressureS0SignalCost is the #10882 S0 theory probe: it
// measures the grouped claimable per-source signal query (the claim-path
// consult) on the #10862 replay fixture and on a 10x synthetic, and
// reports the cost against the single-claim probe on the same fixture.
// It is gated on ESHU_POSTGRES_DSN like the #10862 replay test and never
// touches QA. The verdict it records (grouped scan vs bounded per-family
// probe) is the S1 input the design's prove-the-theory gate requires.
func TestSourceBackpressureS0SignalCost(t *testing.T) {
	dsn := reducerClaimBenchmarkDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the source-backpressure S0 probe")
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

	schemaName := fmt.Sprintf("source_bp_s0_%d", time.Now().UnixNano())
	defer func() { _, _ = conn.ExecContext(context.Background(), "DROP SCHEMA "+schemaName+" CASCADE") }()
	if err := createReducerClaimBenchmarkSchema(ctx, conn, schemaName); err != nil {
		t.Fatalf("create replay schema: %v", err)
	}
	for _, def := range []string{"workflow_control_plane", "workflow_claim_pacing"} {
		if _, err := conn.ExecContext(ctx, MigrationSQL(def)); err != nil {
			t.Fatalf("create %s: %v", def, err)
		}
	}

	now := time.Now().UTC()
	seeded := seedReducerQueue10862Replay(t, ctx, conn, schemaName, now)
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze replay tables: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
		t.Fatalf("set replay search_path: %v", err)
	}

	// The replay seed attributes every row to one payload source, so the
	// grouped signal returns exactly one group; the scan still covers the
	// whole claimable backlog, which is what the probe measures.
	plan, rows := explainSignalQuery(t, ctx, sqlConn, sourceClaimableSignalQuery, now)
	t.Logf("S0 replay-scale (%d rows): groups=%d plan:\n%s", seeded, len(rows), plan)

	// The #6794 doctrine reserves the per-row CTE for selective probes
	// and the materialized form for full-set aggregates. This signal is
	// a full-set aggregate, so the probe measures the materialized
	// form derived from the shipped constant on the same fixture.
	materialized := strings.Replace(sourceClaimableSignalQuery, activeFactWorkItemsPerRowCTE, activeFactWorkItemsCTE, 1)
	if materialized == sourceClaimableSignalQuery {
		t.Fatal("materialized S0 variant did not replace the per-row CTE")
	}
	matPlan, matRows := explainSignalQuery(t, ctx, sqlConn, materialized, now)
	matP50 := timeQuery(t, ctx, sqlConn, materialized, now, 25)
	t.Logf("S0 replay-scale materialized: groups=%d p50=%s plan:\n%s", len(matRows), matP50, matPlan)
	if len(matRows) != len(rows) {
		t.Fatalf("materialized groups = %d, want %d (per-row form)", len(matRows), len(rows))
	}

	signalP50 := timeQuery(t, ctx, sqlConn, sourceClaimableSignalQuery, now, 25)
	claimP50 := timeClaimProbe(t, ctx, sqlConn, now, 25)
	t.Logf("S0 replay-scale wall: signal p50=%s claim-probe p50=%s", signalP50, claimP50)

	if len(rows) != 1 || rows[0].source != "replay" {
		t.Fatalf("replay signal groups = %+v, want single replay group", rows)
	}
	if rows[0].depth != int64(seeded) {
		t.Fatalf("replay signal depth = %d, want %d", rows[0].depth, seeded)
	}

	// 10x synthetic: nine more replay mixes beside the seeded one,
	// attributed across six sources with the same oldest-age tail, so the
	// grouped scan covers ~87k claimable rows.
	const sources10x = 6
	if err := seedS010xSynthetic(ctx, conn, now, sources10x); err != nil {
		t.Fatalf("seed 10x synthetic: %v", err)
	}
	if err := analyzeReducerClaimBenchmarkTables(ctx, conn); err != nil {
		t.Fatalf("analyze 10x tables: %v", err)
	}
	plan10x, rows10x := explainSignalQuery(t, ctx, sqlConn, sourceClaimableSignalQuery, now)
	t.Logf("S0 10x-scale: groups=%d plan:\n%s", len(rows10x), plan10x)
	signal10xP50 := timeQuery(t, ctx, sqlConn, sourceClaimableSignalQuery, now, 25)
	mat10xP50 := timeQuery(t, ctx, sqlConn, materialized, now, 25)
	claim10xP50 := timeClaimProbe(t, ctx, sqlConn, now, 25)
	t.Logf("S0 10x-scale wall: per-row signal p50=%s materialized p50=%s claim-probe p50=%s",
		signal10xP50, mat10xP50, claim10xP50)

	// The replay seed's single 'replay' group persists beside the six
	// synthetic sources.
	if len(rows10x) != sources10x+1 {
		t.Fatalf("10x signal groups = %d, want %d", len(rows10x), sources10x+1)
	}

	// Existing-index variant: the shipped fact_work_items table already
	// carries fact_work_items_reducer_source_claim_idx on the payload
	// source expression (COALESCE(NULLIF(BTRIM(...)), 'unknown')). The
	// probe below rewrites the prefilter in exactly those terms, so no
	// new DDL is needed if it matches the custom-index timing.
	if _, err := sqlConn.ExecContext(ctx, "ANALYZE fact_work_items"); err != nil {
		t.Fatalf("analyze after 10x seed: %v", err)
	}
	indexedP50 := timeQuery(t, ctx, sqlConn, sourceClaimableSignalQuery, now, 25)
	t.Logf("S0 10x-scale grouped (existing indexes only): p50=%s", indexedP50)
	// Full-fidelity bounded probe: the shipped S1 consult shape
	// (boundedSourceSignalQuery, derived from the grouped constant).
	// Unlike the floor probe above it keeps the active-generation
	// filter, so its timing is the shippable consult shape for one
	// family source.
	fullFidelityProbe := boundedSourceSignalQuery()
	fullP50 := timeQuerySingle(t, ctx, sqlConn, fullFidelityProbe, now, "s0-source-0", 25)
	fullPlan := explainOne(t, ctx, sqlConn, fullFidelityProbe, now, s0ProbeParams(now, "s0-source-0")[1:]...)
	t.Logf("S0 10x-scale full-fidelity per-source probe: p50=%s plan:\n%s", fullP50, fullPlan)

	// Sargable form is the shipped builder verbatim: the prefilter is
	// already spelled in the existing index's expression terms, so the
	// planner drives the work scan from
	// fact_work_items_reducer_source_claim_idx with no new DDL.
	sargableProbe := fullFidelityProbe
	sargP50 := timeQuerySingle(t, ctx, sqlConn, sargableProbe, now, "s0-source-0", 25)
	sargPlan := explainOne(t, ctx, sqlConn, sargableProbe, now, s0ProbeParams(now, "s0-source-0")[1:]...)
	t.Logf("S0 10x-scale sargable per-source probe: p50=%s plan:\n%s", sargP50, sargPlan)
	for _, want := range rows10x {
		var age float64
		var depth, pastEngage, pastRelease int64
		if err := sqlConn.QueryRowContext(ctx, sargableProbe, s0ProbeParams(now, want.source)...).Scan(&age, &depth, &pastEngage, &pastRelease); err != nil {
			t.Fatalf("sargable probe for %s: %v", want.source, err)
		}
		if depth != want.depth || pastEngage != want.pastEngage || pastRelease != want.pastRelease {
			t.Fatalf("sargable probe for %s = (depth %d, past engage %d, past release %d), want grouped (%d, %d, %d)",
				want.source, depth, pastEngage, pastRelease, want.depth, want.pastEngage, want.pastRelease)
		}
	}
	// Row-equivalence between the bounded probe and the grouped signal
	// for every 10x source: the S1 consult must see the same numbers.
	for _, want := range rows10x {
		var age float64
		var depth, pastEngage, pastRelease int64
		if err := sqlConn.QueryRowContext(ctx, fullFidelityProbe, s0ProbeParams(now, want.source)...).Scan(&age, &depth, &pastEngage, &pastRelease); err != nil {
			t.Fatalf("bounded probe for %s: %v", want.source, err)
		}
		if depth != want.depth || pastEngage != want.pastEngage || pastRelease != want.pastRelease {
			t.Fatalf("bounded probe for %s = (depth %d, past engage %d, past release %d), want grouped (%d, %d, %d)",
				want.source, depth, pastEngage, pastRelease, want.depth, want.pastEngage, want.pastRelease)
		}
		if gotAge := time.Duration(age * float64(time.Second)); gotAge != want.age {
			t.Fatalf("bounded probe age for %s = %s, want grouped %s", want.source, gotAge, want.age)
		}
	}
	const perSourceProbe = `SELECT COALESCE(EXTRACT(EPOCH FROM ($1 - MIN(COALESCE(work.reopened_at, work.created_at)))), 0) AS oldest_age_seconds,
COUNT(*) AS claimable_depth
FROM fact_work_items AS work
WHERE work.stage = 'reducer'
  AND work.status IN ('pending', 'retrying')
  AND (work.visible_at IS NULL OR work.visible_at <= $1)
  AND (work.payload ->> 'source_system') = $2`
	perSourceP50 := timePerSourceProbe(t, ctx, sqlConn, perSourceProbe, now, "s0-source-0", 25)
	planPerSource := explainOne(t, ctx, sqlConn, perSourceProbe, now, "s0-source-0")
	t.Logf("S0 10x-scale per-source bounded probe: p50=%s plan:\n%s", perSourceP50, planPerSource)

	// The S1 gate (S0 verdict): the grouped scan stays above
	// low-millisecond at 10x on this fixture (see walls above), so the
	// claim-hot consult path takes the bounded per-source probe. The
	// bounded probe must stay low-millisecond at 10x on the same
	// fixture; the grouped text stays for the publication path only.
	const s0Budget = 100 * time.Millisecond
	if sargP50 > s0Budget {
		t.Fatalf("S0 bounded 10x probe p50 = %s, exceeds %s", sargP50, s0Budget)
	}
	t.Logf("S0 verdict: grouped p50=%s exceeds budget; bounded p50=%s within %s: S1 ships the bounded consult, grouped stays for publication",
		signal10xP50, sargP50, s0Budget)
}

// sourceSignalRow is one decoded grouped-signal row for the S0 probe.
type sourceSignalRow struct {
	source      string
	age         time.Duration
	depth       int64
	pastEngage  int64
	pastRelease int64
}

// explainSignalQuery runs EXPLAIN (ANALYZE, BUFFERS) over a signal query
// text and decodes the live signal rows the same text returns.
func explainSignalQuery(t *testing.T, ctx context.Context, sqlConn *sql.Conn, signalQuery string, now time.Time) (string, []sourceSignalRow) {
	t.Helper()
	explainRows, err := sqlConn.QueryContext(ctx,
		"EXPLAIN (ANALYZE, TIMING OFF, COSTS OFF, BUFFERS) "+signalQuery, s0GroupedParams(now)...)
	if err != nil {
		t.Fatalf("explain signal query: %v", err)
	}
	var lines []string
	for explainRows.Next() {
		var line string
		if err := explainRows.Scan(&line); err != nil {
			_ = explainRows.Close()
			t.Fatalf("scan explain: %v", err)
		}
		lines = append(lines, line)
	}
	if err := explainRows.Err(); err != nil {
		_ = explainRows.Close()
		t.Fatalf("explain rows: %v", err)
	}
	_ = explainRows.Close()

	dataRows, err := sqlConn.QueryContext(ctx, signalQuery, s0GroupedParams(now)...)
	if err != nil {
		t.Fatalf("signal query: %v", err)
	}
	var rows []sourceSignalRow
	for dataRows.Next() {
		var row sourceSignalRow
		var ageSeconds float64
		if err := dataRows.Scan(&row.source, &ageSeconds, &row.depth, &row.pastEngage, &row.pastRelease); err != nil {
			_ = dataRows.Close()
			t.Fatalf("scan signal row: %v", err)
		}
		row.age = time.Duration(ageSeconds * float64(time.Second))
		rows = append(rows, row)
	}
	if err := dataRows.Err(); err != nil {
		_ = dataRows.Close()
		t.Fatalf("signal rows: %v", err)
	}
	_ = dataRows.Close()
	return strings.Join(lines, "\n"), rows
}

// timeQuery reports the median wall time of n reads of a signal query.
func timeQuery(t *testing.T, ctx context.Context, sqlConn *sql.Conn, signalQuery string, now time.Time, n int) time.Duration {
	t.Helper()
	samples := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		rows, err := sqlConn.QueryContext(ctx, signalQuery, s0GroupedParams(now)...)
		if err != nil {
			t.Fatalf("time signal query: %v", err)
		}
		for rows.Next() {
			var source string
			var age float64
			var depth, pastEngage, pastRelease int64
			if err := rows.Scan(&source, &age, &depth, &pastEngage, &pastRelease); err != nil {
				_ = rows.Close()
				t.Fatalf("scan signal query: %v", err)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			t.Fatalf("signal query rows: %v", err)
		}
		_ = rows.Close()
		samples = append(samples, float64(time.Since(start)))
	}
	return time.Duration(percentileSorted(samples, 0.5))
}

// timeClaimProbe reports the median wall time of n single-claim-shaped
// probes: the oldest visible unleased reducer row, the same shape the
// hand-out path floats first. It is the claim-cost baseline the consult
// cost is reported against.
func timeClaimProbe(t *testing.T, ctx context.Context, sqlConn *sql.Conn, now time.Time, n int) time.Duration {
	t.Helper()
	const claimProbe = `SELECT work_item_id FROM fact_work_items
WHERE stage = 'reducer'
  AND status IN ('pending', 'retrying')
  AND (visible_at IS NULL OR visible_at <= $1)
  AND (claim_until IS NULL OR claim_until <= $1)
ORDER BY updated_at, work_item_id
LIMIT 1`
	samples := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		var id string
		if err := sqlConn.QueryRowContext(ctx, claimProbe, now.UTC()).Scan(&id); err != nil {
			t.Fatalf("claim probe: %v", err)
		}
		samples = append(samples, float64(time.Since(start)))
	}
	return time.Duration(percentileSorted(samples, 0.5))
}

// timePerSourceProbe reports the median wall time of n bounded
// per-source probe reads. The probe text is an experiment bound only:
// it omits the active-generation filter the shipped consult applies,
// so its timing is a floor, not a shippable cost.
func timePerSourceProbe(t *testing.T, ctx context.Context, sqlConn *sql.Conn, probe string, now time.Time, source string, n int) time.Duration {
	t.Helper()
	samples := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		var age float64
		var depth int64
		if err := sqlConn.QueryRowContext(ctx, probe, now.UTC(), source).Scan(&age, &depth); err != nil {
			t.Fatalf("per-source probe: %v", err)
		}
		samples = append(samples, float64(time.Since(start)))
	}
	return time.Duration(percentileSorted(samples, 0.5))
}

// timeQuerySingle reports the median wall time of n bounded single-source
// probe reads.
func timeQuerySingle(t *testing.T, ctx context.Context, sqlConn *sql.Conn, probe string, now time.Time, source string, n int) time.Duration {
	t.Helper()
	samples := make([]float64, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		var age float64
		var depth, pastEngage, pastRelease int64
		if err := sqlConn.QueryRowContext(ctx, probe, s0ProbeParams(now, source)...).Scan(&age, &depth, &pastEngage, &pastRelease); err != nil {
			t.Fatalf("time bounded probe: %v", err)
		}
		samples = append(samples, float64(time.Since(start)))
	}
	return time.Duration(percentileSorted(samples, 0.5))
}

// explainOne runs EXPLAIN (ANALYZE, BUFFERS) over one probe invocation.
func explainOne(t *testing.T, ctx context.Context, sqlConn *sql.Conn, probe string, now time.Time, args ...any) string {
	t.Helper()
	params := append([]any{now.UTC()}, args...)
	rows, err := sqlConn.QueryContext(ctx, "EXPLAIN (ANALYZE, TIMING OFF, COSTS OFF, BUFFERS) "+probe, params...)
	if err != nil {
		t.Fatalf("explain probe: %v", err)
	}
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			_ = rows.Close()
			t.Fatalf("scan probe explain: %v", err)
		}
		lines = append(lines, line)
	}
	_ = rows.Close()
	return strings.Join(lines, "\n")
}

// seedS010xSynthetic plants nine more replay mixes beside the seeded one,
// attributed across sources so the grouped signal fans out. Payload
// sources s0-source-0..s0-source-5 each carry ~1.5 replay mixes; the age
// tail matches the replay seed's oldest row.
func seedS010xSynthetic(ctx context.Context, conn reducerClaimBenchmarkConn, now time.Time, sources int) error {
	mix := replay10862Mix()
	oldest := time.Date(2026, time.October, 19, 5, 21, 48, 0, time.UTC)
	span := now.Sub(oldest)
	n := len(mix)
	for round := 1; round < 10; round++ {
		for start := 0; start < n; start += 500 {
			end := start + 500
			if end > n {
				end = n
			}
			values := make([]string, 0, end-start)
			for i := start; i < end; i++ {
				row := mix[i]
				created := oldest.Add(time.Duration(float64(round*n+i) / float64(10*n) * float64(span)))
				scopeOrdinal := ((round*n + i) % 600) + 1
				source := fmt.Sprintf("s0-source-%d", (round*n+i)%sources)
				visible := "NULL"
				nextAttempt := "NULL"
				failureClass := "NULL"
				if row.status == "retrying" {
					visible = "'" + created.Format(time.RFC3339) + "'"
					nextAttempt = "'" + created.Format(time.RFC3339) + "'"
					failureClass = "'reducer_retryable'"
				}
				ts := created.Format(time.RFC3339)
				payload := fmt.Sprintf("jsonb_build_object('entity_key','s0-entity','reason','10882-s0','fact_id','s0-fact-%d-%d','source_system','%s')",
					round, i, source)
				values = append(values, fmt.Sprintf(
					"('s0-work-%d-%d','scope-replay-%d','generation-replay-%d','reducer','%s','scope',NULL,'%s',%d,NULL,NULL,%s,NULL,%s,%s,NULL,NULL,%s,'%s','%s')",
					round, i, scopeOrdinal, scopeOrdinal, string(row.domain), row.status, row.attempts,
					visible, nextAttempt, failureClass, payload, ts, ts))
			}
			if _, err := conn.ExecContext(ctx, `INSERT INTO fact_work_items (
work_item_id, scope_id, generation_id, stage, domain, conflict_domain,
conflict_key, status, attempt_count, lease_owner, claim_until, visible_at,
last_attempt_at, next_attempt_at, failure_class, failure_message,
failure_details, payload, created_at, updated_at) VALUES `+strings.Join(values, ",")); err != nil {
				return fmt.Errorf("insert 10x work items [%d:%d]: %w", start, end, err)
			}
		}
	}
	return nil
}

// s0GroupedParams binds the shipped grouped signal: now plus the engage and
// release cutoffs the two aged-row COUNT FILTER columns compare against
// (#10882 F5), at the test config's ages.
func s0GroupedParams(now time.Time) []any {
	cfg := sourceBackpressureTestConfig()
	return []any{now.UTC(), now.UTC().Add(-cfg.EngageAge), now.UTC().Add(-cfg.ReleaseAge)}
}

// s0ProbeParams binds the shipped bounded probe: the grouped parameters plus
// the source system.
func s0ProbeParams(now time.Time, source string) []any {
	return append(s0GroupedParams(now), source)
}
