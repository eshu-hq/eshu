// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestCrossScopeCompletionExpiredLeaseIsRootOfBoundedFanoutLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 2, 10, 0, 0, 0, time.UTC)

	pendingID := insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"pending", "", time.Time{}, 0, now.Add(-time.Minute),
	)
	expiredID := insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"claimed", "expired-owner", now.Add(-time.Second), 7, now,
	)
	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return now }

	lease, ok, err := store.Claim(ctx, "takeover-owner", time.Minute)
	if err != nil || !ok {
		t.Fatalf("take over expired completion lease = %+v ok=%t err=%v", lease, ok, err)
	}
	if lease.EventID != expiredID || lease.ClaimEpoch != 8 {
		t.Fatalf("takeover lease = %+v, want event=%d epoch=8", lease, expiredID)
	}
	stale := reducer.CrossScopeCompletionLease{
		EventID:        expiredID,
		ProducerDomain: reducer.DomainContainerImageIdentity,
		LeaseOwner:     "expired-owner",
		ClaimEpoch:     7,
	}
	if err := store.Heartbeat(ctx, stale, time.Minute); !errors.Is(err, ErrCrossScopeCompletionClaimRejected) {
		t.Fatalf("stale heartbeat error = %v, want claim rejected", err)
	}
	if _, err := store.Fanout(ctx, stale, 1); !errors.Is(err, ErrCrossScopeCompletionClaimRejected) {
		t.Fatalf("stale fanout error = %v, want claim rejected", err)
	}

	result, err := store.Fanout(ctx, lease, 1)
	if err != nil {
		t.Fatalf("bounded takeover fanout: %v", err)
	}
	if result.EventsProcessed != 1 {
		t.Fatalf("bounded takeover fanout = %+v, want one event", result)
	}
	assertCrossScopeCompletionEventState(t, ctx, db, expiredID, "")
	assertCrossScopeCompletionEventState(t, ctx, db, pendingID, "pending")
}

func TestCrossScopeCompletionConcurrentClaimHasOneLiveOwnerPerDomain(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 2, 10, 30, 0, 0, time.UTC)
	insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainCICDRunCorrelation,
		"pending", "", time.Time{}, 0, now,
	)

	start := make(chan struct{})
	results := make(chan bool, 16)
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for index := range 16 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			<-start
			store := NewCrossScopeCompletionStore(SQLDB{DB: db})
			store.Now = func() time.Time { return now }
			_, ok, err := store.Claim(ctx, fmt.Sprintf("claim-worker-%d", worker), time.Minute)
			results <- ok
			errs <- err
		}(index)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)

	claimed := 0
	for ok := range results {
		if ok {
			claimed++
		}
	}
	for err := range errs {
		if err != nil {
			t.Errorf("concurrent completion claim: %v", err)
		}
	}
	if claimed != 1 {
		t.Fatalf("successful concurrent claims = %d, want 1", claimed)
	}
}

func TestCrossScopeCompletionRetryMergesPendingAndStaysBoundedLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	base := time.Now().UTC().Add(-time.Minute)
	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return base }
	insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"pending", "", time.Time{}, 0, base.Add(-time.Second),
	)

	for attempt := range 4 {
		store.Now = func() time.Time { return base.Add(time.Duration(attempt) * time.Minute) }
		lease, ok, err := store.Claim(ctx, "bounded-retry-owner", time.Minute)
		if err != nil || !ok {
			t.Fatalf("claim bounded retry %d = %+v ok=%t err=%v", attempt, lease, ok, err)
		}
		insertCrossScopeCompletionEvent(
			t, ctx, db, reducer.DomainContainerImageIdentity,
			"pending", "", time.Time{}, 0, store.now(),
		)
		visibleAt := store.now().Add(30 * time.Second)
		if err := store.Retry(ctx, lease, errors.New("synthetic bounded retry"), visibleAt); err != nil {
			t.Fatalf("merge bounded retry %d: %v", attempt, err)
		}
		var rows int
		var status string
		var itemCount int64
		var gotVisible time.Time
		if err := db.QueryRowContext(ctx, `
SELECT count(*), min(status), sum(producer_item_count), max(visible_at)
FROM cross_scope_completion_events
WHERE producer_domain = 'container_image_identity'
`).Scan(&rows, &status, &itemCount, &gotVisible); err != nil {
			t.Fatalf("read bounded retry %d: %v", attempt, err)
		}
		if rows != 1 || status != "retrying" || itemCount != int64(attempt+2) || !gotVisible.Equal(visibleAt) {
			t.Fatalf(
				"bounded retry %d = rows:%d status:%s items:%d visible:%s, want 1/retrying/%d/%s",
				attempt, rows, status, itemCount, gotVisible, attempt+2, visibleAt,
			)
		}
	}
}

