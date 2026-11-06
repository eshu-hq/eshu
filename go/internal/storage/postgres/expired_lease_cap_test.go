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

// TestWorkflowControlStoreReapExpiredClaimsDeadLettersPastCap proves the reap
// statement carries the bounded expired-lease cap (eshu-hq/eshu#10113): the
// query must branch to failed_terminal past the cap and the call must bind
// the cap and the named class. RED on today's code, which requeues with no
// cap.
func TestWorkflowControlStoreReapExpiredClaimsDeadLettersPastCap(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"claim-1",
				"item-1",
				sql.NullInt64{Int64: 3, Valid: true},
				"collector-pod-1",
				"expired",
				now.Add(-time.Minute),
				now.Add(-time.Second),
				now.Add(-time.Second),
				now.Add(-time.Minute),
				now,
			}}},
		},
	}
	store := NewWorkflowControlStore(db)

	if _, err := store.ReapExpiredClaims(context.Background(), now, 10, 0); err != nil {
		t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	query := db.queries[0].query
	for _, want := range []string{
		"failed_terminal",
		"attempt_count",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reap query missing %q (expired-lease cap branch): %s", want, query)
		}
	}
	args := db.queries[0].args
	if len(args) < 5 {
		t.Fatalf("reap args = %d, want at least 5 (asOf, limit, visible_at, cap, class)", len(args))
	}
	cap, ok := args[3].(int)
	if !ok {
		t.Fatalf("reap args[3] = %T, want int cap", args[3])
	}
	if cap != workflow.DefaultExpiredLeaseMaxRequeues() {
		t.Fatalf("reap cap = %d, want DefaultExpiredLeaseMaxRequeues() = %d", cap, workflow.DefaultExpiredLeaseMaxRequeues())
	}
	class, ok := args[4].(string)
	if !ok {
		t.Fatalf("reap args[4] = %T, want string class", args[4])
	}
	if class != workflow.FailureClassExpiredLeaseBudgetExhausted {
		t.Fatalf("reap class = %q, want %q", class, workflow.FailureClassExpiredLeaseBudgetExhausted)
	}
}

// TestWorkflowControlStoreIntegrationExpiredLeaseRequeueCapDeadLetters seeds a
// never-returning run (eshu-hq/eshu#10113): the worker claims, never returns a
// failure verdict, its lease expires, and the reaper requeues. Past the cap
// the item must dead-letter with the named class instead of requeueing, and a
// further claim must find no work. RED on today's code, which requeues
// forever.
func TestWorkflowControlStoreIntegrationExpiredLeaseRequeueCapDeadLetters(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.November, 6, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-expired-lease-cap",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-expired-lease-cap",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-expired-lease-cap",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	cap := workflow.DefaultExpiredLeaseMaxRequeues()
	claimAt := now
	var lastClaim workflow.Claim
	var lastItem workflow.WorkItem
	deadLettered := false
	// One extra lap past the cap proves the terminal state sticks: no further
	// claim is granted after the dead-letter reap.
	for i := 0; i < cap+2; i++ {
		item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			OwnerID:             "collector-pod-never-returns",
			ClaimID:             "claim-never-returns-" + string(rune('a'+i)),
		}, claimAt, time.Minute)
		if !found {
			if deadLettered {
				break
			}
			t.Fatalf("lap %d: ClaimNextEligible() found = false, want true (still within cap)", i)
		}
		if err != nil {
			t.Fatalf("lap %d: ClaimNextEligible() error = %v, want nil", i, err)
		}
		lastClaim, lastItem = claim, item
		// The run never returns: no heartbeat, no complete, no failure. Its
		// lease simply expires.
		markClaimExpired(t, db, claim.ClaimID, item.WorkItemID, claim.OwnerID, claim.FencingToken, claimAt.Add(-time.Second))
		reapAt := claimAt.Add(2 * time.Minute)
		if _, err := store.ReapExpiredClaims(ctx, reapAt, 10, 0); err != nil {
			t.Fatalf("lap %d: ReapExpiredClaims() error = %v, want nil", i, err)
		}
		status, class, attempt := workItemTerminalState(t, db, item.WorkItemID)
		if status == string(workflow.WorkItemStatusFailedTerminal) {
			deadLettered = true
			if class != workflow.FailureClassExpiredLeaseBudgetExhausted {
				t.Fatalf("lap %d: dead-letter class = %q, want %q", i, class, workflow.FailureClassExpiredLeaseBudgetExhausted)
			}
			if attempt < cap {
				t.Fatalf("lap %d: dead-letter attempt_count = %d, want >= cap %d", i, attempt, cap)
			}
			claimAt = reapAt.Add(DefaultWorkflowExpiredClaimRequeueDelay + time.Second)
			continue
		}
		if status != string(workflow.WorkItemStatusPending) {
			t.Fatalf("lap %d: work item status = %q, want pending (within cap) or failed_terminal (past cap)", i, status)
		}
		claimAt = reapAt.Add(DefaultWorkflowExpiredClaimRequeueDelay + time.Second)
	}
	if !deadLettered {
		t.Fatalf("never-returning run still pending after %d expiries (last item %+v, last claim %+v): want dead-letter with %q past cap %d",
			cap+2, lastItem, lastClaim, workflow.FailureClassExpiredLeaseBudgetExhausted, cap)
	}
	// The dead-lettered item must not be claimable again.
	if _, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-never-returns",
		ClaimID:             "claim-never-returns-after-dead-letter",
	}, claimAt, time.Minute); err != nil {
		t.Fatalf("ClaimNextEligible() after dead-letter error = %v, want nil", err)
	} else if found {
		t.Fatal("ClaimNextEligible() after dead-letter found = true, want false")
	}
}

func workItemTerminalState(t *testing.T, db *sql.DB, workItemID string) (status, class string, attempt int) {
	t.Helper()

	var gotStatus sql.NullString
	var gotClass sql.NullString
	var gotAttempt int
	if err := db.QueryRowContext(context.Background(), `
SELECT status, last_failure_class, attempt_count
FROM workflow_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&gotStatus, &gotClass, &gotAttempt); err != nil {
		t.Fatalf("query work item %q error = %v, want nil", workItemID, err)
	}
	if gotStatus.Valid {
		status = gotStatus.String
	}
	if gotClass.Valid {
		class = gotClass.String
	}
	return status, class, gotAttempt
}
