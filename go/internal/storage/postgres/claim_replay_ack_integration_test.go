// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Live proof for the Q1 replay-ack alignment (#9428, ruling 6476206689
// SATELLITE HOLDS) against real Postgres: identical terminal replays ack,
// heartbeat-after-terminal rejects, replay-after-reclaim rejects, and a
// concurrent replay-vs-reclaim pair keeps an exactly-once final state with
// the successor epoch unaffected.
//
// Requires ESHU_POSTGRES_DSN; skips without it like every other workflow
// control integration proof.

func replayAckSeedRunItem(t *testing.T, store *WorkflowControlStore, ctx context.Context, now time.Time, tag string) (workflow.WorkItem, workflow.Claim) {
	t.Helper()
	run := workflow.Run{
		RunID:       "replay-ack-" + tag,
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "replay-ack-item-" + tag,
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-replay-ack-" + tag,
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-" + tag,
		ClaimID:             "replay-ack-claim-" + tag,
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}
	return item, claim
}

func replayAckMutationFor(item workflow.WorkItem, claim workflow.Claim, at time.Time) workflow.ClaimMutation {
	return workflow.ClaimMutation{
		WorkItemID:     item.WorkItemID,
		ClaimID:        claim.ClaimID,
		FencingToken:   claim.FencingToken,
		OwnerID:        claim.OwnerID,
		ObservedAt:     at,
		LeaseDuration:  time.Minute,
		VisibleAt:      at,
		FailureClass:   "collect_failure",
		FailureMessage: "synthetic probe failure",
	}
}

func replayAckApplyOp(t *testing.T, store *WorkflowControlStore, ctx context.Context, op string, mutation workflow.ClaimMutation) {
	t.Helper()
	var err error
	switch op {
	case "complete":
		err = store.CompleteClaim(ctx, mutation)
	case "release":
		err = store.ReleaseClaim(ctx, mutation)
	case "fail-retryable":
		err = store.FailClaimRetryable(ctx, mutation)
	case "fail-terminal":
		err = store.FailClaimTerminal(ctx, mutation)
	default:
		t.Fatalf("unknown op %q", op)
	}
	if err != nil {
		t.Fatalf("%s first apply error = %v, want nil", op, err)
	}
}

func TestWorkflowControlStoreIntegrationReplayAckTerminalReplays(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 15, 0, 0, 0, time.UTC)

	cases := []struct {
		name            string
		op              string
		wantClaimStatus workflow.ClaimStatus
		wantItemStatus  workflow.WorkItemStatus
	}{
		{name: "complete", op: "complete", wantClaimStatus: workflow.ClaimStatusCompleted, wantItemStatus: workflow.WorkItemStatusCompleted},
		{name: "release", op: "release", wantClaimStatus: workflow.ClaimStatusReleased, wantItemStatus: workflow.WorkItemStatusPending},
		{name: "fail-retryable", op: "fail-retryable", wantClaimStatus: workflow.ClaimStatusFailedRetryable, wantItemStatus: workflow.WorkItemStatusPending},
		{name: "fail-terminal", op: "fail-terminal", wantClaimStatus: workflow.ClaimStatusFailedTerminal, wantItemStatus: workflow.WorkItemStatusFailedTerminal},
	}
	for i, tc := range cases {
		tag := fmt.Sprintf("%s-%d", tc.name, i)
		item, claim := replayAckSeedRunItem(t, store, ctx, now.Add(time.Duration(i)*time.Hour), tag)
		first := replayAckMutationFor(item, claim, now.Add(time.Duration(i)*time.Hour).Add(10*time.Second))
		mustHeartbeatClaim(t, store, ctx, workflow.ClaimMutation{
			WorkItemID:    item.WorkItemID,
			ClaimID:       claim.ClaimID,
			FencingToken:  claim.FencingToken,
			OwnerID:       claim.OwnerID,
			ObservedAt:    first.ObservedAt,
			LeaseDuration: time.Minute,
		})
		replayAckApplyOp(t, store, ctx, tc.op, first)
		mustClaimState(t, db, claim.ClaimID, tc.wantClaimStatus, claim.FencingToken)

		// Identical replay acks: same claim, fencing, owner, unmoved item.
		if err := func() error {
			switch tc.op {
			case "complete":
				return store.CompleteClaim(ctx, first)
			case "release":
				return store.ReleaseClaim(ctx, first)
			case "fail-retryable":
				return store.FailClaimRetryable(ctx, first)
			default:
				return store.FailClaimTerminal(ctx, first)
			}
		}(); err != nil {
			t.Fatalf("%s identical replay error = %v, want nil", tc.op, err)
		}
		mustClaimState(t, db, claim.ClaimID, tc.wantClaimStatus, claim.FencingToken)

		// Heartbeat after the terminal state never acks.
		heartbeatErr := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
			WorkItemID:    item.WorkItemID,
			ClaimID:       claim.ClaimID,
			FencingToken:  claim.FencingToken,
			OwnerID:       claim.OwnerID,
			ObservedAt:    first.ObservedAt.Add(time.Second),
			LeaseDuration: time.Minute,
		})
		if !errors.Is(heartbeatErr, ErrWorkflowClaimRejected) {
			t.Fatalf("%s heartbeat-after-terminal error = %v, want ErrWorkflowClaimRejected", tc.op, heartbeatErr)
		}
	}
}