func TestCrossScopeCompletionFanoutRollsBackEventAndIntentTogetherLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 2, 11, 0, 0, 0, time.UTC)
	const (
		scopeID    = "repository:5740-atomic-fanout"
		generation = "generation:5740-atomic-fanout"
		baseSupply = "reducer_5740_atomic_supply"
		leaseOwner = "atomic-fanout-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate atomic fanout scope: %v", err)
	}
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, baseSupply, scopeID, generation,
		reducer.DomainSupplyChainImpact, now,
	)
	eventID := insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"pending", "", time.Time{}, 0, now,
	)
	if _, err := db.ExecContext(ctx, `
CREATE FUNCTION reject_cross_scope_schedule() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.status = 'pending' OR NEW.cross_scope_replay_required THEN
        RAISE EXCEPTION 'synthetic schedule rejection';
    END IF;
    RETURN NEW;
END
$$;
CREATE TRIGGER reject_cross_scope_schedule
BEFORE UPDATE ON fact_work_items
FOR EACH ROW EXECUTE FUNCTION reject_cross_scope_schedule()
`); err != nil {
		t.Fatalf("install synthetic schedule rejection: %v", err)
	}

	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return now }
	lease, ok, err := store.Claim(ctx, leaseOwner, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim atomic fanout = %+v ok=%t err=%v", lease, ok, err)
	}
	if _, err := store.Fanout(ctx, lease, 500); err == nil {
		t.Fatal("fanout error = nil, want synthetic insert failure")
	}
	assertCrossScopeCompletionEventState(t, ctx, db, eventID, "claimed")
	assertCrossScopeConsumerState(t, ctx, db, baseSupply, "succeeded", false)
	if _, err := db.ExecContext(ctx, "DROP TRIGGER reject_cross_scope_schedule ON fact_work_items"); err != nil {
		t.Fatalf("drop synthetic schedule rejection: %v", err)
	}
	if _, err := store.Fanout(ctx, lease, 500); err != nil {
		t.Fatalf("fanout after removing synthetic failure: %v", err)
	}
	assertCrossScopeCompletionEventState(t, ctx, db, eventID, "")
	assertCrossScopeConsumerState(t, ctx, db, baseSupply, "pending", false)
}

