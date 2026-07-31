// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityClaimLatchSurvivesServiceRetryAndRejectsLegacyCallbacksLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-claim-latch-service"
		generationID = "generation:5854-claim-latch-service"
		workItemID   = "claim-latch-service-5854"
		owner        = "reducer-5854-shared-owner"
	)
	now := time.Date(2026, time.July, 31, 13, 0, 0, 0, time.UTC)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
	seedContainerImageIdentityAckWorkItem(
		t,
		ctx,
		db,
		workItemID,
		scopeID,
		generationID,
		owner,
		now.Add(-time.Minute),
		now.Add(-2*time.Minute),
	)

	queue := &ReducerQueue{
		db:             SQLDB{DB: db},
		LeaseOwner:     owner,
		LeaseDuration:  time.Minute,
		RetryDelay:     time.Second,
		MaxAttempts:    3,
		JitterFraction: 0,
		Now:            func() time.Time { return now },
		ClaimDomain:    reducer.DomainContainerImageIdentity,
	}
	executor := &containerImageIdentityClaimLatchServiceExecutor{
		started: make(chan reducer.Intent, 1),
		release: make(chan struct{}),
	}
	serviceCtx, stopService := context.WithCancel(ctx)
	service := reducer.Service{
		WorkSource:   queue,
		Executor:     executor,
		WorkSink:     queue,
		PollInterval: time.Millisecond,
		Wait: func(context.Context, time.Duration) error {
			stopService()
			return context.Canceled
		},
	}
	serviceDone := make(chan error, 1)
	go func() {
		serviceDone <- service.Run(serviceCtx)
	}()

	var claimed reducer.Intent
	select {
	case claimed = <-executor.started:
	case <-ctx.Done():
		t.Fatal("capable service did not claim the expired legacy attempt")
	}
	if claimed.IntentID != workItemID || claimed.ClaimEpoch != 2 {
		t.Fatalf("capable claim = %+v, want %s at epoch 2", claimed, workItemID)
	}
	assertContainerImageIdentityClaimLatchState(
		t, ctx, db, workItemID, "claimed", 2, true, "claimed",
	)

	legacyResult, legacyErr := db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		workItemID,
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, legacyResult, legacyErr)

	close(executor.release)
	select {
	case err := <-serviceDone:
		if err != nil {
			t.Fatalf("service after retryable lock conflict: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("service did not durably fail the lock-conflicted attempt")
	}
	assertContainerImageIdentityClaimLatchState(
		t, ctx, db, workItemID, "retrying", 2, true, "retrying",
	)
	assertContainerImageIdentityAckOrderingMarkerCount(
		t, ctx, db, scopeID, generationID, 0,
	)

	_, legacyReclaimErr := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed',
    attempt_count = attempt_count + 1,
    lease_owner = $2,
    claim_until = $3,
    last_attempt_at = $1,
    updated_at = $1
WHERE work_item_id = $4
  AND status = 'retrying'
`, now.Add(2*time.Second), owner, now.Add(time.Minute), workItemID)
	assertContainerImageIdentitySQLState(
		t,
		legacyReclaimErr,
		"55000",
		"legacy reclaim after capable lock-conflict retry",
	)

	retryQueue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now.Add(5 * time.Second) },
		ClaimDomain:   reducer.DomainContainerImageIdentity,
	}
	retry, ok, err := retryQueue.Claim(ctx)
	if err != nil {
		t.Fatalf("capable retry claim: %v", err)
	}
	if !ok || retry.IntentID != workItemID || retry.ClaimEpoch != 3 {
		t.Fatalf("capable retry = %+v ok=%t, want %s at epoch 3", retry, ok, workItemID)
	}
	assertContainerImageIdentityClaimLatchState(
		t, ctx, db, workItemID, "claimed", 3, true, "claimed",
	)
	insertContainerImageIdentityCutoverMarker(
		t, ctx, db, scopeID, generationID,
	)
	assertContainerImageIdentityClaimLatchState(
		t, ctx, db, workItemID, "running", 3, true, "running",
	)
	assertContainerImageIdentityAckOrderingMarkerCount(
		t, ctx, db, scopeID, generationID, 1,
	)
}

type containerImageIdentityClaimLatchServiceExecutor struct {
	started chan reducer.Intent
	release chan struct{}
}

func (e *containerImageIdentityClaimLatchServiceExecutor) Execute(
	ctx context.Context,
	intent reducer.Intent,
) (reducer.Result, error) {
	select {
	case e.started <- intent:
	case <-ctx.Done():
		return reducer.Result{}, ctx.Err()
	}
	select {
	case <-e.release:
		return reducer.Result{}, containerImageIdentityClaimLatchLockBusyError{}
	case <-ctx.Done():
		return reducer.Result{}, ctx.Err()
	}
}

type containerImageIdentityClaimLatchLockBusyError struct{}

func (containerImageIdentityClaimLatchLockBusyError) Error() string {
	return "synthetic container image identity cutover row lock conflict"
}

func (containerImageIdentityClaimLatchLockBusyError) Retryable() bool {
	return true
}

func (containerImageIdentityClaimLatchLockBusyError) FailureClass() string {
	return "container_image_identity_cutover_lock_busy"
}

func assertContainerImageIdentityClaimLatchState(
	t *testing.T,
	ctx context.Context,
	db interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	workItemID string,
	wantStatus string,
	wantEpoch int64,
	wantRequired bool,
	wantAuthorized string,
) {
	t.Helper()
	var (
		status     string
		epoch      int64
		required   bool
		authorized string
	)
	if err := db.QueryRowContext(ctx, `
SELECT
    status,
    container_image_identity_claim_epoch,
    container_image_identity_v2_required,
    container_image_identity_v2_authorized_status
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&status, &epoch, &required, &authorized); err != nil {
		t.Fatalf("read claim latch state: %v", err)
	}
	if status != wantStatus ||
		epoch != wantEpoch ||
		required != wantRequired ||
		authorized != wantAuthorized {
		t.Fatalf(
			"claim latch state = %s/%d/%t/%q, want %s/%d/%t/%q",
			status, epoch, required, authorized,
			wantStatus, wantEpoch, wantRequired, wantAuthorized,
		)
	}
}

var (
	_ error                              = containerImageIdentityClaimLatchLockBusyError{}
	_ interface{ Retryable() bool }      = containerImageIdentityClaimLatchLockBusyError{}
	_ interface{ FailureClass() string } = containerImageIdentityClaimLatchLockBusyError{}
)
