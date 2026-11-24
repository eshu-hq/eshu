// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The #10972 D4b proofs: the source pass reads the in-flight claims once when
// its evidence load ends (set A) and again at fan-out (set B), and flags only
// the claims in both. Every holder below is claimed through the real
// ReducerQueue.Claim and every read goes through the real fan-out store.

// loadEndHookFanOut wraps the real fan-out port. It records the first load-end
// read (set A) of each asking scope, runs afterLoadEnd once right after the
// first read returns, and fails the read with failLoadEnd while it is set.
type loadEndHookFanOut struct {
	supplychaincore.SupplyChainImpactFanOut
	mu           sync.Mutex
	afterLoadEnd func()
	failLoadEnd  error
	loadEnd      map[string][]supplychaincore.InFlightClaim
}

func (f *loadEndHookFanOut) SnapshotInFlightNonMarkerClaims(ctx context.Context, own string) ([]supplychaincore.InFlightClaim, error) {
	claims, err := f.SupplyChainImpactFanOut.SnapshotInFlightNonMarkerClaims(ctx, own)
	f.mu.Lock()
	if _, seen := f.loadEnd[own]; !seen {
		if f.loadEnd == nil {
			f.loadEnd = map[string][]supplychaincore.InFlightClaim{}
		}
		f.loadEnd[own] = claims
	}
	hook, fail := f.afterLoadEnd, f.failLoadEnd
	f.afterLoadEnd = nil // the hook's own passes read through this wrapper too
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	if fail != nil {
		return nil, fail
	}
	return claims, err
}

func (f *loadEndHookFanOut) stopFailing() {
	f.mu.Lock()
	f.failLoadEnd = nil
	f.mu.Unlock()
}

// sourceLoadEndHolds reports whether the source pass's first set A names
// scope.
func (f *loadEndHookFanOut) sourceLoadEndHolds(scope string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, claim := range f.loadEnd[convergenceSource] {
		if claim.ScopeID == scope {
			return true
		}
	}
	return false
}

// hookFanOut installs a load-end wrapper over the harness's real port.
func (c *convergence) hookFanOut() *loadEndHookFanOut {
	hook := &loadEndHookFanOut{SupplyChainImpactFanOut: c.fanOut}
	c.handler.FanOut = hook
	return hook
}

// seedNoRowHolder creates a scope that holds no finding row and one EPSS fact
// for the CVE, with no work item: the row read cannot see it, so only the
// in-flight arm can flag it.
func (c *convergence) seedNoRowHolder(scope string) {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, scope, "g1")
	c.upsert(c.envelope(scope, fanOutGen(scope, "g1"), "fact:10972:epss:"+scope, "vulnerability.epss_score", map[string]any{
		"cve_id": convergenceCVE, "probability": "0.5", "percentile": "0.9", "score_date": "2026-10-31",
	}))
}

// finishClaimed runs a claimed pass to its acknowledgement.
func (c *convergence) finishClaimed(intent reducercontract.Intent) {
	c.t.Helper()
	result, err := c.handler.Handle(c.ctx, intent)
	if err != nil {
		c.t.Fatalf("pass %s: %v", intent.ScopeID, err)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		c.t.Fatalf("ack %s: %v", intent.ScopeID, err)
	}
}

// claimTarget claims until scope's item is claimed. Any other item claimed
// first is a pass this test's own passes reopened (a holder's fan-out reopens
// the scopes holding its CVE, scope:9695:epss among them); it runs to its
// acknowledgement so it is not left in flight.
func (c *convergence) claimTarget(scope string) reducercontract.Intent {
	c.t.Helper()
	for i := 0; i < 10; i++ {
		intent := c.raceClaim("")
		if intent.ScopeID == scope {
			return intent
		}
		c.finishClaimed(intent)
	}
	c.t.Fatalf("scope %s was not claimable within 10 claims", scope)
	return reducercontract.Intent{}
}

// drainedRuns drains the queue and returns how many times scope ran in it.
func (c *convergence) drainedRuns(scope string) int {
	c.t.Helper()
	c.handled = nil
	c.drain()
	runs := 0
	for _, entry := range c.handled {
		if strings.HasPrefix(entry, scope+"#") {
			runs++
		}
	}
	return runs
}

func workItemXmin(t *testing.T, ctx context.Context, db *sql.DB, id string) string {
	t.Helper()
	var xmin string
	if err := db.QueryRowContext(ctx, `SELECT xmin::text FROM fact_work_items WHERE work_item_id = $1`, id).Scan(&xmin); err != nil {
		t.Fatalf("read xmin of %s: %v", id, err)
	}
	return xmin
}

