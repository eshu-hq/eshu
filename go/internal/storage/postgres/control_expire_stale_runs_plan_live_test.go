// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10802 real PostgreSQL plan proof for the run-age bound expiry statement: the per-tick candidate walk over a backlog-shaped fixture (many fresh-run pending items plus stale runs) serves the work-item backlog from the open-items updated index with no sequential scan of that table; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func expireStaleRunsPlanRunID(prefix string, n int) string {
	return fmt.Sprintf("expire-plan-%s-run-%02d", prefix, n)
}

func expireStaleRunsPlanItemID(prefix string, run, n int) string {
	return fmt.Sprintf("expire-plan-%s-item-%02d-%04d", prefix, run, n)
}

// TestExpireStaleOpenRunsPlanStaysIndexServed proves #10802: the per-tick
// run-age bound expiry statement plans its candidate walk without a
// sequential scan of either backlog table on a backlog-shaped fixture (many
// fresh-run pending items plus stale runs). The EXPLAINed text IS the shipped
// expireStaleOpenRunsQuery constant with all six parameters bound, so any
// production edit to the statement changes what is planned here instead of
// silently proving a stale copy.
func TestExpireStaleOpenRunsPlanStaysIndexServed(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	cutoff := now.Add(-24 * time.Hour)
	instance := expireStaleRunsLiveInstance(t)

	// Backlog shape: two hundred fresh runs, each holding one thousand
	// pending items (a 200k-row retention-scale pile, as in the #7929 plan
	// proof: at a few thousand rows the planner legitimately prefers
	// bitmaps), plus five stale runs with twenty items each. Items go in
	// through the bulk enqueue path, not one INSERT per row. The fresh pile
	// is what a sequential scan would have to wade through every tick.
	var items []workflow.WorkItem
	addItems := func(prefix string, run, count int, created time.Time) {
		runID := expireStaleRunsPlanRunID(prefix, run)
		seedExpireStaleRun(t, ctx, database, runID, "collection_active", created)
		for i := 0; i < count; i++ {
			id := expireStaleRunsPlanItemID(prefix, run, i)
			items = append(items, workflow.WorkItem{
				WorkItemID:          id,
				RunID:               runID,
				CollectorKind:       expireStaleRunsLiveKind,
				CollectorInstanceID: instance,
				SourceSystem:        string(expireStaleRunsLiveKind),
				ScopeID:             "plan-scope",
				AcceptanceUnitID:    "unit-plan-scope",
				SourceRunID:         "gen-" + id,
				GenerationID:        "gen-" + id,
				Status:              workflow.WorkItemStatusPending,
				AttemptCount:        1,
				CreatedAt:           created,
				UpdatedAt:           created,
			})
		}
	}
	for r := 0; r < 200; r++ {
		addItems("fresh", r, 1000, now.Add(-time.Hour))
	}
	for r := 0; r < 5; r++ {
		addItems("stale", r, 20, now.Add(-25*time.Hour))
	}
	if err := store.EnqueueWorkItems(ctx, items); err != nil {
		t.Fatalf("EnqueueWorkItems(%d items): %v", len(items), err)
	}
	// Bulk seeding leaves the tables without planner statistics, and
	// without them the planner prefers scans even with the indexes present.
	// Production tables carry autovacuum statistics, so ANALYZE here proves
	// the steady-state plan, not the unanalyzed transient.
	for _, table := range []string{"workflow_work_items", "workflow_runs", "workflow_claims"} {
		if _, err := database.ExecContext(ctx, `ANALYZE `+table); err != nil {
			t.Fatalf("ANALYZE %s: %v", table, err)
		}
	}

	rows, err := database.QueryContext(
		ctx,
		"EXPLAIN (COSTS OFF) "+expireStaleOpenRunsQuery,
		cutoff,
		100,
		now,
		"run_age_bound_exceeded",
		"run-age bound expired the work item",
		"run_age_expire",
	)
	if err != nil {
		t.Fatalf("EXPLAIN run-age expiry: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan EXPLAIN row: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read EXPLAIN rows: %v", err)
	}
	t.Logf("expiry candidate plan:\n%s", plan.String())
	// The work-item backlog is the table that scales: the candidate walk
	// must serve it from the open-items updated index and stop at the
	// LIMIT, never scan it. The runs and claims tables stay small —
	// non-terminal runs are bounded by the #8829 retention window and
	// claims only cover the selected items — so sequential scans there are
	// the planner's legitimate small-table choice, not a scaling hazard.
	if got := plan.String(); strings.Contains(got, "Seq Scan on workflow_work_items") {
		t.Fatalf("expiry candidate plan sequentially scans the work-item backlog:\n%s", got)
	}
	if got := plan.String(); !strings.Contains(got, "Index Scan using workflow_items_open_updated_idx") {
		t.Fatalf("expiry candidate plan does not use the open-items updated index:\n%s", got)
	}
}
