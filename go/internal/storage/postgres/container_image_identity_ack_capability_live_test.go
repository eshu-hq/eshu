// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const legacyContainerImageIdentityAckQuery = `
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    visible_at = NULL,
    updated_at = $1,
    failure_class = NULL,
    failure_message = NULL,
    failure_details = NULL
WHERE work_item_id = $2
  AND stage = 'reducer'
  AND lease_owner = $3
  AND status IN ('claimed', 'running')
`

func TestContainerImageIdentityAckAttemptFenceMixedVersionLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	// Live clock: the ACK fence requires claim_until > clock_timestamp(),
	// so a fixed seed date rots into permanent fence rejections.
	now := time.Now().UTC().Truncate(time.Microsecond)

	const (
		scopeMarkedSingle  = "repository:5854-ack-marked-single"
		scopeUnmarked      = "repository:5854-ack-before-marker"
		scopeMarkedBatch   = "repository:5854-ack-marked-batch"
		scopeUnmarkedBatch = "repository:5854-ack-unmarked-batch"
		scopeReclaim       = "repository:5854-ack-reclaim"
		scopeLegacyV2      = "repository:5854-ack-legacy-v2"
		legacyOwner        = "legacy-reducer-5854"
		capableOwner       = "capable-reducer-5854"
		markedSingle       = "generation:5854-ack-marked-single"
		unmarked           = "generation:5854-ack-before-marker"
		markedBatch        = "generation:5854-ack-marked-batch"
		unmarkedBatch      = "generation:5854-ack-unmarked-batch"
		reclaimGen         = "generation:5854-ack-reclaim"
		legacyV2Gen        = "generation:5854-ack-legacy-v2"
	)
	for _, fixture := range []struct {
		scopeID      string
		generationID string
	}{
		{scopeMarkedSingle, markedSingle},
		{scopeUnmarked, unmarked},
		{scopeMarkedBatch, markedBatch},
		{scopeUnmarkedBatch, unmarkedBatch},
		{scopeReclaim, reclaimGen},
		{scopeLegacyV2, legacyV2Gen},
	} {
		seedContainerImageIdentityAckScope(t, ctx, db, fixture.scopeID)
		seedContainerImageIdentityAckGeneration(
			t,
			ctx,
			db,
			fixture.scopeID,
			fixture.generationID,
		)
	}

	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, "ack-5854-marked-single", scopeMarkedSingle, markedSingle,
		legacyOwner, now.Add(time.Minute), now,
	)
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeMarkedSingle, markedSingle)
	// The replacement guard must also preserve an existing marker without
	// advancing either claim latch a second time.
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeMarkedSingle, markedSingle)

	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, "ack-5854-legacy-v2", scopeLegacyV2, legacyV2Gen,
		legacyOwner, now.Add(time.Minute), now,
	)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_v3_required = FALSE,
    container_image_identity_v3_authorized_status = ''
WHERE work_item_id = 'ack-5854-legacy-v2'
`); err != nil {
		t.Fatalf("mark synthetic legacy-v2 attempt: %v", err)
	}
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeLegacyV2, legacyV2Gen)
	var legacyV2Status, legacyV2V3Authorized string
	var legacyV2V3Required bool
	if err := db.QueryRowContext(ctx, `
SELECT status, container_image_identity_v3_required,
       container_image_identity_v3_authorized_status
FROM fact_work_items
WHERE work_item_id = 'ack-5854-legacy-v2'
`).Scan(&legacyV2Status, &legacyV2V3Required, &legacyV2V3Authorized); err != nil {
		t.Fatalf("read synthetic legacy-v2 attempt: %v", err)
	}
	if legacyV2Status != "running" || legacyV2V3Required || legacyV2V3Authorized != "" {
		t.Fatalf(
			"legacy-v2 marker state = %q required=%t authorized=%q, want running/false/empty",
			legacyV2Status,
			legacyV2V3Required,
			legacyV2V3Authorized,
		)
	}
	insertContainerImageIdentityV2Fact(t, ctx, db, scopeMarkedSingle, markedSingle, "single")
	_, err := db.ExecContext(
		ctx,
		containerImageIdentityAckLegacyFactInsertSQL("single"),
		scopeMarkedSingle,
		markedSingle,
	)
	var legacySQLState interface{ SQLState() string }
	if !errors.As(err, &legacySQLState) || legacySQLState.SQLState() != "55000" {
		t.Fatalf("post-cutover legacy fact error = %v, want SQLSTATE 55000", err)
	}
	legacyResult, legacyErr := db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-marked-single",
		legacyOwner,
	)
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, db, "ack-5854-marked-single", legacyResult, legacyErr, 1, "pending",
	)
	assertContainerImageIdentityAckFactCount(
		t, ctx, db, scopeMarkedSingle, markedSingle, "image_ref_v2", 1,
	)
	capableQueue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    capableOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	// The fence cleared the lease, so the capable owner claims the fenced
	// row through the queue instead of a hand-built intent.
	markedSingleClaim, ok, err := capableQueue.Claim(ctx)
	if err != nil || !ok || markedSingleClaim.IntentID != "ack-5854-marked-single" {
		t.Fatalf("claim fenced marked single = %+v ok=%t err=%v", markedSingleClaim, ok, err)
	}
	if err := capableQueue.Ack(ctx, markedSingleClaim, reducer.Result{}); err != nil {
		t.Fatalf("capable single ACK: %v", err)
	}
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, "ack-5854-marked-single", "succeeded", "",
	)

	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, "ack-5854-before-marker", scopeUnmarked, unmarked,
		legacyOwner, now.Add(time.Minute), now,
	)
	result, err := db.ExecContext(
		ctx,
		containerImageIdentityAckLegacyFactInsertSQL("before-marker"),
		scopeUnmarked,
		unmarked,
	)
	if err != nil {
		t.Fatalf("pre-cutover legacy fact insert: %v", err)
	}
	assertContainerImageIdentityAckRowsAffected(t, result, 1)
	legacyResult, legacyErr = db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-before-marker",
		legacyOwner,
	)
	// Pre-marker rows carry no v2 requirement, so the fence resets the
	// authorized status to empty rather than pending.
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, db, "ack-5854-before-marker", legacyResult, legacyErr, 1, "",
	)
	preCutover, ok, err := capableQueue.Claim(ctx)
	if err != nil || !ok || preCutover.IntentID != "ack-5854-before-marker" {
		t.Fatalf("claim fenced pre-cutover = %+v ok=%t err=%v", preCutover, ok, err)
	}
	if err := capableQueue.Ack(ctx, preCutover, reducer.Result{}); err != nil {
		t.Fatalf("capable pre-cutover ACK: %v", err)
	}
	reopened, err := capableQueue.ReopenSucceeded(
		ctx,
		"ack-5854-before-marker",
	)
	if err != nil || !reopened {
		t.Fatalf("reopen pre-cutover legacy success = %t, %v", reopened, err)
	}
	current, ok, err := capableQueue.Claim(ctx)
	if err != nil || !ok || current.IntentID != "ack-5854-before-marker" ||
		current.ClaimEpoch != 3 {
		t.Fatalf("claim pre-cutover legacy success = %+v ok=%t err=%v", current, ok, err)
	}
	completeContainerImageIdentityAckCutover(
		t, ctx, db, scopeUnmarked, unmarked, "before-marker",
	)
	assertContainerImageIdentityAckFactCount(
		t, ctx, db, scopeUnmarked, unmarked, "", 0,
	)
	assertContainerImageIdentityAckFactCount(
		t, ctx, db, scopeUnmarked, unmarked, "image_ref_v2", 1,
	)

	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, "ack-5854-batch-marked", scopeMarkedBatch, markedBatch,
		legacyOwner, now.Add(time.Minute), now,
	)
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, "ack-5854-batch-unmarked", scopeUnmarkedBatch, unmarkedBatch,
		legacyOwner, now.Add(time.Minute), now,
	)
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeMarkedBatch, markedBatch)
	legacyResult, legacyErr = db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'succeeded',
    lease_owner = NULL,
    claim_until = NULL,
    updated_at = $1
