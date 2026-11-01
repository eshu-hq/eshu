// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// gatedPass runs a claimed holder pass in the background and parks it after its
// evidence load, before its writer commits: the window the in-flight read exists
// for. release lets it finish and acknowledges it.
type gatedPass struct {
	c      *convergence
	intent reducercontract.Intent
	gate   *raceGateWriter
	done   chan error
}

func (c *convergence) startGatedPass(intent reducercontract.Intent) *gatedPass {
	c.t.Helper()
	gate := &raceGateWriter{inner: c.handler.Writer, loaded: make(chan struct{}), release: make(chan struct{})}
	handler := c.handler
	handler.Writer = gate
	pass := &gatedPass{c: c, intent: intent, gate: gate, done: make(chan error, 1)}
	go func() {
		result, err := handler.Handle(c.ctx, intent)
		if err != nil {
			pass.done <- err
			return
		}
		pass.done <- c.queue.Ack(c.ctx, intent, result)
	}()
	<-gate.loaded
	return pass
}

func (p *gatedPass) finish() {
	p.c.t.Helper()
	close(p.gate.release)
	if err := <-p.done; err != nil {
		p.c.t.Fatalf("gated pass: %v", err)
	}
}

// rejectSource commits the rejection through the real source pass, whose
// fan-out runs while a gated holder pass is in flight, and returns the result.
func (c *convergence) rejectSource() reducercontract.Result {
	c.t.Helper()
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	src := c.raceClaim(convergenceSource)
	result, err := c.handler.Handle(c.ctx, src)
	if err != nil {
		c.t.Fatalf("source handle: %v", err)
	}
	if err := c.queue.Ack(c.ctx, src, result); err != nil {
		c.t.Fatal(err)
	}
	return result
}

// TestFindInFlightScopesLive pins the selection: a claimed or running item at the
// scope's ACTIVE generation, never the asking scope, never a pending, retrying or
// succeeded item, an item at an older generation, or another domain.
func TestFindInFlightScopesLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	for _, scope := range []string{"own", "claimed", "running", "pending", "succeeded", "old-generation", "other-domain"} {
		fanOutSeedScope(t, ctx, db, scope, "g2", "g1")
		fanOutSeedItem(t, ctx, db, scope, "g2")
	}
	fanOutSeedItem(t, ctx, db, "old-generation", "g1")
	for scope, status := range map[string]string{"own": "claimed", "claimed": "claimed", "running": "running", "succeeded": "succeeded"} {
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status = $2, lease_owner = CASE WHEN $2 IN ('claimed','running') THEN 'w' END,
claim_until = CASE WHEN $2 IN ('claimed','running') THEN now() + interval '1 hour' END, last_attempt_at = now()
WHERE scope_id = $1 AND generation_id = $3`, scope, status, fanOutGen(scope, "g2")); err != nil {
			t.Fatal(err)
		}
	}
	// An older-generation item that is running does not count: its pass writes a
	// superseded generation.
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='running', lease_owner='w', claim_until=now()+interval '1 hour', last_attempt_at=now()
WHERE scope_id='old-generation' AND generation_id = $1`, fanOutGen("old-generation", "g1")); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET domain='workload_materialization', status='claimed', lease_owner='w', claim_until=now()+interval '1 hour', last_attempt_at=now()
WHERE scope_id='other-domain'`); err != nil {
		t.Fatal(err)
	}
	got, err := fanOutLiveStore(db).FindInFlightScopes(ctx, "own")
	if err != nil {
		t.Fatalf("FindInFlightScopes() error = %v", err)
	}
	if want := []string{"claimed", "running"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("in-flight = %v, want %v", got, want)
	}
}

// TestFindInFlightScopesTimesOutUnderALockedTableLive: the read has the same
// bounded timeout and typed error as the holder read.
func TestFindInFlightScopesTimesOutUnderALockedTableLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	if _, err := tx.ExecContext(ctx, `LOCK TABLE fact_work_items IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	store := fanOutLiveStore(db)
	store.ReadTimeout = 150_000_000
	if _, err := store.FindInFlightScopes(ctx, "own"); !errors.Is(err, supplychaincore.ErrFanOutReadTimeout) {
		t.Fatalf("error = %v, want ErrFanOutReadTimeout", err)
	}
}

