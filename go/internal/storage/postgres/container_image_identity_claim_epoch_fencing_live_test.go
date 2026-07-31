// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityCapablePreCutoverClaimSkipsTriggerFunctionLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-capable-precutover-skip"
		generationID = "generation:5854-capable-precutover-skip"
		workItemID   = "claim-trigger-5854-capable-precutover-skip"
		owner        = "reducer-5854-capable-precutover-skip"
	)
	now := time.Date(2026, time.July, 31, 11, 30, 0, 0, time.UTC)
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
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'pending',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = $1
WHERE work_item_id = $2
`, now, workItemID); err != nil {
		t.Fatalf("prepare capable pre-cutover claim: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
CREATE OR REPLACE FUNCTION advance_container_image_identity_claim_epoch()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION USING
        ERRCODE = '55000',
        MESSAGE = 'capable pre-cutover claim entered trigger function';
END;
$function$
`); err != nil {
		t.Fatalf("install trigger-entry sentinel: %v", err)
	}

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainContainerImageIdentity,
	}
	intent, ok, err := queue.Claim(ctx)
	if err != nil {
		t.Fatalf("capable pre-cutover claim: %v", err)
	}
	if !ok || intent.IntentID != workItemID || intent.ClaimEpoch != 2 {
		t.Fatalf(
			"capable pre-cutover claim = %+v ok=%t, want %s at epoch 2",
			intent,
			ok,
			workItemID,
		)
	}
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
`, workItemID).Scan(
		&status,
		&attemptCount,
		&claimEpoch,
		&authorizedStatus,
	); err != nil {
		t.Fatalf("read capable pre-cutover claim: %v", err)
	}
	if status != "claimed" ||
		attemptCount != 2 ||
		claimEpoch != 2 ||
		authorizedStatus != "" {
		t.Fatalf(
			"capable pre-cutover row = %s/%d/%d/%q, want claimed/2/2/empty",
			status,
			attemptCount,
			claimEpoch,
			authorizedStatus,
		)
	}
}

func TestContainerImageIdentityCurrentBatchClaimAdvancesAndSucceedsLive(
	t *testing.T,
) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const owner = "reducer-5854-current-batch"
	now := time.Date(2026, time.July, 31, 1, 30, 0, 0, time.UTC)
	workItemIDs := []string{
		"claim-trigger-5854-current-batch-a",
		"claim-trigger-5854-current-batch-b",
	}
	for index, workItemID := range workItemIDs {
		scopeID := fmt.Sprintf("repository:5854-current-batch-%d", index)
		generationID := fmt.Sprintf("generation:5854-current-batch-%d", index)
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
	}

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    owner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainContainerImageIdentity,
	}
	claimed, err := queue.ClaimBatch(ctx, len(workItemIDs))
	if err != nil {
		t.Fatalf("current batch claim: %v", err)
	}
	if len(claimed) != len(workItemIDs) {
		t.Fatalf("current batch claim count = %d, want %d", len(claimed), len(workItemIDs))
	}
	for _, intent := range claimed {
		if intent.ClaimEpoch != 2 {
			t.Fatalf("current batch claim %s epoch = %d, want 2", intent.IntentID, intent.ClaimEpoch)
		}
	}
	if err := queue.AckBatch(ctx, claimed, nil); err != nil {
		t.Fatalf("ack current batch claims: %v", err)
	}
	for _, workItemID := range workItemIDs {
		assertContainerImageIdentityAckClaimFence(
			t,
			ctx,
			db,
			workItemID,
			"succeeded",
			2,
			2,
		)
	}
}

func TestContainerImageIdentityClaimRejectsInvalidEpochJumpLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	const (
		scopeID      = "repository:5854-invalid-epoch-jump"
		generationID = "generation:5854-invalid-epoch-jump"
		workItemID   = "claim-trigger-5854-invalid-epoch-jump"
		owner        = "reducer-5854-invalid-epoch-jump"
	)
	now := time.Date(2026, time.July, 31, 1, 40, 0, 0, time.UTC)
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

	_, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch =
        container_image_identity_claim_epoch + 2,
    last_attempt_at = $1
WHERE work_item_id = $2
`, now, workItemID)
	assertContainerImageIdentitySQLState(
		t,
		err,
		"55000",
		"invalid claim epoch jump",
	)
	assertContainerImageIdentityAckClaimFence(
		t,
		ctx,
		db,
		workItemID,
		"running",
		1,
		1,
	)
}

func assertContainerImageIdentitySQLState(
	t *testing.T,
	err error,
	want string,
	label string,
) {
	t.Helper()
	var sqlState interface{ SQLState() string }
	if err == nil || !errors.As(err, &sqlState) || sqlState.SQLState() != want {
		t.Fatalf("%s error = %v, want SQLSTATE %s", label, err, want)
	}
}