func TestWorkflowControlStoreIntegrationReplayAckRejectsReplayAfterReclaim(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 16, 0, 0, 0, time.UTC)
	item, claimA := replayAckSeedRunItem(t, store, ctx, now, "reclaim")

	first := replayAckMutationFor(item, claimA, now.Add(10*time.Second))
	replayAckApplyOp(t, store, ctx, "fail-retryable", first)

	// Successor epoch reclaims the item and moves the fencing forward.
	reclaimAt := now.Add(time.Hour)
	item2, claimB, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-reclaim-b",
		ClaimID:             "replay-ack-claim-reclaim-b",
	}, reclaimAt, time.Minute)
	if err != nil || !found {
		t.Fatalf("reclaim ClaimNextEligible() found=%v err=%v, want true nil", found, err)
	}
	if claimB.FencingToken <= claimA.FencingToken {
		t.Fatalf("successor fencing %d does not advance past %d", claimB.FencingToken, claimA.FencingToken)
	}

	// The epoch-1 replay is stale now, not an ack.
	if err := store.FailClaimRetryable(ctx, first); !errors.Is(err, ErrWorkflowClaimRejected) {
		t.Fatalf("replay-after-reclaim error = %v, want ErrWorkflowClaimRejected", err)
	}
	// The original epoch row is untouched: exactly-once terminal state.
	mustClaimState(t, db, claimA.ClaimID, workflow.ClaimStatusFailedRetryable, claimA.FencingToken)

	// The successor epoch is unaffected: it completes, and its own identical
	// replay acks.
	second := replayAckMutationFor(item2, claimB, reclaimAt.Add(10*time.Second))
	replayAckApplyOp(t, store, ctx, "complete", second)
	if err := store.CompleteClaim(ctx, second); err != nil {
		t.Fatalf("successor identical replay error = %v, want nil", err)
	}
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusCompleted, "", claimB.FencingToken)
}

// TestWorkflowControlStoreIntegrationReplayAckConcurrentReplayVsReclaim pins
// the concurrency contract: N identical fail-retryable replays race one
// reclaim-plus-complete successor. Every replay returns nil (read the
// committed epoch-1 terminal state) or ErrWorkflowClaimRejected (lost the race
// to the successor fencing move) — never another error — and the final state
// is exactly once: epoch 1 failed_retryable a single time, epoch 2 completed,
// the successor complete unaffected by the replay storm.
func TestWorkflowControlStoreIntegrationReplayAckConcurrentReplayVsReclaim(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 17, 0, 0, 0, time.UTC)
	item, claimA := replayAckSeedRunItem(t, store, ctx, now, "race")
	first := replayAckMutationFor(item, claimA, now.Add(10*time.Second))
	replayAckApplyOp(t, store, ctx, "fail-retryable", first)

	const replays = 8
	replayErrs := make([]error, replays)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range replays {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			replayErrs[i] = store.FailClaimRetryable(ctx, first)
		}(i)
	}
	var successorErr error
	var successorClaim workflow.Claim
	var successorItem workflow.WorkItem
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		reclaimAt := now.Add(time.Hour)
		next, nextClaim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			OwnerID:             "collector-pod-race-b",
			ClaimID:             "replay-ack-claim-race-b",
		}, reclaimAt, time.Minute)
		if err != nil || !found {
			successorErr = fmt.Errorf("reclaim ClaimNextEligible() found=%v err=%v", found, err)
			return
		}
		successorItem, successorClaim = next, nextClaim
		second := replayAckMutationFor(next, nextClaim, reclaimAt.Add(10*time.Second))
		if err := store.CompleteClaim(ctx, second); err != nil {
			successorErr = fmt.Errorf("successor CompleteClaim() error = %w", err)
		}
	}()
	close(start)
	wg.Wait()

	if successorErr != nil {
		t.Fatalf("successor epoch error = %v, want nil", successorErr)
	}
	for i, err := range replayErrs {
		if err != nil && !errors.Is(err, ErrWorkflowClaimRejected) {
			t.Fatalf("replay %d error = %v, want nil or ErrWorkflowClaimRejected", i, err)
		}
	}
	// Exactly-once final: epoch 1 terminal exactly as committed, epoch 2
	// completed, item fenced to the successor.
	mustClaimState(t, db, claimA.ClaimID, workflow.ClaimStatusFailedRetryable, claimA.FencingToken)
	mustClaimState(t, db, successorClaim.ClaimID, workflow.ClaimStatusCompleted, successorClaim.FencingToken)
	mustWorkItemState(t, db, successorItem.WorkItemID, workflow.WorkItemStatusCompleted, "", successorClaim.FencingToken)
}
