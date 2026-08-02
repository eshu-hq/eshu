// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestCrossScopeCompletionIdentityThenCICDConvergesLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Date(2026, time.August, 1, 20, 30, 0, 0, time.UTC)

	const (
		producerScope = "repository:5740-completion-producer"
		producerGen   = "generation:5740-completion-producer"
		consumerScope = "repository:5740-completion-consumer"
		consumerGen   = "generation:5740-completion-consumer"
		identityID    = "reducer_5740_completion_identity"
		cicdID        = "reducer_5740_completion_cicd"
		supplyID      = "reducer_5740_completion_supply"
		leaseOwner    = "reducer-5740-completion"
		fanoutOwner   = "fanout-5740-completion"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, producerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, producerScope, producerGen)
	seedContainerImageIdentityAckScope(t, ctx, db, consumerScope)
	seedContainerImageIdentityAckGeneration(t, ctx, db, consumerScope, consumerGen)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes
SET active_generation_id = CASE scope_id
    WHEN $1 THEN $2
    WHEN $3 THEN $4
END
WHERE scope_id IN ($1, $3)
`, producerScope, producerGen, consumerScope, consumerGen); err != nil {
		t.Fatalf("activate completion scopes: %v", err)
	}
	seedContainerImageIdentityAckWorkItem(
		t, ctx, db, identityID, producerScope, producerGen,
		leaseOwner, now.Add(time.Minute), now,
	)
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, cicdID, consumerScope, consumerGen,
		reducer.DomainCICDRunCorrelation, now,
	)
	insertCrossScopeCompletionBaseConsumer(
		t, ctx, db, supplyID, consumerScope, consumerGen,
		reducer.DomainSupplyChainImpact, now,
	)

	queue := ReducerQueue{
		db:            SQLDB{DB: db},
		LeaseOwner:    leaseOwner,
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return now },
	}
	identity := reducer.Intent{
		IntentID:   identityID,
		Domain:     reducer.DomainContainerImageIdentity,
		ClaimEpoch: 1,
	}
	if err := queue.Ack(ctx, identity, reducer.Result{}); err != nil {
		t.Fatalf("ack identity producer: %v", err)
	}
	if err := queue.Ack(ctx, identity, reducer.Result{}); !errors.Is(err, ErrReducerClaimRejected) {
		t.Fatalf("duplicate identity ACK error = %v, want claim rejected", err)
	}
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainContainerImageIdentity, 1)

	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return time.Now().UTC().Add(3 * time.Second) }
	runner := reducer.CrossScopeCompletionRunner{
		Queue:      store,
		LeaseOwner: fanoutOwner,
		LeaseTTL:   time.Minute,
		BatchSize:  500,
		Now:        store.Now,
	}
	processed, first, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run identity completion = %+v processed=%t err=%v", first, processed, err)
	}
	if first.EventsProcessed != 1 || first.IntentsEnqueued != 2 {
		t.Fatalf("identity fanout = %+v, want events=1 intents=2", first)
	}
	assertCrossScopeConsumerState(t, ctx, db, cicdID, "pending", false)
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)

	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running', lease_owner = $2, claim_until = $3
WHERE work_item_id IN ($1, $4)
`, cicdID, leaseOwner, now.Add(time.Minute), supplyID); err != nil {
		t.Fatalf("claim synthetic downstream intents: %v", err)
	}
	if err := queue.Ack(ctx, reducer.Intent{
		IntentID: cicdID,
		Domain:   reducer.DomainCICDRunCorrelation,
	}, reducer.Result{}); err != nil {
		t.Fatalf("ack CI/CD producer: %v", err)
	}
	assertCrossScopeCompletionEventCount(t, ctx, db, reducer.DomainCICDRunCorrelation, 1)

	processed, second, err := runner.RunOnce(ctx)
	if err != nil || !processed {
		t.Fatalf("run CI/CD completion = %+v processed=%t err=%v", second, processed, err)
	}
	if second.EventsProcessed != 1 || second.IntentsEnqueued != 1 {
		t.Fatalf("CI/CD fanout = %+v, want events=1 intents=1", second)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "running", true)
	if err := queue.Ack(ctx, reducer.Intent{
		IntentID: supplyID,
		Domain:   reducer.DomainSupplyChainImpact,
	}, reducer.Result{}); err != nil {
		t.Fatalf("ack in-flight supply consumer: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "pending", false)
	if _, err := db.ExecContext(ctx, `
UPDATE fact_work_items
SET status = 'running', lease_owner = $2, claim_until = $3
WHERE work_item_id = $1
`, supplyID, leaseOwner, now.Add(time.Minute)); err != nil {
		t.Fatalf("claim required supply replay: %v", err)
	}
	if err := queue.Ack(ctx, reducer.Intent{
		IntentID: supplyID,
		Domain:   reducer.DomainSupplyChainImpact,
	}, reducer.Result{}); err != nil {
		t.Fatalf("ack required supply replay: %v", err)
	}
	assertCrossScopeConsumerState(t, ctx, db, supplyID, "succeeded", false)
	var workItems int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items`).Scan(&workItems); err != nil {
		t.Fatalf("count canonical completion work items: %v", err)
	}
	if workItems != 3 {
		t.Fatalf("completion work items = %d, want exactly 3 canonical rows", workItems)
	}
}

func TestCrossScopeCompletionFanoutSchedulesOnlyEligibleConsumerStatesLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	now := time.Now().UTC()
	const (
		scopeID    = "repository:5740-consumer-states"
		generation = "generation:5740-consumer-states"
		owner      = "consumer-state-owner"
	)
	seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
	seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generation)
	if _, err := db.ExecContext(ctx, `
UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1
`, scopeID, generation); err != nil {
		t.Fatalf("activate completion consumer-state scope: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, lease_owner, claim_until,
    payload, created_at, updated_at
)
SELECT 'reducer_5740_state_' || state.status,
       $1, $2, 'reducer', 'supply_chain_impact', state.status,
       'intent', 'reducer_5740_state_' || state.status,
       CASE WHEN state.status IN ('claimed', 'running') THEN $3::text ELSE NULL END,
       CASE WHEN state.status IN ('claimed', 'running') THEN $4::timestamptz ELSE NULL END,
       '{}'::jsonb, $5, $5
FROM unnest(ARRAY['succeeded','claimed','running','pending','retrying','dead_letter'])
     AS state(status)
`, scopeID, generation, owner, now.Add(time.Minute), now); err != nil {
		t.Fatalf("seed completion consumer states: %v", err)
	}
	insertCrossScopeCompletionEvent(
		t, ctx, db, reducer.DomainContainerImageIdentity,
		"pending", "", time.Time{}, 0, now.Add(-time.Second),
	)
	store := NewCrossScopeCompletionStore(SQLDB{DB: db})
	store.Now = func() time.Time { return now }
	lease, ok, err := store.Claim(ctx, owner, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim consumer-state event = %+v ok=%t err=%v", lease, ok, err)
	}
	result, err := store.Fanout(ctx, lease, 500)
	if err != nil {
		t.Fatalf("fanout consumer states: %v", err)
	}
	if result.IntentsEnqueued != 3 {
		t.Fatalf("consumer-state fanout = %+v, want three newly scheduled states", result)
	}
	for _, expected := range []struct {
		id     string
		status string
		replay bool
	}{
		{id: "reducer_5740_state_succeeded", status: "pending", replay: false},
		{id: "reducer_5740_state_claimed", status: "claimed", replay: true},
		{id: "reducer_5740_state_running", status: "running", replay: true},
		{id: "reducer_5740_state_pending", status: "pending", replay: false},
		{id: "reducer_5740_state_retrying", status: "retrying", replay: false},
		{id: "reducer_5740_state_dead_letter", status: "dead_letter", replay: false},
	} {
		assertCrossScopeConsumerState(t, ctx, db, expected.id, expected.status, expected.replay)
	}
}

