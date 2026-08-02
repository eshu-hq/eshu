// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerAckCoalescesPendingCompletionEventsPerDomainLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	const owner = "coalesced-ack-owner"
	queue := ReducerQueue{
		db: SQLDB{DB: db}, LeaseOwner: owner,
		LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}

	for index := range 2 {
		scopeID := fmt.Sprintf("repository:5740-coalesced-ack-%d", index)
		generationID := fmt.Sprintf("generation:5740-coalesced-ack-%d", index)
		workItemID := fmt.Sprintf("reducer_5740_coalesced_ack_%d", index)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, workItemID, scopeID, generationID,
			owner, now.Add(time.Minute), now,
		)
		if err := queue.Ack(ctx, reducer.Intent{
			IntentID: workItemID, Domain: reducer.DomainContainerImageIdentity, ClaimEpoch: 1,
		}, reducer.Result{}); err != nil {
			t.Fatalf("ACK coalesced producer %d: %v", index, err)
		}
	}

	var events int
	var items int64
	var visibleAt time.Time
	var createdAt time.Time
	if err := db.QueryRowContext(ctx, `
SELECT count(*), COALESCE(sum(producer_item_count), 0), max(visible_at), min(created_at)
FROM cross_scope_completion_events
WHERE producer_domain = 'container_image_identity'
  AND status = 'pending'
`).Scan(&events, &items, &visibleAt, &createdAt); err != nil {
		t.Fatalf("read coalesced completion event: %v", err)
	}
	if events != 1 || items != 2 {
		t.Fatalf("pending completion aggregate = events:%d items:%d, want 1/2", events, items)
	}
	if visibleAt.Before(createdAt.Add(250*time.Millisecond)) ||
		visibleAt.After(createdAt.Add(2*time.Second)) {
		t.Fatalf("pending completion window = created:%s visible:%s, want 250ms..2s", createdAt, visibleAt)
	}
}

func TestReducerAckEventInsertFailureRollsBackProducerSuccessLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 2, 12, 30, 0, 0, time.UTC)
	const (
		scopeID    = "repository:5740-ack-event-rollback"
		generation = "generation:5740-ack-event-rollback"
		workItemID = "reducer_5740_ack_event_rollback"
		owner      = "ack-event-rollback-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, workItemID, scopeID, generation,
		owner, now.Add(time.Minute), now,
	)
	if _, err := db.ExecContext(ctx, `
CREATE FUNCTION reject_completion_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'synthetic completion event rejection';
END
$$;
CREATE TRIGGER reject_completion_event
BEFORE INSERT ON cross_scope_completion_events
FOR EACH ROW EXECUTE FUNCTION reject_completion_event()
`); err != nil {
		t.Fatalf("install completion-event rejection: %v", err)
	}
	queue := ReducerQueue{
		db: SQLDB{DB: db}, LeaseOwner: owner,
		LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}
	if err := queue.Ack(ctx, reducer.Intent{
		IntentID: workItemID, Domain: reducer.DomainContainerImageIdentity, ClaimEpoch: 1,
	}, reducer.Result{}); err == nil {
		t.Fatal("ACK error = nil, want completion-event insert failure")
	}
	assertContainerImageIdentityAckWorkItemState(t, ctx, db, workItemID, "claimed", owner)
}
