// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wait_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// TestReadinessWaitStaleClearCannotTombstoneNewerWriteLive is review P3-b.
// Both writers read the same unsettled row at one epoch. The live worker sees
// a new missing set and rewrites the row at that epoch; the straggler saw the
// set empty and clears. The straggler's clear lands last and must not
// tombstone the live wait, or the live worker's next read restarts the bound.
// Both writes come from crossscope.DecideWait against the row read here.
func TestReadinessWaitStaleClearCannotTombstoneNewerWriteLive(t *testing.T) {
	store, _, ctx := readinessWaitLiveStore(t)
	scopeID := fmt.Sprintf("aws:readiness-wait-clear-cas-%d", time.Now().UnixNano())
	anchor := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	seed := liveWait(scopeID, anchor, "a")
	if err := store.UpsertReadinessWait(ctx, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	read, found, err := store.GetReadinessWait(ctx, scopeID, seed.Domain)
	if err != nil || !found {
		t.Fatalf("read: found %v err %v", found, err)
	}
	input := func(now time.Time, missing ...string) crossscope.WaitInput {
		return crossscope.WaitInput{
			Existing: read, Found: true, ScopeID: scopeID, Domain: seed.Domain,
			GenerationID: "gen-2", CycleStartedAt: anchor, Missing: missing, Now: now,
		}
	}
	live := crossscope.DecideWait(input(anchor.Add(time.Minute), "a", "b"))
	stale := crossscope.DecideWait(input(anchor.Add(2 * time.Minute)))
	if !live.Upsert || live.ResetAnchor || live.Row.AnchorEpoch != read.AnchorEpoch || !stale.Clear {
		t.Fatalf("decisions: live %+v stale %+v, want a same-epoch upsert and a clear", live, stale)
	}
	for _, write := range []crossscope.WaitDecision{live, stale} {
		if err := crossscope.ApplyWaitDecision(ctx, store, write, scopeID, seed.Domain); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	got, found, err := store.GetReadinessWait(ctx, scopeID, seed.Domain)
	if err != nil || !found {
		t.Fatalf("get: found %v err %v", found, err)
	}
	if got.Cleared() || got.MissingCount != 2 || !got.FirstDeferredAt.Equal(anchor) {
		t.Fatalf("straggler clear tombstoned the live wait: %+v, want {a b} anchored at %v", got, anchor)
	}

	// The live worker's own clear, from the row it just read, still applies.
	own := crossscope.DecideWait(crossscope.WaitInput{
		Existing: got, Found: true, ScopeID: scopeID, Domain: seed.Domain,
		GenerationID: "gen-2", CycleStartedAt: anchor, Now: anchor.Add(3 * time.Minute),
	})
	if err := crossscope.ApplyWaitDecision(ctx, store, own, scopeID, seed.Domain); err != nil {
		t.Fatalf("own clear: %v", err)
	}
	if got, _, err = store.GetReadinessWait(ctx, scopeID, seed.Domain); err != nil || !got.Cleared() {
		t.Fatalf("own clear: row %+v err %v, want a tombstone", got, err)
	}
}
