// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package wait_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
)

// TestReadinessWaitStaleWriterCannotUnsettleLive is review P3-a. Both writers
// read the same unsettled row. The live worker's evaluation crosses the bound
// and settles; the straggler's evaluation, from before the bound, defers. The
// straggler's write lands last and must not clear settled_at, or the next
// evaluation settles the same missing set again and counts abandoned twice.
// Both rows come from crossscope.DecideWait, so the test binds the shipped
// decision to the shipped SQL fence.
func TestReadinessWaitStaleWriterCannotUnsettleLive(t *testing.T) {
	store, _, ctx := readinessWaitLiveStore(t)
	scopeID := fmt.Sprintf("aws:readiness-wait-settle-fence-%d", time.Now().UnixNano())
	anchor := time.Date(2026, time.September, 19, 9, 0, 0, 0, time.UTC)
	seed := liveWait(scopeID, anchor, "a")
	if err := store.UpsertReadinessWait(ctx, seed); err != nil {
		t.Fatalf("seed: %v", err)
	}
	read, found, err := store.GetReadinessWait(ctx, scopeID, seed.Domain)
	if err != nil || !found {
		t.Fatalf("read: found %v err %v", found, err)
	}
	input := func(now time.Time) crossscope.WaitInput {
		return crossscope.WaitInput{
			Existing: read, Found: true, ScopeID: scopeID, Domain: seed.Domain,
			GenerationID: "gen-2", CycleStartedAt: anchor, Missing: []string{"a"}, Now: now,
		}
	}
	settle := crossscope.DecideWait(input(anchor.Add(crossscope.ProducerReadinessMaxWait)))
	stale := crossscope.DecideWait(input(anchor.Add(time.Minute)))
	if settle.Outcome != crossscope.ReadinessWaitAbandoned || !stale.Defer || !stale.Upsert {
		t.Fatalf("decisions: settle %+v stale %+v, want an abandoned settle and a deferring upsert", settle, stale)
	}
	for _, write := range []crossscope.WaitDecision{settle, stale} {
		if err := crossscope.ApplyWaitDecision(ctx, store, write, scopeID, seed.Domain); err != nil {
			t.Fatalf("apply: %v", err)
		}
	}
	got, found, err := store.GetReadinessWait(ctx, scopeID, seed.Domain)
	if err != nil || !found {
		t.Fatalf("get: found %v err %v", found, err)
	}
	if !got.Settled() {
		t.Fatalf("straggler un-settled the wait: %+v", got)
	}
	again := crossscope.DecideWait(crossscope.WaitInput{
		Existing: got, Found: true, ScopeID: scopeID, Domain: seed.Domain, GenerationID: "gen-2",
		CycleStartedAt: anchor, Missing: []string{"a"}, Now: anchor.Add(crossscope.ProducerReadinessMaxWait + time.Minute),
	})
	if again.Outcome != crossscope.ReadinessWaitSettledMissing {
		t.Fatalf("next evaluation outcome = %q, want %q (abandoned must count once)", again.Outcome, crossscope.ReadinessWaitSettledMissing)
	}
}