// TestFanOutRaceHolderClaimedAfterTheSourceLoadIsNotFlaggedLive is D4b test 1:
// the holder is claimed after the source's evidence load ended and before its
// fan-out. It loads later than the source did, so the source's fan-out leaves
// its item untouched (same row version, no replay bit) and it runs once.
func TestFanOutRaceHolderClaimedAfterTheSourceLoadIsNotFlaggedLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	const late = "scope:10972:after-load"
	c.seedNoRowHolder(late)
	hook := c.hookFanOut()
	var (
		holder reducercontract.Intent
		xmin   string
	)
	hook.afterLoadEnd = func() {
		fanOutSeedItem(t, c.ctx, c.db, late, "g1")
		holder = c.claimTarget(late)
		xmin = workItemXmin(t, c.ctx, c.db, holder.IntentID)
	}
	c.rejectSource()
	if holder.IntentID == "" {
		t.Fatal("the source pass took no load-end read")
	}
	if got := workItemXmin(t, c.ctx, c.db, holder.IntentID); got != xmin {
		t.Fatalf("holder item xmin = %s, want %s unchanged: the source's fan-out touched a pass claimed after its load", got, xmin)
	}
	if st := fanOutItem(t, c.ctx, c.db, holder.IntentID); st.status != "claimed" || st.flag {
		t.Fatalf("holder item = %+v, want claimed without the replay bit", st)
	}
	c.finishClaimed(holder)
	if runs := c.drainedRuns(late); runs != 0 {
		t.Fatalf("holder ran %d more times after its first pass (handled %v), want 0", runs, c.handled)
	}
	if got := c.copies()[late]; got != "advisory_retracted" {
		t.Fatalf("holder copy = %q, want advisory_retracted from its single pass (copies %v)", got, c.copies())
	}
}

// TestFanOutRaceHolderClaimedBeforeTheSourceLoadEndedIsFlaggedLive is D4b test
// 2: the holder was claimed before the source's load ended and is still
// claimed at the fan-out, so the same claim is in both sets and it is flagged.
func TestFanOutRaceHolderClaimedBeforeTheSourceLoadEndedIsFlaggedLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	const early = "scope:10972:before-load"
	c.seedNoRowHolder(early)
	fanOutSeedItem(t, c.ctx, c.db, early, "g1")
	holder := c.claimTarget(early)
	hook := c.hookFanOut()
	c.rejectSource()
	if !hook.sourceLoadEndHolds(early) {
		t.Fatalf("the source's set A = %v, want it to name the claimed holder", hook.loadEnd[convergenceSource])
	}
	if st := fanOutItem(t, c.ctx, c.db, holder.IntentID); st.status != "claimed" || !st.flag {
		t.Fatalf("holder item = %+v, want claimed with the replay bit", st)
	}
	c.finishClaimed(holder)
	if runs := c.drainedRuns(early); runs != 1 {
		t.Fatalf("flagged holder ran %d times after its first pass (handled %v), want 1 replay", runs, c.handled)
	}
	if got := c.copies()[early]; got != "advisory_retracted" {
		t.Fatalf("holder copy = %q, want advisory_retracted (copies %v)", got, c.copies())
	}
}