WHERE work_item_id IN ($2, $3)
  AND stage = 'reducer'
  AND lease_owner = $4
  AND status IN ('claimed', 'running')
`, now, "ack-5854-batch-marked", "ack-5854-batch-unmarked", legacyOwner)
	if legacyErr != nil {
		t.Fatalf("mixed batch legacy ACK error = %v, want fenced success", legacyErr)
	}
	assertContainerImageIdentityAckRowsAffected(t, legacyResult, 2)
	assertContainerImageIdentityWorkItemFenced(
		t, ctx, db, "ack-5854-batch-marked", 1, "pending",
	)
	assertContainerImageIdentityWorkItemFenced(
		t, ctx, db, "ack-5854-batch-unmarked", 1, "",
	)
	// The fence cleared both leases, so the capable owner claims the fenced
	// rows through the queue (either order) and completes them as a batch.
	batchIntents := make([]reducer.Intent, 0, 2)
	for range 2 {
		batched, ok, err := capableQueue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("claim fenced mixed batch = %+v ok=%t err=%v", batched, ok, err)
		}
		batchIntents = append(batchIntents, batched)
	}
	if err := capableQueue.AckBatch(ctx, batchIntents, nil); err != nil {
		t.Fatalf("capable mixed batch ACK: %v", err)
	}
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, "ack-5854-batch-marked", "succeeded", "",
	)
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, "ack-5854-batch-unmarked", "succeeded", "",
	)

	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, "ack-5854-reclaim", scopeReclaim, reclaimGen,
		legacyOwner, now.Add(-time.Minute), now.Add(-2*time.Minute),
	)
	insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeReclaim, reclaimGen)
	legacyResult, legacyErr = db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-reclaim",
		legacyOwner,
	)
	assertContainerImageIdentityLegacyAckFenced(
		t, ctx, db, "ack-5854-reclaim", legacyResult, legacyErr, 1, "pending",
	)
	reclaimQueue := ReducerQueue{
		database:      SQLDB{DB: db},
		LeaseOwner:    capableOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
		ClaimDomain:   reducer.DomainContainerImageIdentity,
	}
	claimed, ok, err := reclaimQueue.Claim(ctx)
	if err != nil {
		t.Fatalf("reclaim expired legacy lease: %v", err)
	}
	if !ok || claimed.IntentID != "ack-5854-reclaim" {
		t.Fatalf("reclaimed intent = %+v ok=%t, want ack-5854-reclaim", claimed, ok)
	}
	if err := reclaimQueue.Ack(ctx, claimed, reducer.Result{}); err != nil {
		t.Fatalf("attempt-bound ACK after lease reclaim: %v", err)
	}
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, "ack-5854-reclaim", "succeeded", "",
	)
}

func openContainerImageIdentityAckCapabilityProofDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the ACK attempt fence proof")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	schema := fmt.Sprintf("eshu_5854_ack_attempt_fence_%d", time.Now().UnixNano())
	adminDB := openActiveOCIWarningIndexProofDB(t, dsn)
	if _, err := adminDB.ExecContext(ctx, "CREATE SCHEMA "+quoteSQLIdentifier(schema)); err != nil {
		t.Fatalf("create ACK attempt fence schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(
			cleanupCtx,
			"DROP SCHEMA IF EXISTS "+quoteSQLIdentifier(schema)+" CASCADE",
		); err != nil {
			t.Errorf("drop ACK attempt fence schema: %v", err)
		}
	})
	db := openActiveOCIWarningIndexProofDB(
		t,
		activeOCIWarningIndexSchemaDSN(t, dsn, schema),
	)
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("apply ACK attempt fence schema: %v", err)
	}
	return db
}

func assertContainerImageIdentityLegacyAckRejected(
	t *testing.T,
	_ sql.Result,
	err error,
) {
	t.Helper()
	if err == nil || !strings.Contains(
		err.Error(),
		"fact_work_items_container_image_identity_v2_status_check",
	) {
		t.Fatalf("legacy ACK error = %v, want attempt-token constraint", err)
	}
	var sqlState interface{ SQLState() string }
	if !errors.As(err, &sqlState) || sqlState.SQLState() != "23514" {
		t.Fatalf("legacy ACK SQLSTATE = %v, want 23514", sqlState)
	}
}
