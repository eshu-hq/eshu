// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

// Q1 replay-ack alignment (#9428, ruling 6476206689 SATELLITE HOLDS): an
// identical replay of an already-applied terminal mutation (matching
// claim+fencing+owner, unmoved item fencing) returns nil instead of
// ErrWorkflowClaimRejected. Heartbeat never acks post-terminal. Everything
// else stays rejected.
//
// These unit tests pin the classification contract against the scripted fake:
// zero-row mutation, then PK reads of workflow_claims (status/fencing/owner)
// and workflow_work_items (status, NULL-safe current_fencing_token).

func replayAckMutation(op string) ClaimMutation {
	mutation := ClaimMutation{
		WorkItemID:    "item-1",
		ClaimID:       "claim-1",
		FencingToken:  7,
		OwnerID:       "collector-pod-1",
		ObservedAt:    time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
		LeaseDuration: time.Minute,
		VisibleAt:     time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC),
	}
	if strings.HasPrefix(op, "fail") {
		mutation.FailureClass = "collect_failure"
		mutation.FailureMessage = "synthetic probe failure"
	}
	return mutation
}

// replayAckStore stages a zero-row mutation followed by one claim PK row and
// one work-item PK row, the exact read shape the classifier uses.
func replayAckStore(claimStatus string, claimFence sql.NullInt64, itemStatus string, itemFence sql.NullInt64) (*WorkflowControlStore, *fakeExecQueryer) {
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}},
		queryResponses: []queueFakeRows{
			{rows: [][]any{{claimStatus, claimFence, "collector-pod-1"}}},
			{rows: [][]any{{itemStatus, itemFence}}},
		},
	}
	return NewWorkflowControlStore(db), db
}

func replayAckApply(t *testing.T, store *WorkflowControlStore, op string, mutation ClaimMutation) error {
	t.Helper()
	switch op {
	case "complete":
		return store.CompleteClaim(context.Background(), mutation)
	case "release":
		return store.ReleaseClaim(context.Background(), mutation)
	case "fail-retryable":
		return store.FailClaimRetryable(context.Background(), mutation)
	case "fail-terminal":
		return store.FailClaimTerminal(context.Background(), mutation)
	case "heartbeat":
		return store.HeartbeatClaim(context.Background(), mutation)
	default:
		t.Fatalf("unknown op %q", op)
		return nil
	}
}

func TestWorkflowControlStoreReplayAckIdenticalTerminalReplays(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		op          string
		claimStatus string
		itemStatus  string
	}{
		{name: "complete", op: "complete", claimStatus: "completed", itemStatus: "completed"},
		{name: "release", op: "release", claimStatus: "released", itemStatus: "pending"},
		{name: "fail-retryable", op: "fail-retryable", claimStatus: "failed_retryable", itemStatus: "pending"},
		{name: "fail-terminal", op: "fail-terminal", claimStatus: "failed_terminal", itemStatus: "failed_terminal"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fence := sql.NullInt64{Int64: 7, Valid: true}
			store, db := replayAckStore(tc.claimStatus, fence, tc.itemStatus, fence)
			if err := replayAckApply(t, store, tc.op, replayAckMutation(tc.op)); err != nil {
				t.Fatalf("%s replay error = %v, want nil", tc.op, err)
			}
			if got, want := len(db.execs), 1; got != want {
				t.Fatalf("exec count = %d, want %d", got, want)
			}
			if got, want := len(db.queries), 2; got != want {
				t.Fatalf("classification query count = %d, want %d", got, want)
			}
			for _, q := range db.queries {
				if strings.Contains(q.query, "tenant_scope_grants") {
					t.Fatalf("classification read must bypass the tenant-grant join: %s", q.query)
				}
			}
		})
	}
}

