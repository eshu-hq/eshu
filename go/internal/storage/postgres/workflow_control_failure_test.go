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

func TestWorkflowControlStoreFailClaimTerminalUsesDensePostgresParameters(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.May, 25, 14, 0, 0, 0, time.UTC)

	err := store.FailClaimTerminal(context.Background(), workflow.ClaimMutation{
		WorkItemID:     "security-alert-item-1",
		ClaimID:        "security-alert-claim-1",
		FencingToken:   2,
		OwnerID:        "collector-security-alerts-1",
		ObservedAt:     now,
		FailureClass:   "provider_config_invalid",
		FailureMessage: "configured source cannot be collected",
	})
	if err != nil {
		t.Fatalf("FailClaimTerminal() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), 7; got != want {
		t.Fatalf("terminal mutation arg count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"status = 'failed_terminal'",
		"item.current_fencing_token = $2",
		"item.current_owner_id = $3",
		"item.current_claim_id = $4",
		"item.work_item_id = $5",
		"failure_class = NULLIF($6, '')",
		"failure_message = NULLIF($7, '')",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("terminal claim query missing %q:\n%s", want, query)
		}
	}
	if strings.Contains(query, "$8") {
		t.Fatalf("terminal claim query leaves an unused parameter hole:\n%s", query)
	}
}

func TestWorkflowControlStoreIntegrationFailClaimTerminalRecordsFailureWithoutParameterHole(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.May, 25, 14, 10, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-terminal-security-alert",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-terminal-security-alert",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorSecurityAlert,
		CollectorInstanceID: "collector-security-alerts-default",
		ScopeID:             "security-alert:repository:fixture",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorSecurityAlert,
		CollectorInstanceID: "collector-security-alerts-default",
		OwnerID:             "collector-security-alerts-pod",
		ClaimID:             "claim-terminal-security-alert",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}

	failureClass := "provider_config_invalid"
	failureMessage := "configured source cannot be collected"
	if err := store.FailClaimTerminal(ctx, workflow.ClaimMutation{
		WorkItemID:     item.WorkItemID,
		ClaimID:        claim.ClaimID,
		FencingToken:   claim.FencingToken,
		OwnerID:        claim.OwnerID,
		ObservedAt:     now.Add(30 * time.Second),
		FailureClass:   failureClass,
		FailureMessage: failureMessage,
	}); err != nil {
		t.Fatalf("FailClaimTerminal() error = %v, want nil", err)
	}

	mustClaimState(t, db, claim.ClaimID, workflow.ClaimStatusFailedTerminal, claim.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusFailedTerminal, "", claim.FencingToken)
	mustWorkflowFailure(t, db, item.WorkItemID, claim.ClaimID, failureClass, failureMessage)
}

func mustWorkflowFailure(t *testing.T, db *sql.DB, workItemID string, claimID string, wantClass string, wantMessage string) {
	t.Helper()

	var itemClass sql.NullString
	var itemMessage sql.NullString
	if err := db.QueryRowContext(context.Background(), `
SELECT last_failure_class, last_failure_message
FROM workflow_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&itemClass, &itemMessage); err != nil {
		t.Fatalf("query workflow work item %q failure metadata error = %v, want nil", workItemID, err)
	}
	if !itemClass.Valid || itemClass.String != wantClass {
		t.Fatalf("work item %q failure_class = %q valid=%v, want %q", workItemID, itemClass.String, itemClass.Valid, wantClass)
	}
	if !itemMessage.Valid || itemMessage.String != wantMessage {
		t.Fatalf("work item %q failure_message = %q valid=%v, want %q", workItemID, itemMessage.String, itemMessage.Valid, wantMessage)
	}

	var claimClass sql.NullString
	var claimMessage sql.NullString
	if err := db.QueryRowContext(context.Background(), `
SELECT failure_class, failure_message
FROM workflow_claims
WHERE claim_id = $1
`, claimID).Scan(&claimClass, &claimMessage); err != nil {
		t.Fatalf("query workflow claim %q failure metadata error = %v, want nil", claimID, err)
	}
	if !claimClass.Valid || claimClass.String != wantClass {
		t.Fatalf("claim %q failure_class = %q valid=%v, want %q", claimID, claimClass.String, claimClass.Valid, wantClass)
	}
	if !claimMessage.Valid || claimMessage.String != wantMessage {
		t.Fatalf("claim %q failure_message = %q valid=%v, want %q", claimID, claimMessage.String, claimMessage.Valid, wantMessage)
	}
}
