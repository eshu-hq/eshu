// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Fixture and assertions shared by the #10091 live proofs in
// run_reconciliation_terminal_siblings_live_test.go.

const terminalSiblingsLease = 5 * time.Minute

// terminalSiblingsRun is the fixture for one planner's view of a collector
// instance: every plan for the same instance and scopes targets the same work.
type terminalSiblingsRun struct {
	instanceID string
	scopes     []string
}

// plan builds the run and work items one planner pass would create. label moves
// run_id, work_item_id and generation_id only; every column the guard compares
// stays the same, so two plans are duplicates of one target set.
func (f terminalSiblingsRun) plan(label string, at time.Time) (workflow.Run, []workflow.WorkItem) {
	runID := f.instanceID + ":" + label
	run := workflow.Run{
		RunID:              runID,
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorGit),
		CreatedAt:          at,
		UpdatedAt:          at,
	}
	items := make([]workflow.WorkItem, 0, len(f.scopes))
	for _, scopeID := range f.scopes {
		generationID := runID + ":" + scopeID + ":generation"
		items = append(items, workflow.WorkItem{
			WorkItemID:          runID + ":" + scopeID,
			RunID:               runID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: f.instanceID,
			SourceSystem:        string(scope.CollectorGit),
			ScopeID:             scopeID,
			AcceptanceUnitID:    scopeID,
			SourceRunID:         generationID,
			GenerationID:        generationID,
			FairnessKey:         scopeID,
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           at,
			UpdatedAt:           at,
		})
	}
	return run, items
}

func (f terminalSiblingsRun) admit(
	t *testing.T,
	ctx context.Context,
	store *WorkflowControlStore,
	label string,
	at time.Time,
) workflow.RunAdmission {
	t.Helper()
	run, items := f.plan(label, at)
	admission, err := store.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets(%s) error = %v, want nil", label, err)
	}
	return admission
}

// claim takes the next eligible item for the instance. The fair-claim rotation
// picks the order, so callers act on the returned item, never on a fixed id.
func (f terminalSiblingsRun) claim(
	t *testing.T,
	ctx context.Context,
	store *WorkflowControlStore,
	at time.Time,
	claimID string,
) (workflow.WorkItem, workflow.Claim) {
	t.Helper()
	item, claim, found, err := store.ClaimNextEligible(ctx, f.selector(claimID), at, terminalSiblingsLease)
	if err != nil {
		t.Fatalf("ClaimNextEligible(%s) error = %v, want nil", claimID, err)
	}
	if !found {
		t.Fatalf("ClaimNextEligible(%s) found nothing, want a claimable sibling", claimID)
	}
	return item, claim
}

func (f terminalSiblingsRun) selector(claimID string) workflow.ClaimSelector {
	return workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: f.instanceID,
		OwnerID:             f.instanceID + "-owner",
		ClaimID:             claimID,
	}
}

func terminalSiblingsMutation(item workflow.WorkItem, claim workflow.Claim, at time.Time) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:   item.WorkItemID,
		ClaimID:      claim.ClaimID,
		FencingToken: claim.FencingToken,
		OwnerID:      claim.OwnerID,
		ObservedAt:   at,
	}
}

func terminalSiblingsFailure(mutation workflow.ClaimMutation) workflow.ClaimMutation {
	mutation.FailureClass = "collect_failure"
	mutation.FailureMessage = "synthetic terminal failure"
	mutation.FailureTerminal = true
	return mutation
}

func terminalSiblingsReconcile(t *testing.T, ctx context.Context, store *WorkflowControlStore, at time.Time) {
	t.Helper()
	if _, err := store.ReconcileWorkflowRuns(ctx, at); err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
}

// terminalSiblingsRunEnd returns the persisted finished_at, so a proof can tell
// an open run from a finished one without trusting the status column alone.
func terminalSiblingsRunEnd(t *testing.T, ctx context.Context, db *sql.DB, runID string) sql.NullTime {
	t.Helper()
	var finishedAt sql.NullTime
	if err := db.QueryRowContext(ctx,
		`SELECT finished_at FROM workflow_runs WHERE run_id = $1`, runID).Scan(&finishedAt); err != nil {
		t.Fatalf("read finished_at of %s: %v", runID, err)
	}
	return finishedAt
}

func terminalSiblingsOpenCount(t *testing.T, ctx context.Context, db *sql.DB, runID string) int {
	t.Helper()
	var open int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM workflow_work_items
WHERE run_id = $1 AND status IN ('pending', 'claimed')`, runID).Scan(&open); err != nil {
		t.Fatalf("count open items of %s: %v", runID, err)
	}
	return open
}

// terminalSiblingsDuplicateTargets counts targets holding more than one open
// item. It is the invariant the guard exists to keep: one target, one open item.
func terminalSiblingsDuplicateTargets(ctx context.Context, db *sql.DB, instanceID string) (int, error) {
	var duplicates int
	err := db.QueryRowContext(ctx, `
SELECT count(*) FROM (
    SELECT scope_id FROM workflow_work_items
    WHERE collector_instance_id = $1 AND status IN ('pending', 'claimed')
    GROUP BY scope_id
    HAVING count(*) > 1
) AS duplicated`, instanceID).Scan(&duplicates)
	return duplicates, err
}