// TestFanOutRaceInFlightHolderThatNeverHeldTheCVELive: the holder has no finding
// row anywhere, its first pass claimed and parked after its evidence load. The
// row read cannot see it; the in-flight read flags it and it reruns.
func TestFanOutRaceInFlightHolderThatNeverHeldTheCVELive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	const late = "scope:9695:late"
	fanOutSeedScope(t, c.ctx, c.db, late, "g1")
	c.upsert(c.envelope(late, fanOutGen(late, "g1"), "fact:9695:epss:late", "vulnerability.epss_score", map[string]any{
		"cve_id": convergenceCVE, "probability": "0.5", "percentile": "0.9", "score_date": "2026-10-31",
	}))
	fanOutSeedItem(c.t, c.ctx, c.db, late, "g1")
	pass := c.startGatedPass(c.raceClaim(late))
	result := c.rejectSource()
	if got := result.SubSignals["fanout_holders"]; got < 1 {
		t.Fatalf("source fanout_holders = %v, want the in-flight scope counted", got)
	}
	if st := fanOutItem(t, c.ctx, c.db, pass.intent.IntentID); !st.flag {
		t.Fatalf("late holder item = %+v, want the replay bit set while in flight", st)
	}
	pass.finish()
	c.drain()
	if got := c.copies()[late]; got != "advisory_retracted" {
		t.Fatalf("late holder copy = %q, want advisory_retracted after its rerun (copies %v)", got, c.copies())
	}
}

// TestFanOutRaceRunningHolderWithRowsOnlyInAnOlderGenerationLive is the reviewer's
// race with the item in the 'running' state (a heartbeat moved it from claimed).
func TestFanOutRaceRunningHolderWithRowsOnlyInAnOlderGenerationLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.raceRescan(convergenceHolder, "g1", "g2")
	intent := c.raceClaim(convergenceHolder)
	if _, err := c.db.ExecContext(c.ctx, `UPDATE fact_work_items SET status = 'running' WHERE work_item_id = $1`, intent.IntentID); err != nil {
		t.Fatal(err)
	}
	pass := c.startGatedPass(intent)
	c.rejectSource()
	if st := fanOutItem(t, c.ctx, c.db, intent.IntentID); st.status != "running" || !st.flag {
		t.Fatalf("holder item = %+v, want running with the replay bit", st)
	}
	pass.finish()
	c.drain()
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// racingFanOut finishes a chosen holder's pass between the holder read and the
// reopen, the "succeeded between read and reopen" interleaving.
type racingFanOut struct {
	supplychaincore.SupplyChainImpactFanOut
	between func()
}

func (r *racingFanOut) FindInFlightScopes(ctx context.Context, own string) ([]string, error) {
	scopes, err := r.SupplyChainImpactFanOut.FindInFlightScopes(ctx, own)
	if err == nil && r.between != nil {
		hook := r.between
		r.between = nil // the hook's own pass reads through this wrapper too
		hook()
	}
	return scopes, err
}

