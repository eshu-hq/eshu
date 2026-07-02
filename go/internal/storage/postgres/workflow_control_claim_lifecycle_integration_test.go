// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreIntegrationHeartbeatReclaimAndSplitBrain(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 15, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-1",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-integration-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	item, claimA, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-a",
		ClaimID:             "claim-a",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}
	if got, want := claimA.FencingToken, int64(1); got != want {
		t.Fatalf("claimA.FencingToken = %d, want %d", got, want)
	}
	mustHeartbeatClaim(t, store, ctx, workflow.ClaimMutation{
		WorkItemID:    item.WorkItemID,
		ClaimID:       claimA.ClaimID,
		FencingToken:  claimA.FencingToken,
		OwnerID:       claimA.OwnerID,
		ObservedAt:    now.Add(10 * time.Second),
		LeaseDuration: time.Minute,
	})
	mustClaimState(t, db, claimA.ClaimID, workflow.ClaimStatusActive, claimA.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusClaimed, claimA.ClaimID, claimA.FencingToken)

	reapAt := now.Add(2 * time.Minute)
	claims, err := store.ReapExpiredClaims(ctx, reapAt, 10, 0)
	if err != nil {
		t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
	}
	if got, want := len(claims), 1; got != want {
		t.Fatalf("len(claims) = %d, want %d", got, want)
	}
	if got, want := claims[0].ClaimID, claimA.ClaimID; got != want {
		t.Fatalf("reaped claim id = %q, want %q", got, want)
	}
	mustClaimState(t, db, claimA.ClaimID, workflow.ClaimStatusExpired, claimA.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusPending, "", claimA.FencingToken)

	claimAfterReap := reapAt.Add(DefaultWorkflowExpiredClaimRequeueDelay + time.Second)
	item2, claimB, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-b",
		ClaimID:             "claim-b",
	}, claimAfterReap, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() after reap error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() after reap found = false, want true")
	}
	if got, want := item2.WorkItemID, item.WorkItemID; got != want {
		t.Fatalf("reclaimed work item = %q, want %q", got, want)
	}
	if got, want := claimB.FencingToken, claimA.FencingToken+1; got != want {
		t.Fatalf("claimB.FencingToken = %d, want %d", got, want)
	}

	staleErr := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID:    item.WorkItemID,
		ClaimID:       claimA.ClaimID,
		FencingToken:  claimA.FencingToken,
		OwnerID:       claimA.OwnerID,
		ObservedAt:    claimAfterReap.Add(time.Second),
		LeaseDuration: time.Minute,
	})
	if !errors.Is(staleErr, ErrWorkflowClaimRejected) {
		t.Fatalf("stale HeartbeatClaim() error = %v, want ErrWorkflowClaimRejected", staleErr)
	}
	staleErr = store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID:   item.WorkItemID,
		ClaimID:      claimA.ClaimID,
		FencingToken: claimA.FencingToken,
		OwnerID:      claimA.OwnerID,
		ObservedAt:   claimAfterReap.Add(2 * time.Second),
	})
	if !errors.Is(staleErr, ErrWorkflowClaimRejected) {
		t.Fatalf("stale CompleteClaim() error = %v, want ErrWorkflowClaimRejected", staleErr)
	}

	if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
		WorkItemID:   item2.WorkItemID,
		ClaimID:      claimB.ClaimID,
		FencingToken: claimB.FencingToken,
		OwnerID:      claimB.OwnerID,
		ObservedAt:   claimAfterReap.Add(3 * time.Second),
	}); err != nil {
		t.Fatalf("CompleteClaim() error = %v, want nil", err)
	}
	mustClaimState(t, db, claimB.ClaimID, workflow.ClaimStatusCompleted, claimB.FencingToken)
	mustWorkItemState(t, db, item.WorkItemID, workflow.WorkItemStatusCompleted, "", claimB.FencingToken)
}

