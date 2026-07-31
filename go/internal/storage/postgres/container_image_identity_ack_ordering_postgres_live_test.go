// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityAckMarkerOrderingLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)

	t.Run("marker commit makes blocked legacy ACK reject", func(t *testing.T) {
		scopeID, generationID, workItemID, owner := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "marker-first", now, true,
		)
		markerTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin marker-first transaction: %v", err)
		}
		defer func() { _ = markerTx.Rollback() }()
		insertContainerImageIdentityCutoverMarker(
			t, ctx, markerTx, scopeID, generationID,
		)

		ackDone := runContainerImageIdentityLegacyAckAsync(
			ctx, db, now, workItemID, owner,
		)
		assertContainerImageIdentityAckStillBlocked(t, ackDone, "marker commit")
		if err := markerTx.Commit(); err != nil {
			t.Fatalf("commit marker-first transaction: %v", err)
		}
		assertContainerImageIdentityOrderingLegacyAckRejected(t, ackDone)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, workItemID, "running", owner,
		)
	})

	t.Run("legacy ACK commit releases blocked marker", func(t *testing.T) {
		scopeID, generationID, workItemID, owner := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "ack-first", now, true,
		)
		ackTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin ACK-first transaction: %v", err)
		}
		defer func() { _ = ackTx.Rollback() }()
		if _, err := ackTx.ExecContext(
			ctx,
			legacyContainerImageIdentityAckQuery,
			now,
			workItemID,
			owner,
		); err != nil {
			t.Fatalf("execute ACK-first legacy ACK: %v", err)
		}

		markerDone := runContainerImageIdentityMarkerAsync(
			ctx, db, scopeID, generationID,
		)
		assertContainerImageIdentityAckStillBlocked(t, markerDone, "legacy ACK commit")
		if err := ackTx.Commit(); err != nil {
			t.Fatalf("commit ACK-first transaction: %v", err)
		}
		assertContainerImageIdentityOrderingOperationRejected(
			t,
			markerDone,
			"marker after legacy ACK",
			"first cutover requires the exact active claim epoch",
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, workItemID, "succeeded", "",
		)
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeID, generationID, 0,
		)
	})

	t.Run("marker rollback releases legacy ACK", func(t *testing.T) {
		scopeID, generationID, workItemID, owner := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "marker-rollback", now, true,
		)
		markerTx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin marker-rollback transaction: %v", err)
		}
		defer func() { _ = markerTx.Rollback() }()
		insertContainerImageIdentityCutoverMarker(
			t, ctx, markerTx, scopeID, generationID,
		)

		ackDone := runContainerImageIdentityLegacyAckAsync(
			ctx, db, now, workItemID, owner,
		)
		assertContainerImageIdentityAckStillBlocked(t, ackDone, "marker rollback")
		if err := markerTx.Rollback(); err != nil {
			t.Fatalf("roll back marker transaction: %v", err)
		}
		assertContainerImageIdentityOrderingOperationSucceeded(
			t, ackDone, "legacy ACK after marker rollback",
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, workItemID, "succeeded", "",
		)
	})

	t.Run("marker without stable work item fails closed", func(t *testing.T) {
		scopeID, generationID, _, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "late-work-item", now, false,
		)
		if _, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id, generation_id,
    activated_by_work_item_id, activated_by_claim_epoch
)
VALUES ($1, $2, 'missing-work-item', 1)
`, scopeID, generationID); err == nil || !strings.Contains(
			err.Error(),
			"requires the exact current claim epoch",
		) {
			t.Fatalf("marker without work item error = %v, want exact-epoch rejection", err)
		}
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeID, generationID, 0,
		)
	})

	t.Run("stale claim epoch cannot publish first marker", func(t *testing.T) {
		scopeID, generationID, workItemID, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "stale-first-marker", now, true,
		)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 2
WHERE work_item_id = $1
`, workItemID); err != nil {
			t.Fatalf("advance marker proof claim epoch: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id, generation_id,
    activated_by_work_item_id, activated_by_claim_epoch
)
VALUES ($1, $2, $3, 1)
`, scopeID, generationID, workItemID); err == nil ||
			!strings.Contains(err.Error(), "requires the exact current claim epoch") {
			t.Fatalf("stale first marker error = %v, want exact-epoch rejection", err)
		}
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeID, generationID, 0,
		)
	})

	t.Run("same epoch duplicate follows marker commit", func(t *testing.T) {
		scopeID, generationID, workItemID, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "same-epoch-commit", now, true,
		)
		first, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin first same-epoch marker: %v", err)
		}
		defer func() { _ = first.Rollback() }()
		insertContainerImageIdentityCutoverMarker(
			t, ctx, first, scopeID, generationID,
		)
		secondDone := runContainerImageIdentityMarkerForEpochAsync(
			ctx, db, scopeID, generationID, workItemID, 1,
		)
		assertContainerImageIdentityAckStillBlocked(
			t, secondDone, "first same-epoch marker commit",
		)
		if err := first.Commit(); err != nil {
			t.Fatalf("commit first same-epoch marker: %v", err)
		}
		assertContainerImageIdentityOrderingOperationSucceeded(
			t, secondDone, "same-epoch duplicate after marker commit",
		)
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeID, generationID, 1,
		)
	})

	t.Run("same epoch duplicate becomes activator after rollback", func(t *testing.T) {
		scopeID, generationID, workItemID, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "same-epoch-rollback", now, true,
		)
		first, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin rolled-back same-epoch marker: %v", err)
		}
		defer func() { _ = first.Rollback() }()
		insertContainerImageIdentityCutoverMarker(
			t, ctx, first, scopeID, generationID,
		)
		secondDone := runContainerImageIdentityMarkerForEpochAsync(
			ctx, db, scopeID, generationID, workItemID, 1,
		)
		assertContainerImageIdentityAckStillBlocked(
			t, secondDone, "first same-epoch marker rollback",
		)
		if err := first.Rollback(); err != nil {
			t.Fatalf("roll back first same-epoch marker: %v", err)
		}
		assertContainerImageIdentityOrderingOperationSucceeded(
			t, secondDone, "same-epoch duplicate after marker rollback",
		)
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeID, generationID, 1,
		)
	})

	t.Run("existing marker accepts a later active claim epoch", func(t *testing.T) {
		scopeID, generationID, workItemID, owner := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "existing-marker-later-epoch", now, true,
		)
		insertContainerImageIdentityCutoverMarker(
			t, ctx, db, scopeID, generationID,
		)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running',
    lease_owner = $2,
    claim_until = $3,
    container_image_identity_claim_epoch = 2,
    container_image_identity_v2_authorized_status = 'running'
WHERE work_item_id = $1
`, workItemID, owner, now.Add(time.Minute)); err != nil {
			t.Fatalf("advance existing-marker work item epoch: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id, generation_id,
    activated_by_work_item_id, activated_by_claim_epoch
)
VALUES ($1, $2, $3, 2)
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeID, generationID, workItemID); err != nil {
			t.Fatalf("existing marker with later active epoch: %v", err)
		}
		var activatedEpoch int64
		if err := db.QueryRowContext(ctx, `
SELECT activated_by_claim_epoch
FROM container_image_identity_cutovers
WHERE scope_id = $1
  AND generation_id = $2
`, scopeID, generationID).Scan(&activatedEpoch); err != nil {
			t.Fatalf("read immutable activation epoch: %v", err)
		}
		if activatedEpoch != 1 {
			t.Fatalf("activation epoch = %d, want immutable first epoch 1", activatedEpoch)
		}
	})

	t.Run("second distinct marker key aborts transaction", func(t *testing.T) {
		scopeOne, generationOne, _, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "marker-key-one", now, true,
		)
		scopeTwo, generationTwo, workItemTwo, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "marker-key-two", now, true,
		)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin multi-key marker transaction: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		insertContainerImageIdentityCutoverMarker(
			t, ctx, tx, scopeOne, generationOne,
		)
		insertContainerImageIdentityCutoverMarker(
			t, ctx, tx, scopeOne, generationOne,
		)
		if _, err := tx.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id, generation_id,
    activated_by_work_item_id, activated_by_claim_epoch
)
VALUES ($1, $2, $3, 1)
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeTwo, generationTwo, workItemTwo); err == nil ||
			!strings.Contains(err.Error(), "one scope generation per transaction") {
			t.Fatalf("second distinct marker key error = %v, want explicit rejection", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("roll back distinct marker-key transaction: %v", err)
		}
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeOne, generationOne, 0,
		)
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeTwo, generationTwo, 0,
		)
	})

	t.Run("multi-row distinct marker keys abort atomically", func(t *testing.T) {
		scopeOne, generationOne, workItemOne, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "marker-multi-one", now, true,
		)
		scopeTwo, generationTwo, workItemTwo, _ := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "marker-multi-two", now, true,
		)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin multi-row marker transaction: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO container_image_identity_cutovers (
    scope_id, generation_id,
    activated_by_work_item_id, activated_by_claim_epoch
)
VALUES ($1, $2, $3, 1), ($4, $5, $6, 1)
ON CONFLICT (scope_id, generation_id) DO NOTHING
`, scopeOne, generationOne, workItemOne, scopeTwo, generationTwo, workItemTwo); err == nil ||
			!strings.Contains(err.Error(), "one scope generation per transaction") {
			_ = tx.Rollback()
			t.Fatalf("multi-row distinct marker key error = %v, want explicit rejection", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("roll back multi-row marker transaction: %v", err)
		}
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeOne, generationOne, 0,
		)
		assertContainerImageIdentityAckOrderingMarkerCount(
			t, ctx, db, scopeTwo, generationTwo, 0,
		)
	})

	t.Run("attempt-bound ACK bypasses advisory lock", func(t *testing.T) {
		scopeID, generationID, workItemID, owner := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "capable-bypass", now, true,
		)
		blocker, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin capable-bypass advisory blocker: %v", err)
		}
		defer func() { _ = blocker.Rollback() }()
		if _, err := blocker.ExecContext(ctx, `