// TestFanOutRaceHolderReclaimedAfterTheSourceLoadIsNotFlaggedLive is D4b test
// 3: the holder is in set A, acknowledges, and is claimed again before set B.
// The new claim has a newer last_attempt_at, so the old claim does not match
// it and the new claim, which loads after the source's load, is not flagged.
// The first claim is acknowledged without a pass, so the holder still has no
// finding row and only the in-flight arm could flag it: a first pass would
// write the CVE's row, and the unchanged row read would then reopen (and flag)
// the second claim whatever the two sets say.
func TestFanOutRaceHolderReclaimedAfterTheSourceLoadIsNotFlaggedLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	const reclaimed = "scope:10972:reclaimed"
	c.seedNoRowHolder(reclaimed)
	fanOutSeedItem(t, c.ctx, c.db, reclaimed, "g1")
	first := c.claimTarget(reclaimed)
	hook := c.hookFanOut()
	var (
		second reducercontract.Intent
		xmin   string
	)
	hook.afterLoadEnd = func() {
		if err := c.queue.Ack(c.ctx, first, reducercontract.Result{
			IntentID: first.IntentID,
			Domain:   reducercontract.DomainSupplyChainImpact,
			Status:   reducercontract.ResultStatusSucceeded,
		}); err != nil {
			t.Errorf("ack first claim: %v", err)
		}
		c.rerun(reclaimed)
		second = c.claimTarget(reclaimed)
		xmin = workItemXmin(t, c.ctx, c.db, second.IntentID)
	}
	c.rejectSource()
	if !hook.sourceLoadEndHolds(reclaimed) {
		t.Fatalf("the source's set A = %v, want it to name the first claim", hook.loadEnd[convergenceSource])
	}
	if second.ClaimedAt == nil || first.ClaimedAt == nil || !second.ClaimedAt.After(*first.ClaimedAt) {
		t.Fatalf("claim identities first=%v second=%v, want the second claim strictly newer", first.ClaimedAt, second.ClaimedAt)
	}
	if got := workItemXmin(t, c.ctx, c.db, second.IntentID); got != xmin {
		t.Fatalf("holder item xmin = %s, want %s unchanged: the old claim matched the new one", got, xmin)
	}
	if st := fanOutItem(t, c.ctx, c.db, second.IntentID); st.status != "claimed" || st.flag {
		t.Fatalf("re-claimed holder item = %+v, want claimed without the replay bit", st)
	}
	c.finishClaimed(second)
	if runs := c.drainedRuns(reclaimed); runs != 0 {
		t.Fatalf("holder ran %d more times (handled %v), want 0", runs, c.handled)
	}
	if got := c.copies()[reclaimed]; got != "advisory_retracted" {
		t.Fatalf("holder copy = %q, want advisory_retracted (copies %v)", got, c.copies())
	}
}

// TestFanOutRaceLoadEndReadFailureFlagsEveryInFlightPassLive is D4b test 4:
// the load-end read fails (a timeout here), so no claim is proven new. A holder
// claimed after the load is flagged like every other in-flight pass, and the
// source pass still succeeds (rejectSource fails the test on a Handle error).
func TestFanOutRaceLoadEndReadFailureFlagsEveryInFlightPassLive(t *testing.T) {
	for name, failure := range map[string]error{
		"timeout": supplychaincore.ErrFanOutReadTimeout,
		"error":   errors.New("injected load-end read failure"),
	} {
		t.Run(name, func(t *testing.T) {
			c := newConvergence(t, false)
			c.drain()
			const late = "scope:10972:early-read-failed"
			c.seedNoRowHolder(late)
			hook := c.hookFanOut()
			hook.failLoadEnd = failure
			var holder reducercontract.Intent
			hook.afterLoadEnd = func() {
				fanOutSeedItem(t, c.ctx, c.db, late, "g1")
				holder = c.claimTarget(late)
			}
			c.rejectSource()
			hook.stopFailing()
			if holder.IntentID == "" {
				t.Fatal("the source pass took no load-end read")
			}
			if st := fanOutItem(t, c.ctx, c.db, holder.IntentID); st.status != "claimed" || !st.flag {
				t.Fatalf("holder item = %+v, want claimed with the replay bit after a failed load-end read", st)
			}
			c.finishClaimed(holder)
			if runs := c.drainedRuns(late); runs != 1 {
				t.Fatalf("holder ran %d times after its first pass (handled %v), want 1 replay", runs, c.handled)
			}
			if got := c.copies()[late]; got != "advisory_retracted" {
				t.Fatalf("holder copy = %q, want advisory_retracted (copies %v)", got, c.copies())
			}
		})
	}
}

// TestFanOutRaceNoRowHolderInSetAIsFlaggedAndConvergesLive is D4b test 5: the
// holder has no finding row, its pass loaded before the source's change and is
// still running, and set A names it. It is flagged and converges, the case
// TestFanOutRaceInFlightHolderThatNeverHeldTheCVELive also pins.
func TestFanOutRaceNoRowHolderInSetAIsFlaggedAndConvergesLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	const late = "scope:10972:no-rows"
	c.seedNoRowHolder(late)
	fanOutSeedItem(t, c.ctx, c.db, late, "g1")
	hook := c.hookFanOut()
	pass := c.startGatedPass(c.claimTarget(late))
	c.rejectSource()
	if !hook.sourceLoadEndHolds(late) {
		t.Fatalf("the source's set A = %v, want it to name the running holder", hook.loadEnd[convergenceSource])
	}
	if st := fanOutItem(t, c.ctx, c.db, pass.intent.IntentID); !st.flag {
		t.Fatalf("holder item = %+v, want the replay bit while in flight", st)
	}
	pass.finish()
	c.drain()
	if got := c.copies()[late]; got != "advisory_retracted" {
		t.Fatalf("holder copy = %q, want advisory_retracted after its rerun (copies %v)", got, c.copies())
	}
}
