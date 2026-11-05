// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Hermetic companion to workflow_run_reconciliation_factless_live_test.go:
// the factless probe must keep both NOT EXISTS subqueries on the
// (scope_id, generation_id) equality prefix so the planner can use
// fact_records_scope_generation_idx and
// fact_work_items_scope_generation_idx instead of scanning either table.
// A probe that drops a scope equality silently degrades reconcile into a
// full scan on every run tick. The probe must also stay inside the progress
// statement itself: as a separate READ COMMITTED statement it counted
// completions that landed after the progress counts were read, breaking the
// factless <= completed invariant under concurrent completion (#10091).
func TestFactlessProbeUsesScopeGenerationPrefixes(t *testing.T) {
	t.Parallel()

	query := listWorkflowCollectorProgressQuery
	for _, want := range []string{
		"FROM workflow_work_items AS item",
		"item.status = 'completed'",
		"AS factless_completed_items",
		"FROM fact_records AS fact",
		"fact.scope_id = item.scope_id",
		"fact.generation_id = item.generation_id",
		"FROM fact_work_items AS work",
		"work.scope_id = item.scope_id",
		"work.generation_id = item.generation_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("factless probe query missing %q", want)
		}
	}
}

// TestReconcileFactlessCountSharesProgressSnapshot pins the fix for the
// #10091 live failure. The reconciler reads under READ COMMITTED, so every
// statement sees a new snapshot. When the factless count came from its own
// statement after the progress counts, a worker completing another factless
// item in between made factless (2) exceed completed (1), and
// ReconcileRunProgress validation failed the whole tick with "factless
// completed items must not exceed completed work items" — deterministically,
// whenever collectors finished work while a coordinator reconciled. The
// factless count must arrive in the same row as the completed count it
// subsets, from one statement. This fixture queues exactly one progress row
// carrying both counts and no response for a separate factless statement.
func TestReconcileFactlessCountSharesProgressSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 5, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"run-skew",
					string(workflow.TriggerKindSchedule),
					string(workflow.RunStatusCollectionActive),
					"[]",
					sql.NullString{String: string(scope.CollectorAWS), Valid: true},
					now.Add(-time.Minute),
					now.Add(-time.Minute),
					sql.NullTime{},
				}},
			},
			// Progress snapshot from one statement: 3 total, 1 pending,
			// 0 claimed, 1 completed, 1 failed terminal, 1 factless.
			{rows: [][]any{{string(scope.CollectorAWS), 3, 1, 0, 1, 1, 1}}},
			// No published phases.
			{rows: nil},
			// No terminal reducer dead-letters.
			{rows: nil},
		},
	}
	store := NewWorkflowControlStore(db)

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil: a completion landing between snapshot statements must not fail the tick", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	// List, progress, phase counts, dead-letter counts: no fifth statement
	// may reintroduce a separate factless snapshot.
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d (factless must share the progress statement)", got, want)
	}
	if len(db.execs) == 0 || !strings.Contains(db.execs[0].query, "UPDATE workflow_runs") {
		t.Fatalf("first exec = %+v, want the workflow_runs status update", db.execs)
	}
	// Terminal item plus a pending sibling: the run stays open (#10091).
	if got, want := db.execs[0].args[1], string(workflow.RunStatusCollectionActive); got != want {
		t.Fatalf("written run status = %v, want %q", got, want)
	}
}