SELECT pg_advisory_xact_lock(
    hashtextextended($1 || E'\x1f' || $2, 5854)
)
`, scopeID, generationID); err != nil {
			t.Fatalf("hold capable-bypass advisory lock: %v", err)
		}
		queue := ReducerQueue{
			db:            SQLDB{DB: db},
			LeaseOwner:    owner,
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now },
		}
		done := make(chan error, 1)
		go func() {
			done <- queue.Ack(
				ctx,
				reducer.Intent{
					IntentID:     workItemID,
					Domain:       reducer.DomainContainerImageIdentity,
					AttemptCount: 1,
					ClaimEpoch:   1,
				},
				reducer.Result{},
			)
		}()
		assertContainerImageIdentityOrderingOperationSucceeded(
			t, done, "attempt-bound ACK behind advisory blocker",
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, workItemID, "succeeded", "",
		)
	})

	t.Run("isolation fails legacy closed but permits attempt-bound ACK", func(t *testing.T) {
		legacyScopeID, legacyGenerationID, legacyWorkItemID, legacyOwner := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "isolation-legacy", now, true,
		)
		insertContainerImageIdentityCutoverMarker(
			t, ctx, db, legacyScopeID, legacyGenerationID,
		)
		legacyTx, err := db.BeginTx(
			ctx,
			&sql.TxOptions{Isolation: sql.LevelRepeatableRead},
		)
		if err != nil {
			t.Fatalf("begin repeatable-read legacy ACK: %v", err)
		}
		if _, err := legacyTx.ExecContext(
			ctx,
			legacyContainerImageIdentityAckQuery,
			now,
			legacyWorkItemID,
			legacyOwner,
		); err == nil {
			_ = legacyTx.Rollback()
			t.Fatalf("repeatable-read legacy ACK error = nil, want constraint rejection")
		}
		if err := legacyTx.Rollback(); err != nil {
			t.Fatalf("roll back repeatable-read legacy ACK: %v", err)
		}
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, legacyWorkItemID, "running", legacyOwner,
		)

		capableScopeID, capableGenerationID, capableWorkItemID, capableOwner := seedContainerImageIdentityAckOrderingScenario(
			t, ctx, db, "isolation-capable", now, true,
		)
		insertContainerImageIdentityCutoverMarker(
			t, ctx, db, capableScopeID, capableGenerationID,
		)
		capableTx, err := db.BeginTx(
			ctx,
			&sql.TxOptions{Isolation: sql.LevelRepeatableRead},
		)
		if err != nil {
			t.Fatalf("begin repeatable-read attempt-bound ACK: %v", err)
		}
		queue := ReducerQueue{
			db:            SQLTx{Tx: capableTx},
			LeaseOwner:    capableOwner,
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now },
		}
		if err := queue.Ack(
			ctx,
			reducer.Intent{
				IntentID:     capableWorkItemID,
				Domain:       reducer.DomainContainerImageIdentity,
				AttemptCount: 1,
				ClaimEpoch:   1,
			},
			reducer.Result{},
		); err != nil {
			_ = capableTx.Rollback()
			t.Fatalf("repeatable-read attempt-bound ACK: %v", err)
		}
		if err := capableTx.Commit(); err != nil {
			t.Fatalf("commit repeatable-read attempt-bound ACK: %v", err)
		}
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, capableWorkItemID, "succeeded", "",
		)
	})
}
