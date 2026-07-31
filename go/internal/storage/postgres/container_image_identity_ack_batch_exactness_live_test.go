// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityAckBatchAttemptExactnessLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 19, 0, 0, 0, time.UTC)

	for _, batchSize := range []int{1, 16, 64} {
		t.Run(fmt.Sprintf("target batch %d", batchSize), func(t *testing.T) {
			owner := fmt.Sprintf("reducer-5854-ack-exact-%d", batchSize)

			intents := make([]reducer.Intent, batchSize)
			for index := range batchSize {
				scopeID := fmt.Sprintf(
					"repository:5854-ack-exact-%d-%02d",
					batchSize,
					index,
				)
				generationID := fmt.Sprintf(
					"generation:5854-ack-exact-%d-%02d",
					batchSize,
					index,
				)
				workItemID := fmt.Sprintf(
					"ack-5854-exact-%d-%02d",
					batchSize,
					index,
				)
				seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
				seedContainerImageIdentityAckGeneration(
					t,
					ctx,
					db,
					scopeID,
					generationID,
				)
				seedContainerImageIdentityAckWorkItem(
					t,
					ctx,
					db,
					workItemID,
					scopeID,
					generationID,
					owner,
					now.Add(time.Minute),
					now,
				)
				insertContainerImageIdentityCutoverMarker(
					t,
					ctx,
					db,
					scopeID,
					generationID,
				)
				intents[index] = reducer.Intent{
					IntentID:     workItemID,
					Domain:       reducer.DomainContainerImageIdentity,
					AttemptCount: 1,
					ClaimEpoch:   1,
				}
			}

			queue := ReducerQueue{
				db:            SQLDB{DB: db},
				LeaseOwner:    owner,
				LeaseDuration: time.Minute,
				Now:           func() time.Time { return now },
			}
			if err := queue.AckBatch(ctx, intents, nil); err != nil {
				t.Fatalf("AckBatch(%d) error = %v", batchSize, err)
			}
			for _, intent := range intents {
				assertContainerImageIdentityAckWorkItemState(
					t,
					ctx,
					db,
					intent.IntentID,
					"succeeded",
					"",
				)
			}
		})
	}

	t.Run("mixed domain and nonmatching rows", func(t *testing.T) {
		const (
			targetScope     = "repository:5854-ack-exact-mixed-target"
			targetGen       = "generation:5854-ack-exact-mixed-target"
			unrelatedScope  = "repository:5854-ack-exact-mixed-unrelated"
			unrelatedGen    = "generation:5854-ack-exact-mixed-unrelated"
			owner           = "reducer-5854-ack-exact-mixed"
			targetID        = "ack-5854-exact-mixed-target"
			unrelatedID     = "ack-5854-exact-mixed-unrelated"
			wrongOwnerID    = "ack-5854-exact-wrong-owner"
			wrongStageID    = "ack-5854-exact-wrong-stage"
			wrongStatusID   = "ack-5854-exact-wrong-status"
			wrongAttemptID  = "ack-5854-exact-wrong-attempt"
			missingID       = "ack-5854-exact-missing"
			otherLeaseOwner = "reducer-5854-ack-exact-other"
		)
		for _, fixture := range []struct {
			scopeID      string
			generationID string
		}{
			{targetScope, targetGen},
			{unrelatedScope, unrelatedGen},
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
		for _, fixture := range []struct {
			workItemID string
			scopeID    string
			generation string
			leaseOwner string
		}{
			{targetID, targetScope, targetGen, owner},
			{unrelatedID, unrelatedScope, unrelatedGen, owner},
			{wrongOwnerID, unrelatedScope, unrelatedGen, otherLeaseOwner},
			{wrongStageID, unrelatedScope, unrelatedGen, owner},
			{wrongStatusID, unrelatedScope, unrelatedGen, owner},
			{wrongAttemptID, unrelatedScope, unrelatedGen, owner},
		} {
			seedContainerImageIdentityAckWorkItem(
				t,
				ctx,
				db,
				fixture.workItemID,
				fixture.scopeID,
				fixture.generation,
				fixture.leaseOwner,
				now.Add(time.Minute),
				now,
			)
		}
		insertContainerImageIdentityCutoverMarker(
			t,
			ctx,
			db,
			targetScope,
			targetGen,
		)
		if _, err := db.ExecContext(
			ctx,
			`UPDATE fact_work_items
			 SET domain = 'ownership'
			 WHERE work_item_id <> $1
			   AND work_item_id = ANY($2::text[])`,
			targetID,
			[]string{
				unrelatedID,
				wrongOwnerID,
				wrongStageID,
				wrongStatusID,
				wrongAttemptID,
			},
		); err != nil {
			t.Fatalf("mark mixed-domain rows: %v", err)
		}
		if _, err := db.ExecContext(
			ctx,
			`UPDATE fact_work_items
			 SET stage = 'projector'
			 WHERE work_item_id = $1`,
			wrongStageID,
		); err != nil {
			t.Fatalf("mark wrong-stage row: %v", err)
		}
		if _, err := db.ExecContext(
			ctx,
			`UPDATE fact_work_items
			 SET status = 'pending',
			     lease_owner = NULL,
			     claim_until = NULL
			 WHERE work_item_id = $1`,
			wrongStatusID,
		); err != nil {
			t.Fatalf("mark wrong-status row: %v", err)
		}

		queue := ReducerQueue{
			db:            SQLDB{DB: db},
			LeaseOwner:    owner,
			LeaseDuration: time.Minute,
			Now:           func() time.Time { return now },
		}
		intents := []reducer.Intent{
			{
				IntentID: unrelatedID, Domain: reducer.DomainOwnership,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: wrongOwnerID, Domain: reducer.DomainOwnership,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: wrongStageID, Domain: reducer.DomainOwnership,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: wrongStatusID, Domain: reducer.DomainOwnership,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: wrongAttemptID, Domain: reducer.DomainOwnership,
				AttemptCount: 2, ClaimEpoch: 2,
			},
			{
				IntentID: missingID, Domain: reducer.DomainOwnership,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: targetID, Domain: reducer.DomainContainerImageIdentity,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: targetID, Domain: reducer.DomainContainerImageIdentity,
				AttemptCount: 1, ClaimEpoch: 1,
			},
		}
		if err := queue.AckBatch(ctx, intents, nil); err != nil {
			t.Fatalf("mixed AckBatch() error = %v", err)
		}

		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, targetID, "succeeded", "",
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, unrelatedID, "succeeded", "",
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, wrongOwnerID, "claimed", otherLeaseOwner,
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, wrongStageID, "claimed", owner,
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, wrongStatusID, "pending", "",
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, wrongAttemptID, "succeeded", "",
		)
		assertContainerImageIdentityAckFactCount(
			t,
			ctx,
			db,
			targetScope,
			targetGen,
			"image_ref_v2",
			0,
		)
	})

	t.Run("all stale target pairs reject", func(t *testing.T) {
		const (
			scopeID      = "repository:5854-ack-all-stale"
			generation   = "generation:5854-ack-all-stale"
			workItemID   = "ack-5854-all-stale"
			leaseOwner   = "reducer-5854-all-stale"
			currentEpoch = int64(2)
		)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, workItemID, scopeID, generation,
			leaseOwner, now.Add(time.Minute), now,
		)
		insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, generation)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = $2
