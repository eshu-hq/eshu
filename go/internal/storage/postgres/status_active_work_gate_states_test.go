// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// historyBusyLiveShare is the busy shape's live-row predicate: 20% of rows
// (n % 10 IN (0, 1)) take one of the six detail statuses.
const historyBusyLiveShare = "CASE WHEN n % 10 IN (0, 1)\n"

// historyLiveShareSQL is historyBusyWorkSQL with perMille of every 1,000 work
// rows in a detail status instead of 20%; it ends with ANALYZE, so the gate
// reads statistics of this state.
func historyLiveShareSQL(t *testing.T, perMille int) string {
	t.Helper()
	seed, err := replaceOnce(historyBusyWorkSQL, historyBusyLiveShare, "CASE WHEN n % 1000 < "+strconv.Itoa(perMille)+"\n")
	if err != nil {
		t.Fatalf("live share seed: %v", err)
	}
	return seed
}

// historyStaleVMSQL turns 80% of an all-history busy state into failed,
// retrying, pending, and dead-letter rows after the ANALYZE, with
// autovacuum off on the fixture table: the gate keeps the pre-update
// statistics (near 0% live) while 80% of rows are live, the S5 C80s
// stats-window state in which the gate takes the wrong branch.
const historyStaleVMSQL = `ALTER TABLE fact_work_items SET (autovacuum_enabled = false);
UPDATE fact_work_items
SET status = (ARRAY['pending', 'retrying', 'failed', 'dead_letter'])[1 + (substr(work_item_id, 4)::int / 10) % 4]
WHERE substr(work_item_id, 4)::int % 10 < 8`

// historyLiveShareCases are the S5 cell states the Go fixture can build: low,
// half, high, and full live share, each analyzed, and the stale-statistics
// 80% state. Every case runs the shipped gate and both forced branches.
func historyLiveShareCases() []historyCase {
	share := func(name string, perMille int, mode string) historyCase {
		return historyCase{
			name: name,
			asOf: historyEdgeAsOf,
			seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
				historyExec(ctx, t, conn, historyLiveShareSQL(t, perMille))
			},
			expect:   []historyExpectation{{"queue", 1, "total_count", "3000"}},
			wantMode: mode,
			analyzed: true,
		}
	}
	return []historyCase{
		share("live_0_1pct", 1, activeWorkModeGrouped),
		share("live_50pct", 500, activeWorkModeDetail),
		share("live_80pct", 800, activeWorkModeDetail),
		share("live_100pct", 1000, activeWorkModeDetail),
		{
			name: "stale_stats_80pct_live",
			asOf: historyEdgeAsOf,
			seed: func(ctx context.Context, t *testing.T, conn *sql.Conn) {
				historyExec(ctx, t, conn, historyLiveShareSQL(t, 0))
				historyExec(ctx, t, conn, historyStaleVMSQL)
			},
			expect:   []historyExpectation{{"queue", 1, "total_count", "3000"}},
			wantMode: activeWorkModeGrouped,
		},
	}
}

// readSummaryMode runs a summary rendering and returns its mode row.
func readSummaryMode(ctx context.Context, t *testing.T, conn *sql.Conn, query string, asOf time.Time) (string, float64) {
	t.Helper()
	var raw string
	if err := conn.QueryRowContext(ctx, `SELECT section_json FROM (`+query+`) AS summary WHERE section = $2`,
		asOf, activeWorkSectionMode).Scan(&raw); err != nil {
		t.Fatalf("read mode row: %v", err)
	}
	var row struct {
		Mode     string  `json:"mode"`
		Estimate float64 `json:"estimate"`
	}
	if err := json.Unmarshal([]byte(raw), &row); err != nil {
		t.Fatalf("decode mode row %s: %v", raw, err)
	}
	return row.Mode, row.Estimate
}

// trueLiveFraction is the share of fact_work_items rows in a detail status.
func trueLiveFraction(ctx context.Context, t *testing.T, conn *sql.Conn) float64 {
	t.Helper()
	var fraction float64
	if err := conn.QueryRowContext(ctx, `SELECT COALESCE(COUNT(*) FILTER (WHERE status IN `+activeWorkDetailStatuses+`)::float8
  / NULLIF(COUNT(*), 0), 0) FROM fact_work_items`).Scan(&fraction); err != nil {
		t.Fatalf("true live fraction: %v", err)
	}
	return fraction
}

// checkGateBranches requires the shipped gate and both forced branches to
// return the oracle's section rows on the seeded state (S5 mutation a: the
// wrong branch is still correct), each forced render to report its branch,
// the shipped render to report the branch its estimate selects, and, on an
// analyzed state, that estimate to track the true live fraction.
func checkGateBranches(ctx context.Context, t *testing.T, conn *sql.Conn, tc historyCase) {
	t.Helper()
	threshold, err := strconv.ParseFloat(activeWorkGroupedThreshold, 64)
	if err != nil {
		t.Fatalf("threshold: %v", err)
	}
	for _, render := range []struct{ name, force, want string }{
		{"gate", "", tc.wantMode},
		{"forced grouped", activeWorkForceGrouped, activeWorkModeGrouped},
		{"forced detail", activeWorkForceDetail, activeWorkModeDetail},
	} {
		query := activeWorkSummaryQuery
		if render.force != "" {
			if query, err = activeWorkSummaryForcedGate(query, render.force); err != nil {
				t.Fatalf("%s: %v", render.name, err)
			}
		}
		diff := diffAgainstOracle(ctx, t, conn, query, tc.asOf)
		if !diff.equal() || diff.oracleRows == 0 {
			t.Fatalf("%s render differs from the pre-change oracle: %+v", render.name, diff)
		}
		mode, estimate := readSummaryMode(ctx, t, conn, query, tc.asOf)
		if mode != activeWorkModeGrouped && mode != activeWorkModeDetail {
			t.Fatalf("%s render mode = %q, want grouped or detail", render.name, mode)
		}
		if render.force == "" {
			if (mode == activeWorkModeGrouped) != (estimate < threshold) {
				t.Fatalf("gate mode %s disagrees with its estimate %v at threshold %v", mode, estimate, threshold)
			}
			if tc.analyzed {
				if live := trueLiveFraction(ctx, t, conn); math.Abs(live-estimate) > 0.02 {
					t.Fatalf("gate estimate %v, true live fraction %v: off by more than 0.02", estimate, live)
				}
			}
		}
		if render.want != "" && mode != render.want {
			t.Fatalf("%s render mode = %s (estimate %v), want %s", render.name, mode, estimate, render.want)
		}
		t.Logf("%s: %d equal section rows, mode %s, estimate %v", render.name, diff.oracleRows, mode, estimate)
	}
}

// gateRegclassControl replaces the regclass schema lookup with
// current_schema(), the S4 gate the S5 ruling rejected (D2.4).
func gateRegclassControl(t *testing.T) string {
	t.Helper()
	at := strings.Index(activeWorkLiveFractionEstimate, "s.schemaname = (")
	end := strings.Index(activeWorkLiveFractionEstimate, "::regclass)")
	if at < 0 || end < at {
		t.Fatalf("gate estimate has no regclass schema lookup:\n%s", activeWorkLiveFractionEstimate)
	}
	return activeWorkLiveFractionEstimate[:at] + "s.schemaname = current_schema()" + activeWorkLiveFractionEstimate[end+len("::regclass)"):]
}