// TestFanOutRaceHolderSucceedsBetweenTheReadAndTheReopenLive: the holder is in
// flight when read and acknowledged before the reopen statement runs. The CASE runs
// on the locked row, so a succeeded item is reopened, not lost.
func TestFanOutRaceHolderSucceedsBetweenTheReadAndTheReopenLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.handled = nil
	c.setSourceStatus("Rejected")
	c.rerun(convergenceHolder)
	holder := c.raceClaim(convergenceHolder)
	c.rerun(convergenceSource)
	c.handler.FanOut = &racingFanOut{SupplyChainImpactFanOut: c.fanOut, between: func() {
		result, err := c.handler.Handle(c.ctx, holder)
		if err != nil {
			t.Errorf("holder pass: %v", err)
			return
		}
		if err := c.queue.Ack(c.ctx, holder, result); err != nil {
			t.Errorf("holder ack: %v", err)
		}
	}}
	c.drain()
	holderReruns := 0
	for _, entry := range c.handled {
		if strings.HasPrefix(entry, convergenceHolder+"#") {
			holderReruns++
		}
	}
	if holderReruns != 1 {
		t.Fatalf("handled = %v, want the holder to rerun once: the reopen must see the succeeded item", c.handled)
	}
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestFanOutRaceInFlightItemAtAnOlderGenerationIsHarmlessLive: the holder's old
// generation item is still running while its new generation is active and its
// item pending. The in-flight read ignores the old item; the new generation's
// pending pass loads current evidence.
func TestFanOutRaceInFlightItemAtAnOlderGenerationIsHarmlessLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.rerun(convergenceHolder)
	old := c.raceClaim(convergenceHolder) // the old generation's item, in flight
	c.raceRescan(convergenceHolder, "g1", "g2")
	result := c.rejectSource()
	if got := result.SubSignals["fanout_holders"]; got != 0 {
		t.Fatalf("source fanout_holders = %v, want 0: the old-generation item is not a holder", got)
	}
	if st := fanOutItem(t, c.ctx, c.db, old.IntentID); st.status != "claimed" || st.flag || st.originSrc != "" {
		t.Fatalf("old-generation item = %+v, want untouched", st)
	}
	oldResult, err := c.handler.Handle(c.ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.queue.Ack(c.ctx, old, oldResult); err != nil {
		t.Fatal(err)
	}
	c.drain() // the new generation's pending pass loads current evidence
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestFanOutRaceTwoSourcesFlagTheSameInFlightHolderOnceLive: a second source's
// fan-out finds the holder already flagged (pending_noop), and the holder reruns
// exactly once, as a stamped one-hop attempt that fans out nothing.
func TestFanOutRaceTwoSourcesFlagTheSameInFlightHolderOnceLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.rerun(convergenceHolder)
	holder := c.raceClaim(convergenceHolder)
	first := supplychaincore.FanOutOrigin{SourceScopeID: "scope:source-1", SourceGenerationID: "g", SourceIntentID: "i1"}
	second := supplychaincore.FanOutOrigin{SourceScopeID: "scope:source-2", SourceGenerationID: "g", SourceIntentID: "i2"}
	if out, err := c.fanOut.ReopenHolder(c.ctx, convergenceHolder, first); err != nil || out != supplychaincore.FanOutReplayFlagged {
		t.Fatalf("first source = %q %v, want replay_flagged", out, err)
	}
	if out, err := c.fanOut.ReopenHolder(c.ctx, convergenceHolder, second); err != nil || out != supplychaincore.FanOutPendingNoop {
		t.Fatalf("second source = %q %v, want pending_noop", out, err)
	}
	result, err := c.handler.Handle(c.ctx, holder)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.queue.Ack(c.ctx, holder, result); err != nil {
		t.Fatal(err)
	}
	if st := fanOutItem(t, c.ctx, c.db, holder.IntentID); st.status != "pending" || st.originSrc != "scope:source-1" {
		t.Fatalf("holder after its ack = %+v, want pending with the first source's stamp", st)
	}
	rerun := c.raceClaim(convergenceHolder)
	if rerun.AttemptCount != 1 || fanOutOwedPayload(rerun) {
		t.Fatalf("rerun attempt=%d payload=%v, want attempt 1 and no owed marker", rerun.AttemptCount, rerun.Payload)
	}
	got, err := c.handler.Handle(c.ctx, rerun)
	if err != nil {
		t.Fatal(err)
	}
	if v := got.SubSignals["fanout_holders"]; v != 0 {
		t.Fatalf("flagged rerun fanout_holders = %v, want 0: it is one hop", v)
	}
	if err := c.queue.Ack(c.ctx, rerun, got); err != nil {
		t.Fatal(err)
	}
	if more := c.raceTryClaim(); more {
		t.Fatal("the holder had a third claim: two flags must produce one rerun")
	}
}

func fanOutOwedPayload(intent reducercontract.Intent) bool {
	owed, _ := intent.Payload[supplychaincore.PayloadKeyFanOutOwed].(bool)
	return owed
}

func (c *convergence) raceTryClaim() bool {
	c.t.Helper()
	intent, ok, err := c.queue.Claim(c.ctx)
	if err != nil {
		c.t.Fatal(err)
	}
	if ok {
		c.t.Logf("unexpected claim: %s attempt %d", intent.ScopeID, intent.AttemptCount)
	}
	return ok
}

// afterFirstReadFanOut runs a hook once, right after the first fan-out read of
// either kind returns and before the second one starts, so a test can finish a
// holder's pass in the gap between the two reads whichever order the handler uses.
type afterFirstReadFanOut struct {
	supplychaincore.SupplyChainImpactFanOut
	hook func()
	once sync.Once
}

func (f *afterFirstReadFanOut) FindHolderScopes(ctx context.Context, own string, cves []string) ([]string, error) {
	scopes, err := f.SupplyChainImpactFanOut.FindHolderScopes(ctx, own, cves)
	f.once.Do(f.hook)
	return scopes, err
}