WHERE work_item_id = $1
`, workItemID, currentEpoch); err != nil {
			t.Fatalf("advance all-stale batch epoch: %v", err)
		}
		queue := ReducerQueue{
			db: SQLDB{DB: db}, LeaseOwner: leaseOwner,
			LeaseDuration: time.Minute, Now: func() time.Time { return now },
		}
		err := queue.AckBatch(ctx, []reducer.Intent{{
			IntentID: workItemID, Domain: reducer.DomainContainerImageIdentity,
			AttemptCount: 1, ClaimEpoch: 1,
		}}, nil)
		if !errors.Is(err, ErrReducerClaimRejected) {
			t.Fatalf("all-stale target AckBatch error = %v, want claim rejection", err)
		}
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, workItemID, "running", leaseOwner,
		)
	})

	t.Run("valid unrelated row does not mask stale target", func(t *testing.T) {
		const (
			targetScope = "repository:5854-ack-stale-target"
			targetGen   = "generation:5854-ack-stale-target"
			targetID    = "ack-5854-stale-target"
			otherScope  = "repository:5854-ack-valid-unrelated"
			otherGen    = "generation:5854-ack-valid-unrelated"
			otherID     = "ack-5854-valid-unrelated"
			leaseOwner  = "reducer-5854-stale-target"
		)
		for _, fixture := range []struct {
			scopeID      string
			generationID string
		}{
			{targetScope, targetGen},
			{otherScope, otherGen},
		} {
			seedContainerImageIdentityAckScope(t, ctx, db, fixture.scopeID)
			seedContainerImageIdentityAckGeneration(
				t, ctx, db, fixture.scopeID, fixture.generationID,
			)
		}
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, targetID, targetScope, targetGen,
			leaseOwner, now.Add(time.Minute), now,
		)
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, otherID, otherScope, otherGen,
			leaseOwner, now.Add(time.Minute), now,
		)
		insertContainerImageIdentityCutoverMarker(t, ctx, db, targetScope, targetGen)
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 2
WHERE work_item_id = $1
`, targetID); err != nil {
			t.Fatalf("advance stale-target epoch: %v", err)
		}
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET domain = 'ownership'
WHERE work_item_id = $1
`, otherID); err != nil {
			t.Fatalf("mark valid unrelated batch row: %v", err)
		}
		queue := ReducerQueue{
			db: SQLDB{DB: db}, LeaseOwner: leaseOwner,
			LeaseDuration: time.Minute, Now: func() time.Time { return now },
		}
		err := queue.AckBatch(ctx, []reducer.Intent{
			{
				IntentID: targetID, Domain: reducer.DomainContainerImageIdentity,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: otherID, Domain: reducer.DomainOwnership,
				AttemptCount: 1,
			},
		}, nil)
		if !errors.Is(err, ErrReducerClaimRejected) {
			t.Fatalf("stale-target mixed AckBatch error = %v, want claim rejection", err)
		}
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, targetID, "running", leaseOwner,
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, otherID, "succeeded", "",
		)
	})

	t.Run("one valid target preserves partial success", func(t *testing.T) {
		const (
			validScope = "repository:5854-ack-partial-valid"
			validGen   = "generation:5854-ack-partial-valid"
			validID    = "ack-5854-partial-valid"
			staleScope = "repository:5854-ack-partial-stale"
			staleGen   = "generation:5854-ack-partial-stale"
			staleID    = "ack-5854-partial-stale"
			leaseOwner = "reducer-5854-partial"
		)
		for _, fixture := range []struct {
			scopeID      string
			generationID string
			workItemID   string
		}{
			{validScope, validGen, validID},
			{staleScope, staleGen, staleID},
		} {
			seedContainerImageIdentityAckScope(t, ctx, db, fixture.scopeID)
			seedContainerImageIdentityAckGeneration(
				t, ctx, db, fixture.scopeID, fixture.generationID,
			)
			seedContainerImageIdentityAckWorkItem(
				t, ctx, db, fixture.workItemID, fixture.scopeID,
				fixture.generationID, leaseOwner, now.Add(time.Minute), now,
			)
			insertContainerImageIdentityCutoverMarker(
				t, ctx, db, fixture.scopeID, fixture.generationID,
			)
		}
		if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET container_image_identity_claim_epoch = 2
WHERE work_item_id = $1
`, staleID); err != nil {
			t.Fatalf("advance partial stale epoch: %v", err)
		}
		queue := ReducerQueue{
			db: SQLDB{DB: db}, LeaseOwner: leaseOwner,
			LeaseDuration: time.Minute, Now: func() time.Time { return now },
		}
		if err := queue.AckBatch(ctx, []reducer.Intent{
			{
				IntentID: validID, Domain: reducer.DomainContainerImageIdentity,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: staleID, Domain: reducer.DomainContainerImageIdentity,
				AttemptCount: 1, ClaimEpoch: 1,
			},
		}, nil); err != nil {
			t.Fatalf("partial target AckBatch error = %v, want valid pair committed", err)
		}
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, validID, "succeeded", "",
		)
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, staleID, "running", leaseOwner,
		)
	})

	t.Run("conflicting duplicate epochs reject input", func(t *testing.T) {
		const (
			scopeID    = "repository:5854-ack-conflicting-duplicate"
			generation = "generation:5854-ack-conflicting-duplicate"
			workItemID = "ack-5854-conflicting-duplicate"
			leaseOwner = "reducer-5854-conflicting-duplicate"
		)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, workItemID, scopeID, generation,
			leaseOwner, now.Add(time.Minute), now,
		)
		insertContainerImageIdentityCutoverMarker(t, ctx, db, scopeID, generation)
		queue := ReducerQueue{
			db: SQLDB{DB: db}, LeaseOwner: leaseOwner,
			LeaseDuration: time.Minute, Now: func() time.Time { return now },
		}
		err := queue.AckBatch(ctx, []reducer.Intent{
			{
				IntentID: workItemID, Domain: reducer.DomainContainerImageIdentity,
				AttemptCount: 1, ClaimEpoch: 1,
			},
			{
				IntentID: workItemID, Domain: reducer.DomainContainerImageIdentity,
				AttemptCount: 2, ClaimEpoch: 2,
			},
		}, nil)
		if err == nil || !strings.Contains(err.Error(), "conflicting claim epochs") {
			t.Fatalf("conflicting duplicate AckBatch error = %v", err)
		}
		assertContainerImageIdentityAckWorkItemState(
			t, ctx, db, workItemID, "running", leaseOwner,
		)
	})
}