func TestReducerAckBatchAppendsOneCompletionEventPerProducerDomainLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 2, 11, 30, 0, 0, time.UTC)
	const owner = "batch-completion-owner"

	intents := make([]reducer.Intent, 0, 4)
	for index := range 2 {
		scopeID := fmt.Sprintf("repository:5740-batch-identity-%d", index)
		generation := fmt.Sprintf("generation:5740-batch-identity-%d", index)
		workItemID := fmt.Sprintf("reducer_5740_batch_identity_%d", index)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
		seedContainerImageIdentityAckWorkItem(
			t, ctx, db, workItemID, scopeID, generation,
			owner, now.Add(time.Minute), now,
		)
		intents = append(intents, reducer.Intent{
			IntentID: workItemID, Domain: reducer.DomainContainerImageIdentity, ClaimEpoch: 1,
		})
	}
	for index := range 2 {
		scopeID := fmt.Sprintf("repository:5740-batch-cicd-%d", index)
		generation := fmt.Sprintf("generation:5740-batch-cicd-%d", index)
		workItemID := fmt.Sprintf("reducer_5740_batch_cicd_%d", index)
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
		insertCrossScopeCompletionRunningProducer(
			t, ctx, db, workItemID, scopeID, generation,
			reducer.DomainCICDRunCorrelation, owner, now,
		)
		intents = append(intents, reducer.Intent{
			IntentID: workItemID, Domain: reducer.DomainCICDRunCorrelation,
		})
	}
	queue := ReducerQueue{
		db: SQLDB{DB: db}, LeaseOwner: owner,
		LeaseDuration: time.Minute, Now: func() time.Time { return now },
	}
	if err := queue.AckBatch(ctx, intents, make([]reducer.Result, len(intents))); err != nil {
		t.Fatalf("batch ACK completion producers: %v", err)
	}
	for _, domain := range []reducer.Domain{
		reducer.DomainContainerImageIdentity,
		reducer.DomainCICDRunCorrelation,
	} {
		var eventCount, itemCount int
		if err := db.QueryRowContext(ctx, `
SELECT count(*), COALESCE(max(producer_item_count), 0)
FROM cross_scope_completion_events
WHERE producer_domain = $1
`, domain).Scan(&eventCount, &itemCount); err != nil {
			t.Fatalf("read %s batch completion event: %v", domain, err)
		}
		if eventCount != 1 || itemCount != 2 {
			t.Fatalf("%s batch event = events:%d items:%d, want 1/2", domain, eventCount, itemCount)
		}
	}
}

func insertCrossScopeCompletionEvent(
	t *testing.T,
	ctx context.Context,
	db interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	domain reducer.Domain,
	status string,
	leaseOwner string,
	claimUntil time.Time,
	claimEpoch int64,
	createdAt time.Time,
) int64 {
	t.Helper()
	var eventID int64
	var owner any
	var until any
	if leaseOwner != "" {
		owner = leaseOwner
	}
	if !claimUntil.IsZero() {
		until = claimUntil
	}
	if err := db.QueryRowContext(ctx, `
INSERT INTO cross_scope_completion_events (
    producer_domain, producer_item_count,
    status, lease_owner, claim_until, claim_epoch, created_at, updated_at
) VALUES ($1, 1, $2, $3, $4, $5, $6, $6)
RETURNING event_id
`, domain, status, owner, until, claimEpoch, createdAt).Scan(&eventID); err != nil {
		t.Fatalf("insert %s completion event: %v", domain, err)
	}
	return eventID
}

func assertCrossScopeCompletionEventState(
	t *testing.T,
	ctx context.Context,
	db interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	eventID int64,
	wantStatus string,
) {
	t.Helper()
	var status string
	err := db.QueryRowContext(ctx, `
SELECT status
FROM cross_scope_completion_events
WHERE event_id = $1
`, eventID).Scan(&status)
	if wantStatus == "" {
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("completion event %d error = %v, want deleted", eventID, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("read completion event %d: %v", eventID, err)
	}
	if status != wantStatus {
		t.Fatalf("completion event %d status = %s, want %s", eventID, status, wantStatus)
	}
}

func insertCrossScopeCompletionRunningProducer(
	t *testing.T,
	ctx context.Context,
	db interface {
		ExecContext(context.Context, string, ...any) (sql.Result, error)
	},
	workItemID string,
	scopeID string,
	generationID string,
	domain reducer.Domain,
	leaseOwner string,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, lease_owner, claim_until,
    payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', $4, 'running',
          'intent', $1, $5, $6, '{}'::jsonb, $7, $7)
`, workItemID, scopeID, generationID, domain, leaseOwner, now.Add(time.Minute), now); err != nil {
		t.Fatalf("insert %s running producer: %v", domain, err)
	}
}
