package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreReleaseClaimUsesFencingAndOwnerGuards(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)

	err := store.ReleaseClaim(context.Background(), ClaimMutation{
		WorkItemID:   "item-1",
		ClaimID:      "claim-1",
		FencingToken: 2,
		OwnerID:      "collector-pod-1",
		ObservedAt:   now,
	})
	if err != nil {
		t.Fatalf("ReleaseClaim() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	query := db.execs[0].query
	for _, want := range []string{
		"UPDATE workflow_claims",
		"status = 'released'",
		"fencing_token = $3",
		"owner_id = $4",
		"SET status = 'pending'",
		"last_failure_class = NULL",
		"last_failure_message = NULL",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("release query missing %q: %s", want, query)
		}
	}
}

func TestWorkflowControlStoreReleaseClaimRejectsStaleClaim(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}}}
	store := NewWorkflowControlStore(db)

	err := store.ReleaseClaim(context.Background(), ClaimMutation{
		WorkItemID:   "item-1",
		ClaimID:      "claim-1",
		FencingToken: 2,
		OwnerID:      "collector-pod-1",
		ObservedAt:   time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("ReleaseClaim() error = nil, want non-nil")
	}
	if err != ErrWorkflowClaimRejected {
		t.Fatalf("ReleaseClaim() error = %v, want ErrWorkflowClaimRejected", err)
	}
}

func TestWorkflowControlStoreReleaseClaimReturnsWorkToPendingWithoutFailure(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 19, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-release",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-release",
		RunID:               run.RunID,
		CollectorKind:       "git",
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-release",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       "git",
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-release",
		ClaimID:             "claim-release",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}

	releaseAt := now.Add(30 * time.Second)
	if err := store.ReleaseClaim(ctx, workflow.ClaimMutation{
		WorkItemID:   item.WorkItemID,
		ClaimID:      claim.ClaimID,
		FencingToken: claim.FencingToken,
		OwnerID:      claim.OwnerID,
		ObservedAt:   releaseAt,
	}); err != nil {
		t.Fatalf("ReleaseClaim() error = %v, want nil", err)
	}

	mustClaimState(t, db, claim.ClaimID, workflow.ClaimStatusReleased, claim.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusPending, "", claim.FencingToken)
	mustWorkItemHasNoFailure(t, db, item.WorkItemID)
}

func mustWorkItemHasNoFailure(t *testing.T, db *sql.DB, workItemID string) {
	t.Helper()

	var failureClass sql.NullString
	var failureMessage sql.NullString
	if err := db.QueryRowContext(context.Background(), `
SELECT last_failure_class, last_failure_message
FROM workflow_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&failureClass, &failureMessage); err != nil {
		t.Fatalf("query work item %q failure metadata error = %v, want nil", workItemID, err)
	}
	if failureClass.Valid || failureMessage.Valid {
		t.Fatalf("work item %q failure metadata = (%q, %q), want nulls", workItemID, failureClass.String, failureMessage.String)
	}
}
