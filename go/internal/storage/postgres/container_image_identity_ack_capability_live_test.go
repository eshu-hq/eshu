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
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)

	const (
		scopeMarkedSingle  = "repository:5854-ack-marked-single"
		scopeUnmarked      = "repository:5854-ack-before-marker"
		scopeMarkedBatch   = "repository:5854-ack-marked-batch"
		scopeUnmarkedBatch = "repository:5854-ack-unmarked-batch"
		scopeReclaim       = "repository:5854-ack-reclaim"
		legacyOwner        = "legacy-reducer-5854"
		capableOwner       = "capable-reducer-5854"
		markedSingle       = "generation:5854-ack-marked-single"
		unmarked           = "generation:5854-ack-before-marker"
		markedBatch        = "generation:5854-ack-marked-batch"
		unmarkedBatch      = "generation:5854-ack-unmarked-batch"
		reclaimGen         = "generation:5854-ack-reclaim"
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
	assertContainerImageIdentityLegacyAckRejected(t, legacyResult, legacyErr)
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, "ack-5854-marked-single", "running", legacyOwner,
	)
	assertContainerImageIdentityAckFactCount(
		t, ctx, db, scopeMarkedSingle, markedSingle, "image_ref_v2", 1,
	)
	capableQueue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    capableOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	if _, err := db.ExecContext(
		ctx,
		`UPDATE fact_work_items SET lease_owner = $1 WHERE work_item_id = $2`,
		capableOwner,
		"ack-5854-marked-single",
	); err != nil {
		t.Fatalf("transfer marked single proof lease: %v", err)
	}
	if err := capableQueue.Ack(
		ctx,
		reducer.Intent{
			IntentID:     "ack-5854-marked-single",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		},
		reducer.Result{},
	); err != nil {
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
	if _, err := db.ExecContext(
		ctx,
		legacyContainerImageIdentityAckQuery,
		now,
		"ack-5854-before-marker",
		legacyOwner,
	); err != nil {
		t.Fatalf("legacy ACK before marker: %v", err)
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
		current.ClaimEpoch != 2 {
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
	assertContainerImageIdentityLegacyAckRejected(t, legacyResult, legacyErr)
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, "ack-5854-batch-marked", "running", legacyOwner,
	)
	assertContainerImageIdentityAckWorkItemState(
		t, ctx, db, "ack-5854-batch-unmarked", "claimed", legacyOwner,
	)
	if _, err := db.ExecContext(
		ctx,
		`UPDATE fact_work_items
		 SET lease_owner = $1
		 WHERE work_item_id IN ($2, $3)`,
		capableOwner,
		"ack-5854-batch-marked",
		"ack-5854-batch-unmarked",
	); err != nil {
		t.Fatalf("transfer mixed batch proof leases: %v", err)
	}
	if err := capableQueue.AckBatch(ctx, []reducer.Intent{
		{
			IntentID:     "ack-5854-batch-marked",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		},
		{
			IntentID:     "ack-5854-batch-unmarked",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		},
	}, nil); err != nil {
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
	assertContainerImageIdentityLegacyAckRejected(t, legacyResult, legacyErr)
	reclaimQueue := ReducerQueue{
		db:            SQLDB{DB: db},
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
