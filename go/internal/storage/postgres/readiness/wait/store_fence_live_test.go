// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wait_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestReadinessWaitStaleWriterCannotUndoResetLive is review P3-1: after a
// settled row restarts its bound for a new missing set, a lease-expired
// straggler that read the row before the reset must not restore the older
// anchor. Each round races the reset against the straggler; the final anchor
// must be the reset anchor whichever commits last.
func TestReadinessWaitStaleWriterCannotUndoResetLive(t *testing.T) {
	store, _, ctx := readinessWaitLiveStore(t)
	scopeID := fmt.Sprintf("aws:readiness-wait-fence-%d", time.Now().UnixNano())
	early := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	resetAt := early.Add(time.Hour)

	for round := 0; round < 20; round++ {
		roundScope := fmt.Sprintf("%s-%d", scopeID, round)
		settled := liveWait(roundScope, early, "a")
		settled.SettledAt = early.Add(30 * time.Minute)
		if err := store.UpsertReadinessWait(ctx, settled); err != nil {
			t.Fatalf("round %d seed: %v", round, err)
		}
		// Both writers read the settled row at epoch 0. The live worker saw a
		// new missing set and resets; the straggler still carries the old
		// unsettled view and the old anchor.
		reset := liveWait(roundScope, resetAt, "a", "c")
		reset.AnchorEpoch = settled.AnchorEpoch + 1
		stale := liveWait(roundScope, early, "a")

		var wg sync.WaitGroup
		errs := make(chan error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs <- store.UpsertReadinessWait(ctx, reset) }()
		go func() {
			defer wg.Done()
			if round%2 == 0 {
				// Force the bad order on half the rounds: straggler last.
				time.Sleep(20 * time.Millisecond)
			}
			errs <- store.UpsertReadinessWait(ctx, stale)
		}()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d upsert: %v", round, err)
			}
		}
		got, found, err := store.GetReadinessWait(ctx, roundScope, reducercontract.DomainWorkloadCloudRelationshipMaterialization)
		if err != nil || !found {
			t.Fatalf("round %d get: found %v err %v", round, found, err)
		}
		if !got.FirstDeferredAt.Equal(resetAt) || got.MissingCount != 2 || got.Settled() {
			t.Fatalf("round %d row = anchor %v count %d settled %v, want the reset anchor %v over {a c}",
				round, got.FirstDeferredAt, got.MissingCount, got.Settled(), resetAt)
		}
	}
}

// TestReadinessWaitStaleWriterCannotResurrectClearedWaitLive: a straggler
// that read a wait before it was cleared must not re-insert its stale anchor.
func TestReadinessWaitStaleWriterCannotResurrectClearedWaitLive(t *testing.T) {
	store, _, ctx := readinessWaitLiveStore(t)
	scopeID := fmt.Sprintf("aws:readiness-wait-clear-fence-%d", time.Now().UnixNano())
	early := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	row := liveWait(scopeID, early, "a")
	if err := store.UpsertReadinessWait(ctx, row); err != nil {
		t.Fatalf("seed: %v", err)
	}
	clear := row
	clear.ClearedAt = early.Add(time.Minute)
	if err := store.ClearReadinessWait(ctx, clear); err != nil {
		t.Fatalf("clear: %v", err)
	}
	// The straggler read the row at epoch 0, before the clear.
	if err := store.UpsertReadinessWait(ctx, liveWait(scopeID, early, "a")); err != nil {
		t.Fatalf("stale upsert: %v", err)
	}
	got, found, err := store.GetReadinessWait(ctx, scopeID, row.Domain)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if found && !got.Cleared() {
		t.Fatalf("stale writer resurrected the cleared wait: %+v", got)
	}
}
