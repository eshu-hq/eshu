// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestWorkflowControlStoreIntegrationWithheldPartialLifecycle proves the
// fenced PostgreSQL claim mutation and status read keep a prior completion
// observable while retryable and terminal partial attempts are withheld.
func TestWorkflowControlStoreIntegrationWithheldPartialLifecycle(t *testing.T) {
	database, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second)
	instance := collector.InstanceSummary{
		CollectorKind: string(scope.CollectorGit), InstanceID: "partial-extension",
		Enabled: true, ExtensionHosted: true,
	}
	newItem := func(runID, itemID string, created time.Time) workflow.WorkItem {
		mustCreateRun(t, store, ctx, workflow.Run{
			RunID: runID, TriggerKind: workflow.TriggerKindBootstrap,
			Status: workflow.RunStatusCollectionPending, CreatedAt: created, UpdatedAt: created,
		})
		item := workflow.WorkItem{
			WorkItemID: itemID, RunID: runID, CollectorKind: scope.CollectorGit,
			CollectorInstanceID: instance.InstanceID, ScopeID: "scope-partial-extension",
			Status: workflow.WorkItemStatusPending, CreatedAt: created, UpdatedAt: created,
		}
		mustEnqueueWorkItem(t, store, ctx, item)
		return item
	}
	claimItem := func(at time.Time, claimID string) (workflow.WorkItem, workflow.Claim) {
		item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind: scope.CollectorGit, CollectorInstanceID: instance.InstanceID,
			OwnerID: "partial-extension-worker", ClaimID: claimID,
		}, at, time.Minute)
		if err != nil || !found {
			t.Fatalf("ClaimNextEligible(%q) = found %v, error %v", claimID, found, err)
		}
		return item, claim
	}
	readOutcome := func(wantUpdated, wantCommitted time.Time, warnings int) {
		rows, err := readWorkflowExtensionOutcomes(ctx, SQLDB{DB: database}, []collector.InstanceSummary{instance})
		if err != nil {
			t.Fatalf("readWorkflowExtensionOutcomes() error = %v", err)
		}
		if len(rows) != 1 || rows[0].ScopeCount != 1 || rows[0].PartialScopes != 1 ||
			rows[0].WarningCount != warnings || rows[0].InFlightScopes != 0 ||
			!rows[0].UpdatedAt.Equal(wantUpdated) || !rows[0].LastCommittedAt.Equal(wantCommitted) {
			t.Fatalf("extension outcomes = %+v, want one partial scope, %d warnings, status %s, committed %s",
				rows, warnings, wantUpdated, wantCommitted)
		}
	}
	readWorkItem := func(itemID string, wantStatus workflow.WorkItemStatus, wantWarnings int, wantVisible time.Time) {
		var status, resultState string
		var partial bool
		var warnings int
		var visible time.Time
		var completed sql.NullTime
		if err := database.QueryRowContext(ctx, `
SELECT status, extension_result_state, extension_partial, extension_warning_count,
       visible_at, last_completed_at
FROM workflow_work_items WHERE work_item_id = $1`, itemID).Scan(
			&status, &resultState, &partial, &warnings, &visible, &completed,
		); err != nil {
			t.Fatalf("read withheld work item %q: %v", itemID, err)
		}
		if status != string(wantStatus) || resultState != "partial_withheld" || !partial ||
			warnings != wantWarnings || !visible.Equal(wantVisible) || completed.Valid {
			t.Fatalf("work item %q = %q/%q/%v/%d visible %s completed %v; want %q/partial_withheld/true/%d visible %s, no completion",
				itemID, status, resultState, partial, warnings, visible, completed, wantStatus, wantWarnings, wantVisible)
		}
	}

	completedItem := newItem("partial-completed-run", "partial-completed-item", base)
	claimed, firstClaim := claimItem(base.Add(time.Second), "partial-completed-claim")
	if claimed.WorkItemID != completedItem.WorkItemID {
		t.Fatalf("completed claim item = %q, want %q", claimed.WorkItemID, completedItem.WorkItemID)
	}
	committedAt := base.Add(2 * time.Second)
	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID: claimed.WorkItemID, ClaimID: firstClaim.ClaimID,
		FencingToken: firstClaim.FencingToken, OwnerID: firstClaim.OwnerID,
		ObservedAt: committedAt, ExtensionOutcome: &workflow.ExtensionOutcome{State: "complete"},
	}); err != nil {
		t.Fatalf("CompleteClaim() error = %v", err)
	}
	mustClaimState(t, database, firstClaim.ClaimID, workflow.ClaimStatusCompleted, firstClaim.FencingToken)

	partialItem := newItem("partial-withheld-run", "partial-withheld-item", base.Add(3*time.Second))
	claimed, retryClaim := claimItem(base.Add(4*time.Second), "partial-retry-claim")
	if claimed.WorkItemID != partialItem.WorkItemID {
		t.Fatalf("retry claim item = %q, want %q", claimed.WorkItemID, partialItem.WorkItemID)
	}
	retryAt := base.Add(5 * time.Second)
	visibleAt := base.Add(10 * time.Second)
	retryMutation := workflow.ClaimMutation{
		WorkItemID: claimed.WorkItemID, ClaimID: retryClaim.ClaimID,
		FencingToken: retryClaim.FencingToken, OwnerID: retryClaim.OwnerID,
		ObservedAt: retryAt, VisibleAt: visibleAt,
		FailureClass:     "partial_snapshot_withheld",
		ExtensionOutcome: &workflow.ExtensionOutcome{State: "partial_withheld", Partial: true, WarningCount: 3},
	}
	if err := store.FailClaimRetryable(ctx, retryMutation); err != nil {
		t.Fatalf("FailClaimRetryable() error = %v", err)
	}
	mustClaimState(t, database, retryClaim.ClaimID, workflow.ClaimStatusFailedRetryable, retryClaim.FencingToken)
	mustWorkItemState(t, database, partialItem.WorkItemID, workflow.WorkItemStatusPending, "", retryClaim.FencingToken)
	readWorkItem(partialItem.WorkItemID, workflow.WorkItemStatusPending, 3, visibleAt)
	readOutcome(retryAt, committedAt, 3)
	if err := store.FailClaimRetryable(ctx, retryMutation); err != nil {
		t.Fatalf("identical retry replay error = %v, want idempotent acknowledgement", err)
	}
	readWorkItem(partialItem.WorkItemID, workflow.WorkItemStatusPending, 3, visibleAt)

	claimed, terminalClaim := claimItem(visibleAt.Add(time.Second), "partial-terminal-claim")
	if claimed.WorkItemID != partialItem.WorkItemID || terminalClaim.FencingToken != retryClaim.FencingToken+1 {
		t.Fatalf("reclaimed item/fence = %q/%d, want %q/%d", claimed.WorkItemID,
			terminalClaim.FencingToken, partialItem.WorkItemID, retryClaim.FencingToken+1)
	}
	if err := store.FailClaimRetryable(ctx, retryMutation); !errors.Is(err, workflow.ErrClaimRejected) {
		t.Fatalf("superseded retry mutation error = %v, want ErrClaimRejected", err)
	}
	mustWorkItemState(t, database, partialItem.WorkItemID, workflow.WorkItemStatusClaimed,
		terminalClaim.ClaimID, terminalClaim.FencingToken)
	terminalAt := visibleAt.Add(2 * time.Second)
	terminalMutation := workflow.ClaimMutation{
		WorkItemID: claimed.WorkItemID, ClaimID: terminalClaim.ClaimID,
		FencingToken: terminalClaim.FencingToken, OwnerID: terminalClaim.OwnerID,
		ObservedAt: terminalAt, FailureClass: "partial_snapshot_withheld", FailureTerminal: true,
		ExtensionOutcome: &workflow.ExtensionOutcome{State: "partial_withheld", Partial: true, WarningCount: 5},
	}
	if err := store.FailClaimTerminal(ctx, terminalMutation); err != nil {
		t.Fatalf("FailClaimTerminal() error = %v", err)
	}
	mustClaimState(t, database, terminalClaim.ClaimID, workflow.ClaimStatusFailedTerminal, terminalClaim.FencingToken)
	mustWorkItemState(t, database, partialItem.WorkItemID, workflow.WorkItemStatusFailedTerminal, "", terminalClaim.FencingToken)
	readWorkItem(partialItem.WorkItemID, workflow.WorkItemStatusFailedTerminal, 5, visibleAt)
	readOutcome(terminalAt, committedAt, 5)
	if err := store.FailClaimTerminal(ctx, terminalMutation); err != nil {
		t.Fatalf("identical terminal replay error = %v, want idempotent acknowledgement", err)
	}
	staleTerminalMutation := terminalMutation
	staleTerminalMutation.FencingToken = retryClaim.FencingToken
	if err := store.FailClaimTerminal(ctx, staleTerminalMutation); !errors.Is(err, workflow.ErrClaimRejected) {
		t.Fatalf("stale terminal fence error = %v, want ErrClaimRejected", err)
	}
	readWorkItem(partialItem.WorkItemID, workflow.WorkItemStatusFailedTerminal, 5, visibleAt)
}