func insertCrossScopeCompletionBaseConsumer(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	scopeID string,
	generationID string,
	domain reducer.Domain,
	now time.Time,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    conflict_domain, conflict_key, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'reducer', $4, 'succeeded',
          'intent', $1, '{}'::jsonb, $5, $5)
`, workItemID, scopeID, generationID, domain, now); err != nil {
		t.Fatalf("insert %s base consumer: %v", domain, err)
	}
}

func assertCrossScopeCompletionEventCount(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	domain reducer.Domain,
	want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM cross_scope_completion_events WHERE producer_domain = $1
`, domain).Scan(&got); err != nil {
		t.Fatalf("count %s completion events: %v", domain, err)
	}
	if got != want {
		t.Fatalf("%s completion events = %d, want %d", domain, got, want)
	}
}

func assertCrossScopeConsumerState(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	workItemID string,
	wantStatus string,
	wantReplay bool,
) {
	t.Helper()
	var status string
	var replay bool
	if err := db.QueryRowContext(ctx, `
SELECT status, cross_scope_replay_required
FROM fact_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&status, &replay); err != nil {
		t.Fatalf("read completion consumer %s: %v", workItemID, err)
	}
	if status != wantStatus || replay != wantReplay {
		t.Fatalf("completion consumer %s = status:%s replay:%t, want %s/%t", workItemID, status, replay, wantStatus, wantReplay)
	}
}