func TestWorkflowControlStoreReplayAckHeartbeatNeverAcks(t *testing.T) {
	t.Parallel()

	// The epoch already completed; even a byte-identical heartbeat replay is
	// rejected, and no classification read runs for the heartbeat op.
	fence := sql.NullInt64{Int64: 7, Valid: true}
	store, db := replayAckStore("completed", fence, "completed", fence)
	err := replayAckApply(t, store, "heartbeat", replayAckMutation("heartbeat"))
	if err != ErrWorkflowClaimRejected {
		t.Fatalf("heartbeat-after-complete error = %v, want ErrWorkflowClaimRejected", err)
	}
	if got, want := len(db.queries), 0; got != want {
		t.Fatalf("heartbeat classification query count = %d, want %d", got, want)
	}
}

func TestWorkflowControlStoreReplayAckRejectsReplayAfterReclaim(t *testing.T) {
	t.Parallel()

	// Epoch 1 failed retryable, then epoch 2 reclaimed the item: the item
	// fencing moved to 8, so the epoch-1 replay is stale, not an ack.
	store, _ := replayAckStore(
		"failed_retryable", sql.NullInt64{Int64: 7, Valid: true},
		"claimed", sql.NullInt64{Int64: 8, Valid: true},
	)
	if err := replayAckApply(t, store, "fail-retryable", replayAckMutation("fail-retryable")); err != ErrWorkflowClaimRejected {
		t.Fatalf("replay-after-reclaim error = %v, want ErrWorkflowClaimRejected", err)
	}
}

func TestWorkflowControlStoreReplayAckRejectsCrossOp(t *testing.T) {
	t.Parallel()

	// The epoch completed, but the replay asks for release: claim status does
	// not equal the op terminal status, so it stays rejected.
	fence := sql.NullInt64{Int64: 7, Valid: true}
	store, _ := replayAckStore("completed", fence, "completed", fence)
	if err := replayAckApply(t, store, "release", replayAckMutation("release")); err != ErrWorkflowClaimRejected {
		t.Fatalf("cross-op replay error = %v, want ErrWorkflowClaimRejected", err)
	}
}

func TestWorkflowControlStoreReplayAckRejectsWrongOwner(t *testing.T) {
	t.Parallel()

	fence := sql.NullInt64{Int64: 7, Valid: true}
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}},
		queryResponses: []queueFakeRows{
			{rows: [][]any{{"completed", fence, "collector-pod-other"}}},
			{rows: [][]any{{"completed", fence}}},
		},
	}
	store := NewWorkflowControlStore(db)
	if err := replayAckApply(t, store, "complete", replayAckMutation("complete")); err != ErrWorkflowClaimRejected {
		t.Fatalf("wrong-owner replay error = %v, want ErrWorkflowClaimRejected", err)
	}
}

func TestWorkflowControlStoreReplayAckRejectsMissingEpoch(t *testing.T) {
	t.Parallel()

	// No claim row for the epoch: short-circuit rejected with one PK read and
	// no work-item read.
	db := &fakeExecQueryer{
		execResults: []sql.Result{rowsAffectedResult{rowsAffected: 0}},
		queryResponses: []queueFakeRows{
			{rows: nil},
		},
	}
	store := NewWorkflowControlStore(db)
	if err := replayAckApply(t, store, "complete", replayAckMutation("complete")); err != ErrWorkflowClaimRejected {
		t.Fatalf("missing-epoch replay error = %v, want ErrWorkflowClaimRejected", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("classification query count = %d, want %d", got, want)
	}
}

func TestWorkflowControlStoreMutationSuccessIssuesNoClassificationReads(t *testing.T) {
	t.Parallel()

	// Hot path is untouched: a mutation that matches a row issues no extra
	// reads on any op.
	for _, op := range []string{"complete", "release", "fail-retryable", "fail-terminal", "heartbeat"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			db := &fakeExecQueryer{}
			store := NewWorkflowControlStore(db)
			if err := replayAckApply(t, store, op, replayAckMutation(op)); err != nil {
				t.Fatalf("%s error = %v, want nil", op, err)
			}
			if got, want := len(db.queries), 0; got != want {
				t.Fatalf("%s query count = %d, want %d", op, got, want)
			}
		})
	}
}
