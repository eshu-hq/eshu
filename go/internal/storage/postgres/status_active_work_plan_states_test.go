// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// summaryPlanGateStates are the two #4446 fixture states the summary guard
// runs in (#7009 S5 ruling D5.5). The fixture as seeded is about 78% live
// (detail branch); rewriting its unleased live rows to succeeded leaves the
// 10% claimed and running rows (grouped branch). Each state is reached by
// seeding the live share and analyzing, never by editing the SQL.
var summaryPlanGateStates = []struct {
	mode string
	seed string
}{
	{mode: activeWorkModeDetail},
	{
		mode: activeWorkModeGrouped,
		seed: "UPDATE fact_work_items SET status = 'succeeded' WHERE status IN ('pending', 'retrying', 'failed'); ANALYZE fact_work_items",
	},
}

// summaryGenericStatement names the prepared summary statement of the
// generic-plan check.
const summaryGenericStatement = "eshu_7009_active_work_summary"

// checkSummaryPlanInGateStates runs the single-pass guard on the summary in
// both gate states, then on a forced generic plan of the prepared statement
// after six executions. In every plan the gate CTE must run exactly once
// (loops=1), the per-execution InitPlan the S5 shim measured, and rules
// R1-R3 of checkSummaryGenerationScans must hold.
func checkSummaryPlanInGateStates(ctx context.Context, t *testing.T, conn db.Executor, asOf time.Time) {
	t.Helper()
	queryer, ok := conn.(db.Queryer)
	if !ok {
		t.Fatal("proof connection does not support QueryContext")
	}
	literal := "'" + asOf.UTC().Format(time.RFC3339) + "'::timestamptz"
	for _, state := range summaryPlanGateStates {
		if state.seed != "" {
			if _, err := conn.ExecContext(ctx, state.seed); err != nil {
				t.Fatalf("seed %s state: %v", state.mode, err)
			}
		}
		if got := summaryModeOf(ctx, t, queryer, activeWorkSummaryQuery, asOf); got != state.mode {
			t.Fatalf("summary gate took %s, want %s in this state", got, state.mode)
		}
		plan, err := statusActiveGenerationExplainAnalyze(ctx, conn, activeWorkSummaryQuery, asOf)
		if err != nil {
			t.Fatalf("explain analyze summary (%s): %v", state.mode, err)
		}
		checkSummaryPlan(t, state.mode, plan)
	}

	if _, err := conn.ExecContext(ctx, "PREPARE "+summaryGenericStatement+"(timestamptz) AS "+activeWorkSummaryQuery); err != nil {
		t.Fatalf("prepare summary: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "DEALLOCATE "+summaryGenericStatement) }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force generic plan: %v", err)
	}
	defer func() { _, _ = conn.ExecContext(context.Background(), "RESET plan_cache_mode") }()
	execute := "EXECUTE " + summaryGenericStatement + "(" + literal + ")"
	for i := 0; i < 6; i++ {
		if got := summaryModeOf(ctx, t, queryer, execute); got != activeWorkModeGrouped {
			t.Fatalf("generic execution %d took %s, want grouped", i+1, got)
		}
	}
	plan, err := statusActiveGenerationExplainAnalyze(ctx, conn, execute)
	if err != nil {
		t.Fatalf("explain analyze generic summary: %v", err)
	}
	if !strings.Contains(plan, "$1") {
		t.Fatalf("plan_cache_mode=force_generic_plan produced a custom plan (no $1):\n%s", plan)
	}
	checkSummaryPlan(t, "generic", plan)
}

// checkSummaryPlan requires actuals, a gate CTE that ran once, and the
// single-pass rules.
func checkSummaryPlan(t *testing.T, name, plan string) {
	t.Helper()
	if !strings.Contains(plan, "actual time") {
		t.Fatalf("%s summary plan has no actuals, so the loops rules cannot run:\n%s", name, plan)
	}
	if err := checkSummaryGateRunsOnce(plan); err != nil {
		t.Fatalf("%s summary plan: %v\n%s", name, err, plan)
	}
	if err := checkSummaryGenerationScans(plan); err != nil {
		t.Fatalf("%s summary plan: %v\nplan:\n%s", name, err, plan)
	}
	t.Logf("%s summary plan:\n%s", name, plan)
}

// checkSummaryGateRunsOnce requires the fact_work_summary_mode CTE's node to
// report loops=1: the gate is evaluated once per execution, never per row.
func checkSummaryGateRunsOnce(plan string) error {
	lines := strings.Split(plan, "\n")
	lo, hi, ok := explainCTERange(lines, "fact_work_summary_mode")
	if !ok || hi <= lo+1 {
		return fmt.Errorf("plan has no fact_work_summary_mode CTE")
	}
	if loops, ok := explainLoops(lines[lo+1]); !ok || loops != 1 {
		return fmt.Errorf("gate CTE node %q: want actual loops=1", strings.TrimSpace(lines[lo+1]))
	}
	return nil
}

// summaryModeOf runs a summary statement and returns its mode row's branch.
func summaryModeOf(ctx context.Context, t *testing.T, queryer db.Queryer, query string, args ...any) string {
	t.Helper()
	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		t.Fatalf("run summary: %v", err)
	}
	defer func() { _ = rows.Close() }()
	mode := ""
	for rows.Next() {
		var section, raw string
		var ordinal int64
		if err := rows.Scan(&section, &ordinal, &raw); err != nil {
			t.Fatalf("scan summary row: %v", err)
		}
		if section != activeWorkSectionMode {
			continue
		}
		var row struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal([]byte(raw), &row); err != nil {
			t.Fatalf("decode mode row %s: %v", raw, err)
		}
		mode = row.Mode
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("summary rows: %v", err)
	}
	return mode
}
