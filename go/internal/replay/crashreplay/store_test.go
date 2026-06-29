// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crashreplay

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/clock"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/schedulereplay"
)

func twoItemStore(t *testing.T, clk clock.Clock, ttl time.Duration) *durableStore {
	t.Helper()
	store, err := newDurableStore([]schedulereplay.WorkItem{
		{IntentID: "a"},
		{IntentID: "b"},
	}, clk, ttl)
	if err != nil {
		t.Fatalf("newDurableStore: %v", err)
	}
	return store
}

func claimOne(t *testing.T, store *durableStore) (string, bool) {
	t.Helper()
	intent, ok, err := store.Claim(context.Background())
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	return intent.IntentID, ok
}

// TestDurableStoreNeverRehandsCompleted is the fencing guarantee: once every
// item is acked, Claim returns nothing. A completed item must never be claimable
// again — that is what makes a clean-boundary crash safe (recovery does not redo
// finished work).
func TestDurableStoreNeverRehandsCompleted(t *testing.T) {
	t.Parallel()
	store := twoItemStore(t, clock.NewSimulated(simStart), time.Minute)
	ctx := context.Background()

	for range 2 {
		intent, ok, err := store.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("Claim: ok=%v err=%v", ok, err)
		}
		if err := store.Ack(ctx, intent, reducer.Result{}); err != nil {
			t.Fatalf("Ack(%s): %v", intent.IntentID, err)
		}
	}
	if _, ok := claimOne(t, store); ok {
		t.Fatal("Claim returned an item after every item was completed; fencing is broken")
	}
	if store.ackedCount() != 2 {
		t.Fatalf("ackedCount = %d, want 2", store.ackedCount())
	}
	if store.doubleAckCount() != 0 {
		t.Fatalf("doubleAckCount = %d, want 0", store.doubleAckCount())
	}
}

// TestDurableStoreAckTwiceIsCounted gives the double-completion detector teeth:
// a second Ack of an already-completed intent (the shape a fencing regression
// would produce) MUST be counted in doubleAckCount, not silently absorbed. This
// is the unit-level guard behind every scenario's DoubleAcks == 0 assertion.
func TestDurableStoreAckTwiceIsCounted(t *testing.T) {
	t.Parallel()
	store := twoItemStore(t, clock.NewSimulated(simStart), time.Minute)
	ctx := context.Background()

	intent, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("Claim: ok=%v err=%v", ok, err)
	}
	if err := store.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("first Ack: %v", err)
	}
	// Re-ack the same intent, simulating a worker that completed work the fence
	// should have prevented it from re-claiming.
	if err := store.Ack(ctx, intent, reducer.Result{}); err != nil {
		t.Fatalf("second Ack: %v", err)
	}
	if store.doubleAckCount() != 1 {
		t.Fatalf("doubleAckCount = %d, want 1 (the detector must observe a re-completion)", store.doubleAckCount())
	}
	if store.ackedCount() != 1 {
		t.Fatalf("ackedCount = %d, want 1 (a re-ack must not inflate the distinct-completion count)", store.ackedCount())
	}
}

// TestDurableStoreReclaimsExpiredLeaseUnderHigherToken proves the lease + clock
// reclaim path: a claimed-but-unacked item is not reclaimable while its lease is
// live, and becomes reclaimable only after the lease lapses on the injected
// clock — handed back under a strictly higher fencing token (attempt count).
func TestDurableStoreReclaimsExpiredLeaseUnderHigherToken(t *testing.T) {
	t.Parallel()
	clk := clock.NewSimulated(simStart)
	store := twoItemStore(t, clk, time.Minute)
	ctx := context.Background()

	first, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("first Claim: ok=%v err=%v", ok, err)
	}
	if first.AttemptCount != 1 {
		t.Fatalf("first claim AttemptCount = %d, want 1", first.AttemptCount)
	}

	// A second claim while the first lease is live must hand out the OTHER item,
	// never the live-leased one.
	second, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("second Claim: ok=%v err=%v", ok, err)
	}
	if second.IntentID == first.IntentID {
		t.Fatalf("second Claim re-handed the live-leased item %q; lease not honored", first.IntentID)
	}

	// Now both leases are live; a third claim returns nothing.
	if _, ok := claimOne(t, store); ok {
		t.Fatal("Claim returned an item while both leases were live")
	}

	// Lapse the leases and reclaim: the first item comes back under attempt 2.
	clk.Advance(time.Minute + time.Second)
	reclaim, ok, err := store.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("reclaim Claim: ok=%v err=%v", ok, err)
	}
	if reclaim.AttemptCount < 2 {
		t.Fatalf("reclaim AttemptCount = %d, want >= 2 (fencing token must advance)", reclaim.AttemptCount)
	}
	if store.reclaimCount() < 1 {
		t.Fatalf("reclaimCount = %d, want >= 1", store.reclaimCount())
	}
	if store.maxAttemptSeen() < 2 {
		t.Fatalf("maxAttemptSeen = %d, want >= 2", store.maxAttemptSeen())
	}
}

// TestNewDurableStoreRejectsDuplicateIntent proves the store fails loudly on a
// duplicate IntentID rather than building an ambiguous schedule.
func TestNewDurableStoreRejectsDuplicateIntent(t *testing.T) {
	t.Parallel()
	_, err := newDurableStore([]schedulereplay.WorkItem{
		{IntentID: "dup"},
		{IntentID: "dup"},
	}, clock.NewSimulated(simStart), time.Minute)
	if err == nil {
		t.Fatal("newDurableStore accepted a duplicate intent id; want an error")
	}
}

// TestDrainFailsLoudlyOnUnrecoverableExecuteError proves the executor-error path
// surfaces the error immediately instead of hanging. The reducer loop turns an
// Execute error into a WorkSink.Fail + re-queue + continue, which against this
// store would spin forever; the fatal-sentinel panic must unwind that loop so
// drain returns the recorded error. The registry is deliberately empty so every
// claimed intent is "unknown" to the executor.
func TestDrainFailsLoudlyOnUnrecoverableExecuteError(t *testing.T) {
	t.Parallel()
	clk := clock.NewSimulated(simStart)
	items := []schedulereplay.WorkItem{{IntentID: "a"}}
	store, err := newDurableStore(items, clk, time.Minute)
	if err != nil {
		t.Fatalf("newDurableStore: %v", err)
	}
	h := &harness{
		store:    store,
		graph:    schedulereplay.NewGraph(),
		clk:      clk,
		apply:    schedulereplay.ApplyCanonical,
		registry: map[string]schedulereplay.WorkItem{}, // empty: every intent is unknown
		leaseTTL: time.Minute,
	}

	// A short deadline: if the fatal path regressed into a spin/hang, this fails
	// via the deadline rather than wedging the whole test binary.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := h.drain(ctx, nil); err == nil {
		t.Fatal("drain did not fail on an unknown intent; the fatal path must surface an error, not hang or pass")
	}
}