func (f *afterFirstReadFanOut) FindInFlightScopes(ctx context.Context, own string) ([]string, error) {
	scopes, err := f.SupplyChainImpactFanOut.FindInFlightScopes(ctx, own)
	f.once.Do(f.hook)
	return scopes, err
}

// TestFanOutRaceHolderCommitsAndAcksBetweenTheTwoReadsLive is the ordering proof.
// The holder's first pass of a new generation loaded evidence before the source
// committed, so it has no rows. It commits and acknowledges in the gap between the
// source's two reads. Read the rows first and neither read sees it (no rows at the
// first read, already succeeded at the second); read the in-flight set first and
// the holder is still claimed at that read, so it is flagged, and its committed
// rows are visible to the row read that follows.
func TestFanOutRaceHolderCommitsAndAcksBetweenTheTwoReadsLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.handled = nil
	c.raceRescan(convergenceHolder, "g1", "g2")
	pass := c.startGatedPass(c.raceClaim(convergenceHolder))
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	source := c.handler
	source.FanOut = &afterFirstReadFanOut{SupplyChainImpactFanOut: c.fanOut, hook: func() {
		pass.finish() // commits its rows and acknowledges, between the two reads
	}}
	src := c.raceClaim(convergenceSource)
	result, err := source.Handle(c.ctx, src)
	if err != nil {
		t.Fatalf("source handle: %v", err)
	}
	if err := c.queue.Ack(c.ctx, src, result); err != nil {
		t.Fatal(err)
	}
	if got := result.SubSignals["fanout_holders"]; got < 1 {
		t.Fatalf("source fanout_holders = %v (holder item %+v), want the holder reached whichever read ran first", got, fanOutItem(t, c.ctx, c.db, pass.intent.IntentID))
	}
	c.drain()
	c.wantCopies(map[string]string{convergenceSource: "advisory_retracted", convergenceHolder: "advisory_retracted"})
}

// TestFanOutRaceAFlaggedRerunIsItselfFlaggableLive pins the mechanism behind the
// in-flight cost. A pass flagged while it runs reruns once after it finishes; that
// rerun is in flight too, so a changed pass that commits during it flags it again
// and it reruns again. Each rerun is a one-hop pass that fans out nothing, so the
// chain is driven only by arrivals and ends the moment they stop.
func TestFanOutRaceAFlaggedRerunIsItselfFlaggableLive(t *testing.T) {
	c := newConvergence(t, false)
	c.drain()
	c.handled = nil
	c.rerun(convergenceHolder)
	runs := 0
	for arrival := 1; arrival <= 3; arrival++ {
		intent := c.raceClaim(convergenceHolder)
		runs++
		origin := supplychaincore.FanOutOrigin{SourceScopeID: "scope:source", SourceGenerationID: "g", SourceIntentID: "i"}
		if out, err := c.fanOut.ReopenHolder(c.ctx, convergenceHolder, origin); err != nil || out != supplychaincore.FanOutReplayFlagged {
			t.Fatalf("arrival %d = %q %v, want replay_flagged on the pass in flight", arrival, out, err)
		}
		if out, err := c.fanOut.ReopenHolder(c.ctx, convergenceHolder, origin); err != nil || out != supplychaincore.FanOutPendingNoop {
			t.Fatalf("second flag during arrival %d = %q %v, want pending_noop: a burst coalesces", arrival, out, err)
		}
		result, err := c.handler.Handle(c.ctx, intent)
		if err != nil {
			t.Fatal(err)
		}
		if got := result.SubSignals["fanout_holders"]; got != 0 {
			t.Fatalf("run %d fanout_holders = %v, want 0: a flagged rerun fans out nothing", runs, got)
		}
		if err := c.queue.Ack(c.ctx, intent, result); err != nil {
			t.Fatal(err)
		}
	}
	// The arrivals stop: the last rerun is claimed, runs, and the item settles.
	last := c.raceClaim(convergenceHolder)
	result, err := c.handler.Handle(c.ctx, last)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.queue.Ack(c.ctx, last, result); err != nil {
		t.Fatal(err)
	}
	if st := fanOutItem(t, c.ctx, c.db, last.IntentID); st.status != "succeeded" {
		t.Fatalf("item = %+v, want succeeded once arrivals stop", st)
	}
	if c.raceTryClaim() {
		t.Fatal("the holder had another claim after arrivals stopped")
	}
}