func TestWorkflowControlStoreIntegrationReapExpiredClaimsLeavesActiveClaimsUntouched(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 16, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-2",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-2",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-integration-2",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-3",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-integration-3",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now.Add(time.Second),
		UpdatedAt:           now.Add(time.Second),
	})

	expiredItem, expiredClaim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-a",
		ClaimID:             "claim-expired",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}
	activeAt := now.Add(90 * time.Second)
	activeItem, activeClaim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-b",
		ClaimID:             "claim-active",
	}, activeAt, time.Minute)
	if err != nil {
		t.Fatalf("second ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("second ClaimNextEligible() found = false, want true")
	}

	markClaimExpired(t, db, expiredClaim.ClaimID, expiredItem.WorkItemID, expiredClaim.OwnerID, expiredClaim.FencingToken, now.Add(-time.Second))

	claims, err := store.ReapExpiredClaims(ctx, now.Add(2*time.Minute), 10, 0)
	if err != nil {
		t.Fatalf("ReapExpiredClaims() error = %v, want nil", err)
	}
	if got, want := len(claims), 1; got != want {
		t.Fatalf("len(claims) = %d, want %d", got, want)
	}
	if got, want := claims[0].ClaimID, expiredClaim.ClaimID; got != want {
		t.Fatalf("reaped claim id = %q, want %q", got, want)
	}

	mustClaimState(t, db, expiredClaim.ClaimID, workflow.ClaimStatusExpired, expiredClaim.FencingToken)
	mustWorkItemState(t, db, expiredItem.WorkItemID, workflow.WorkItemStatusPending, "", expiredClaim.FencingToken)
	mustClaimState(t, db, activeClaim.ClaimID, workflow.ClaimStatusActive, activeClaim.FencingToken)
	mustWorkItemState(t, db, activeItem.WorkItemID, workflow.WorkItemStatusClaimed, activeClaim.ClaimID, activeClaim.FencingToken)
}

func TestWorkflowControlStoreIntegrationClaimOrderRemainsFifoWithinCollectorInstance(t *testing.T) {
	_, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 17, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-3",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	for _, item := range []workflow.WorkItem{
		{
			WorkItemID:          "integration-item-fifo-1",
			RunID:               run.RunID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			ScopeID:             "scope-fifo-1",
			FairnessKey:         "zzz-late-lexical",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		{
			WorkItemID:          "integration-item-fifo-2",
			RunID:               run.RunID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			ScopeID:             "scope-fifo-2",
			FairnessKey:         "aaa-early-lexical",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now.Add(time.Second),
			UpdatedAt:           now.Add(time.Second),
		},
		{
			WorkItemID:          "integration-item-fifo-3",
			RunID:               run.RunID,
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			ScopeID:             "scope-fifo-3",
			FairnessKey:         "mmm-middle-lexical",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now.Add(2 * time.Second),
			UpdatedAt:           now.Add(2 * time.Second),
		},
	} {
		mustEnqueueWorkItem(t, store, ctx, item)
	}

	claimAt := now.Add(10 * time.Second)
	wantOrder := []string{
		"integration-item-fifo-1",
		"integration-item-fifo-2",
		"integration-item-fifo-3",
	}
	for i, wantWorkItemID := range wantOrder {
		item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			OwnerID:             fmt.Sprintf("collector-pod-fifo-%d", i+1),
			ClaimID:             fmt.Sprintf("claim-fifo-%d", i+1),
		}, claimAt.Add(time.Duration(i)*time.Second), time.Minute)
		if err != nil {
			t.Fatalf("ClaimNextEligible() #%d error = %v, want nil", i+1, err)
		}
		if !found {
			t.Fatalf("ClaimNextEligible() #%d found = false, want true", i+1)
		}
		if got := item.WorkItemID; got != wantWorkItemID {
			t.Fatalf("ClaimNextEligible() #%d work item = %q, want %q", i+1, got, wantWorkItemID)
		}
		if err := store.CompleteClaim(ctx, workflow.ClaimMutation{
			WorkItemID:   item.WorkItemID,
			ClaimID:      claim.ClaimID,
			FencingToken: claim.FencingToken,
			OwnerID:      claim.OwnerID,
			ObservedAt:   claimAt.Add(time.Duration(i+1) * time.Second),
		}); err != nil {
			t.Fatalf("CompleteClaim() #%d error = %v, want nil", i+1, err)
		}
	}

	_, _, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-fifo-final",
		ClaimID:             "claim-fifo-final",
	}, claimAt.Add(10*time.Second), time.Minute)
	if err != nil {
		t.Fatalf("final ClaimNextEligible() error = %v, want nil", err)
	}
	if found {
		t.Fatal("final ClaimNextEligible() found = true, want false")
	}
}
