// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/clock"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// Deterministic clock-seam proofs for issue #4121 (R-12 of epic #4102).
//
// The reducer queue decides lease expiry by comparing a row's claim_until
// against "now" inside the claim SQL (claim_until IS NULL OR claim_until <= $1).
// When "now" comes from an injected clock.Simulated, a test can advance time and
// trigger lease expiry deterministically, with no wall-clock sleeping — the
// property the Layer 3 replay tiers (#4122/#4123) build on.

// TestReducerQueueClaimAdvancingSimulatedClockMovesLeaseHorizon is a hermetic
// unit proof: advancing the injected Simulated clock moves the "now" the claim
// SQL evaluates and the claim_until horizon it writes, so a subsequent claim
// evaluates lease expiry against a time strictly past the prior lease deadline.
// It records the production claim SQL's bound arguments against a fake DB, so it
// runs in every suite without Postgres.
func TestReducerQueueClaimAdvancingSimulatedClockMovesLeaseHorizon(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, time.June, 28, 12, 0, 0, 0, time.UTC)
	sim := clock.NewSimulated(start)
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}, {}}}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-A",
		LeaseDuration: time.Minute,
		Now:           sim.Now,
	}

	if _, _, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("first Claim() error = %v, want nil", err)
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("recorded claim queries = %d, want %d", got, want)
	}
	firstNow, ok := db.queries[0].args[0].(time.Time)
	if !ok {
		t.Fatalf("claim arg[0] type = %T, want time.Time", db.queries[0].args[0])
	}
	firstClaimUntil, ok := db.queries[0].args[3].(time.Time)
	if !ok {
		t.Fatalf("claim arg[3] type = %T, want time.Time", db.queries[0].args[3])
	}
	if !firstNow.Equal(start) {
		t.Fatalf("first claim now = %s, want %s", firstNow, start)
	}
	if want := start.Add(time.Minute); !firstClaimUntil.Equal(want) {
		t.Fatalf("first claim_until = %s, want %s", firstClaimUntil, want)
	}

	// Advance the simulated clock past the lease just written.
	sim.Advance(90 * time.Second)

	if _, _, err := queue.Claim(context.Background()); err != nil {
		t.Fatalf("second Claim() error = %v, want nil", err)
	}
	secondNow, ok := db.queries[1].args[0].(time.Time)
	if !ok {
		t.Fatalf("second claim arg[0] type = %T, want time.Time", db.queries[1].args[0])
	}
	if want := start.Add(90 * time.Second); !secondNow.Equal(want) {
		t.Fatalf("second claim now = %s, want %s", secondNow, want)
	}
	// The advanced "now" the claim SQL evaluates is strictly past the prior
	// lease deadline, so the first lease is reclaimable: simulated time advance
	// triggers lease expiry in the production claim predicate.
	if !secondNow.After(firstClaimUntil) {
		t.Fatalf("advanced now %s is not past prior claim_until %s; lease would not expire", secondNow, firstClaimUntil)
	}
}

// TestReducerQueueSimulatedClockTriggersLeaseExpiryReclaim is the real-Postgres
// proof of the same property end to end: worker A claims a work item (lease
// held), worker B cannot claim it while the lease is live, then advancing the
// shared Simulated clock past the lease lets worker B reclaim the now-expired
// lease — all without sleeping on the wall clock. Skipped unless a DSN is set,
// matching the package's other concurrency proofs.
func TestReducerQueueSimulatedClockTriggersLeaseExpiryReclaim(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the clock-seam lease-expiry proof")
	}

	ctx := context.Background()
	db, _ := openReducerFairnessDBWithSchema(t, ctx, dsn)

	start := time.Date(2026, time.June, 28, 9, 0, 0, 0, time.UTC)
	sim := clock.NewSimulated(start)
	seedReducerFairnessScope(t, ctx, db, "scope-expiry", start)
	insertReducerFairnessWorkItem(t, ctx, db, reducerFairnessWorkItem{
		workItemID:     "expiry-item",
		scopeID:        "scope-expiry",
		generationID:   "gen-fair",
		domain:         string(conflictProofClaimDomain),
		conflictDomain: reducerConflictDomainCodeGraph,
		conflictKey:    "scope-expiry",
		updatedAt:      start,
	})

	newQueue := func(owner string) ReducerQueue {
		return ReducerQueue{
			db:            SQLDB{DB: db},
			LeaseOwner:    owner,
			LeaseDuration: time.Minute,
			Now:           sim.Now,
			ClaimDomains:  []reducer.Domain{conflictProofClaimDomain},
		}
	}
	queueA := newQueue("worker-A")
	queueB := newQueue("worker-B")

	intentA, claimedA, err := queueA.Claim(ctx)
	if err != nil {
		t.Fatalf("worker A Claim() error = %v", err)
	}
	if !claimedA {
		t.Fatal("worker A Claim() ok = false, want true (item is pending)")
	}
	if intentA.IntentID != "expiry-item" {
		t.Fatalf("worker A claimed %q, want %q", intentA.IntentID, "expiry-item")
	}

	// Lease is live (now == start, claim_until == start+1m): B must not claim it.
	if _, claimedB, err := queueB.Claim(ctx); err != nil {
		t.Fatalf("worker B early Claim() error = %v", err)
	} else if claimedB {
		t.Fatal("worker B claimed a work item while worker A's lease was still live")
	}

	// Advance the shared simulated clock past the lease; no wall-clock sleep.
	sim.Advance(2 * time.Minute)

	intentB, claimedB, err := queueB.Claim(ctx)
	if err != nil {
		t.Fatalf("worker B reclaim Claim() error = %v", err)
	}
	if !claimedB {
		t.Fatal("worker B Claim() ok = false after lease expiry, want true (lease reclaimable)")
	}
	if intentB.IntentID != "expiry-item" {
		t.Fatalf("worker B reclaimed %q, want %q", intentB.IntentID, "expiry-item")
	}
}
