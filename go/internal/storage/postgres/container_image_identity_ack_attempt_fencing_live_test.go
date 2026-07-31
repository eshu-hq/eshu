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

func TestContainerImageIdentityAckAttemptFenceSurvivesSameOwnerReclaimAndReplayLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-ack-attempt-fence"
		generationID = "generation:5854-ack-attempt-fence"
		workItemID   = "ack-5854-attempt-fence"
		owner        = "reducer"
	)
	now := time.Date(2026, time.July, 30, 21, 0, 0, 0, time.UTC)
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
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, generationID)
	legacyClaimResult, legacyClaimErr := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'claimed',
    attempt_count = attempt_count + 1,
    lease_owner = $2,
    claim_until = $3,
    last_attempt_at = $1,
    updated_at = $1
WHERE work_item_id = $4
  AND stage = 'reducer'
  AND status IN ('pending', 'retrying', 'claimed', 'running')
  AND (claim_until IS NULL OR claim_until <= $1)
`, now, owner, now.Add(time.Minute), workItemID)
	assertContainerImageIdentityLegacyAckRejected(
		t,
		legacyClaimResult,
		legacyClaimErr,
	)

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainContainerImageIdentity,
	}
	reclaimed, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("same-owner reclaim: %v", err)
	}
	if !ok || reclaimed.IntentID != workItemID || reclaimed.AttemptCount != 2 {
		t.Fatalf("same-owner reclaim = %+v ok=%t, want attempt 2", reclaimed, ok)
	}

	staleFirstAttempt := reducer.Intent{
		IntentID:     workItemID,
		Domain:       reducer.DomainContainerImageIdentity,
		AttemptCount: 1,
		ClaimEpoch:   1,
	}
	if err := queue.Ack(ctx, staleFirstAttempt, reducer.Result{}); err != ErrReducerClaimRejected {
		t.Fatalf("stale new-binary ACK = %v, want claim rejection", err)
	}
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, workItemID, "running", owner,
	)
	legacyResult, legacyErr := db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		workItemID,
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, legacyResult, legacyErr)

	if err := queue.Ack(ctx, reclaimed, reducer.Result{}); err != nil {
		t.Fatalf("current attempt ACK: %v", err)
	}
	assertContainerImageIdentityAckClaimFence(
		t, ctx, db, workItemID, "succeeded", 2, 2,
	)

	legacyResult, legacyErr = db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending',
    attempt_count = 0,
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1,
    updated_at = $1
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND status = 'succeeded'
`, now.Add(time.Minute), workItemID)
	assertContainerImageIdentityLegacyAckRejected(t, legacyResult, legacyErr)

	reopened, err := queue.ReopenSucceeded(ctx, workItemID)
	if err != nil {
		t.Fatalf("attempt-safe reopen: %v", err)
	}
	if !reopened {
		t.Fatal("attempt-safe reopen = false, want true")
	}
	assertContainerImageIdentityAckClaimFence(
		t, ctx, db, workItemID, "pending", 0, 2,
	)
	replayed, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("claim replayed work: %v", err)
	}
	if !ok || replayed.IntentID != workItemID ||
		replayed.AttemptCount != 1 || replayed.ClaimEpoch != 3 {
		t.Fatalf("replayed claim = %+v ok=%t, want attempt 1 at monotonic epoch 3", replayed, ok)
	}
	legacyResult, legacyErr = db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now.Add(2*time.Minute),
		workItemID,
		owner,
	)
	assertContainerImageIdentityLegacyAckRejected(t, legacyResult, legacyErr)
	if err := queue.Ack(ctx, replayed, reducer.Result{}); err != nil {
		t.Fatalf("replayed current attempt ACK: %v", err)
	}
	assertContainerImageIdentityAckClaimFence(
		t, ctx, db, workItemID, "succeeded", 1, 3,
	)
}

func assertContainerImageIdentityAckClaimFence(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	wantStatus string,
	wantAttempt int,
	wantEpoch int64,
) {
	t.Helper()
	var (
		status           string
		attemptCount     int
		claimEpoch       int64
		authorizedStatus string
	)
	if err := db.QueryRowContext(ctx, `
SELECT
    status,
    attempt_count,
    container_image_identity_claim_epoch,
    container_image_identity_v2_authorized_status
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&status, &attemptCount, &claimEpoch, &authorizedStatus); err != nil {
		t.Fatalf("read ACK claim fence: %v", err)
	}
	if status != wantStatus || attemptCount != wantAttempt || claimEpoch != wantEpoch {
		t.Fatalf(
			"ACK attempt state = status %s attempt %d epoch %d, want %s/%d/%d",
			status,
			attemptCount,
			claimEpoch,
			wantStatus,
			wantAttempt,
			wantEpoch,
		)
	}
	if authorizedStatus != wantStatus {
		t.Fatalf(
			"authorized status = %q, want %q",
			authorizedStatus,
			wantStatus,
		)
	}
}
