// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The requeue budget of the #10972 re-run contract (arbiter ruling, tests A, B
// and gate E). The earlier fan-out proofs each pinned one event: a flag, a
// one-hop rerun, a scoped pass. These pin the composition: after a change the
// queue reaches a fixpoint within a bounded number of executions, and a pass
// that is not about the change is never re-derived in full because of it.
// Each scenario returns its violations so the seeded self-test can require
// that the budget catches a known-bad flag.

const (
	// Test A: holders of the changed CVE (the convergence EPSS holder plus
	// 29 more), unrelated multi-CVE scopes, and how many of the unrelated
	// scopes have a genuine pass in flight when the change commits.
	quiesceHolders       = 30
	quiesceUnrelated     = 10
	quiesceUnrelatedCVEs = 3
	quiesceParked        = 6
	// quiesceDrainLimit only stops a runaway requeue loop.
	quiesceDrainLimit = 500

	// Test B: changed commits one tick apart, a full pass of the long holder
	// lasting steadyLongPassTicks, and the most runs the holder may take.
	steadyArrivals      = 30
	steadyLongPassTicks = 10
	steadyMaxLongRuns   = 2
	steadyLongScope     = "scope:10972:steady:long"
)

func quiesceHolderScope(i int) string {
	if i == 0 {
		return convergenceHolder
	}
	return fmt.Sprintf("scope:10972:quiesce:holder:%02d", i)
}

func quiesceUnrelatedScope(i int) string { return fmt.Sprintf("scope:10972:quiesce:unrelated:%02d", i) }

func quiesceUnrelatedCVEIDs(i int) []string {
	out := make([]string, 0, quiesceUnrelatedCVEs)
	for j := 0; j < quiesceUnrelatedCVEs; j++ {
		out = append(out, fmt.Sprintf("CVE-2026-71%02d%d", i, j))
	}
	return out
}

// seededFanOut swaps in the status-only in-flight flag when seeded.
func (c *convergence) seededFanOut(seeded bool) {
	if seeded {
		c.handler.FanOut = &statusOnlyInFlightFanOut{SupplyChainImpactFanOut: c.fanOut, db: c.db}
	}
}

// runQuiesceAfterOneChange is test A: one source change while six unrelated
// passes are in flight, then an identical rescan. It returns every budget
// violation.
func runQuiesceAfterOneChange(t *testing.T, seeded bool) []string {
	t.Helper()
	c := newConvergence(t, false)
	meter := c.instrumentPassModes()
	for i := 1; i < quiesceHolders; i++ {
		c.seedQuiesceEPSSHolder(quiesceHolderScope(i))
		c.stampSetupItem(quiesceHolderScope(i))
	}
	unrelated := make([]string, 0, quiesceUnrelated)
	isUnrelated := map[string]bool{}
	for i := 0; i < quiesceUnrelated; i++ {
		scope := quiesceUnrelatedScope(i)
		c.seedQuiesceUnrelatedScope(scope, quiesceUnrelatedCVEIDs(i))
		unrelated = append(unrelated, scope)
		isUnrelated[scope] = true
	}
	c.drainCounted(meter, quiesceDrainLimit)
	if open := c.openSupplyChainItems(); open != 0 {
		t.Fatalf("setup did not settle: %d items open", open)
	}
	if copies := c.copies(); len(copies) != 1+quiesceHolders {
		t.Fatalf("setup copies of %s = %d scopes, want %d (source + holders): %v", convergenceCVE, len(copies), 1+quiesceHolders, copies)
	}
	c.seededFanOut(seeded)

	// Six genuine unrelated passes have loaded their evidence and not
	// committed when the change commits.
	for _, scope := range unrelated[:quiesceParked] {
		c.rerun(scope)
	}
	parked := make([]*gatedPass, 0, quiesceParked)
	for range quiesceParked {
		intent := c.raceClaim("")
		if !isUnrelated[intent.ScopeID] {
			t.Fatalf("claimed %s while parking unrelated passes", intent.ScopeID)
		}
		parked = append(parked, c.startGatedPass(intent))
	}
	c.setSourceStatus("Rejected")
	c.rerun(convergenceSource)
	executions := []quiescePass{c.handleCounted(meter, c.raceClaim(convergenceSource))}
	for _, pass := range parked {
		pass.finish()
	}
	xmins := c.findingXmins(unrelated)
	executions = append(executions, c.drainCounted(meter, quiesceDrainLimit)...)

	var violations []string
	if open := c.openSupplyChainItems(); open != 0 {
		violations = append(violations, fmt.Sprintf("queue did not drain: %d supply_chain_impact items still open after %d executions", open, len(executions)))
	}
	if bound := 1 + quiesceHolders + quiesceParked; len(executions) > bound {
		violations = append(violations, fmt.Sprintf("executions = %d, want at most %d (1 source + %d holders of %s + %d passes in flight at the commit): per scope %v",
			len(executions), bound, quiesceHolders, convergenceCVE, quiesceParked, quiescePassCounts(executions)))
	}
	var full []string
	for _, pass := range executions {
		if isUnrelated[pass.scope] && quiesceModeIsFull(pass.mode) {
			full = append(full, pass.scope+"="+pass.mode)
		}
	}
	if len(full) > 0 {
		sort.Strings(full)
		violations = append(violations, fmt.Sprintf("unrelated scopes ran full passes after a change that does not touch them: %v", full))
	}
	if changed := changedKeys(xmins, c.findingXmins(unrelated)); len(changed) > 0 {
		violations = append(violations, fmt.Sprintf("unrelated finding rows were rewritten (xmin changed) by reruns the change caused: %v", changed))
	}
	for scope, state := range c.copies() {
		if state != "advisory_retracted" {
			violations = append(violations, fmt.Sprintf("accuracy: %s copy of %s is %q, want advisory_retracted", scope, convergenceCVE, state))
		}
	}

	// An identical rescan must cost the source pass and nothing else.
	reopened := c.reopenedAts()
	watermark := c.stagingWatermark()
	c.rescanSource("g2", "")
	rescan := c.drainCounted(meter, quiesceDrainLimit)
	if len(rescan) != 1 || rescan[0].scope != convergenceSource {
		violations = append(violations, fmt.Sprintf("identical rescan ran %v, want the source pass only", rescan))
	}
	after := c.reopenedAts()
	var reopenedAgain []string
	for id, at := range reopened {
		if after[id] != at {
			reopenedAgain = append(reopenedAgain, id)
		}
	}
	if len(reopenedAgain) > 0 {
		sort.Strings(reopenedAgain)
		violations = append(violations, fmt.Sprintf("identical rescan changed reopened_at on %v", reopenedAgain))
	}
	if staged := c.stagedFactMarks(watermark); staged != 0 {
		violations = append(violations, fmt.Sprintf("identical rescan staged %d winners dirty fact marks, want 0", staged))
	}
	t.Logf("QUIESCE seeded=%v executions=%d per_scope=%v rescan=%v violations=%d", seeded, len(executions), quiescePassCounts(executions), rescan, len(violations))
	return violations
}

// quiescePassCounts summarizes executions as scope -> runs (modes).
func quiescePassCounts(passes []quiescePass) map[string]string {
	runs := map[string][]string{}
	for _, pass := range passes {
		runs[pass.scope] = append(runs[pass.scope], pass.mode)
	}
	out := make(map[string]string, len(runs))
	for scope, modes := range runs {
		out[scope] = fmt.Sprintf("%d%v", len(modes), modes)
	}
	return out
}

// steadyGate parks a FULL pass of one scope at its writer until released, so
// a full pass lasts as long as the tick driver says; a scoped pass and a pass
// that writes nothing run straight through.
type steadyGate struct {
	inner   supplychaincore.SupplyChainImpactWriter
	scope   string
	parked  chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *steadyGate) WriteSupplyChainImpactFindings(ctx context.Context, w supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	if w.ScopeID == g.scope && len(w.ScopedCVEIDs) == 0 {
		g.once.Do(func() { close(g.parked); <-g.release })
	}
	return g.inner.WriteSupplyChainImpactFindings(ctx, w)
}

// steadyLongPass is the long holder's pass in flight.
type steadyLongPass struct {
	gate      *steadyGate
	done      chan error
	releaseAt int
}

// startSteadyLong runs the long holder's claimed pass in the background. It
// returns the pass when it parked (a full pass, released releaseAt ticks
// later) and nil when it finished without parking.
func (c *convergence) startSteadyLong(intent reducercontract.Intent, tick int) *steadyLongPass {
	c.t.Helper()
	gate := &steadyGate{inner: c.handler.Writer, scope: steadyLongScope, parked: make(chan struct{}), release: make(chan struct{})}
	handler := c.handler
	handler.Writer = gate
	pass := &steadyLongPass{gate: gate, done: make(chan error, 1)}
	go func() {
		result, err := handler.Handle(c.ctx, intent)
		if err != nil {
			pass.done <- err
			return
		}
		pass.done <- c.queue.Ack(c.ctx, intent, result)
	}()
	select {
	case <-gate.parked:
		pass.releaseAt = tick + steadyLongPassTicks
		return pass
	case err := <-pass.done:
		if err != nil {
			c.t.Fatalf("long holder pass: %v", err)
		}
		return nil
	}
}

func (p *steadyLongPass) finish(t *testing.T) {
	t.Helper()
	close(p.gate.release)
	if err := <-p.done; err != nil {
		t.Fatalf("long holder pass: %v", err)
	}
}

// runSteadyArrivals is test B, driven by ticks, not the wall clock: each tick
// one changed source pass commits, and a full pass of the long holder lasts
// steadyLongPassTicks ticks. It returns every budget violation.
func runSteadyArrivals(t *testing.T, seeded bool) []string {
	t.Helper()
	c := newConvergence(t, false)
	c.seedQuiesceUnrelatedScope(steadyLongScope, []string{"CVE-2026-72001", "CVE-2026-72002", "CVE-2026-72003"})
	c.drain()
	c.seededFanOut(seeded)
	var long *steadyLongPass
	runs, fullRuns := 0, 0
	drainSync := func(tick int) {
		for i := 0; i < quiesceDrainLimit; i++ {
			intent, ok, err := c.queue.Claim(c.ctx)
			if err != nil {
				t.Fatalf("claim: %v", err)
			}
			if !ok {
				return
			}
			if intent.ScopeID == steadyLongScope {
				if long != nil {
					t.Fatalf("long holder claimed while its pass is in flight")
				}
				runs++
				if long = c.startSteadyLong(intent, tick); long != nil {
					fullRuns++
				}
				continue
			}
			result, err := c.handler.Handle(c.ctx, intent)
			if err != nil {
				t.Fatalf("handle %s: %v", intent.ScopeID, err)
			}
			if err := c.queue.Ack(c.ctx, intent, result); err != nil {
				t.Fatalf("ack %s: %v", intent.ScopeID, err)
			}
		}
		t.Fatalf("runaway requeue loop at tick %d", tick)
	}
	c.rerun(steadyLongScope)
	drainSync(0)
	if long == nil {
		t.Fatal("the long holder's genuine pass did not park: the fixture measures nothing")
	}
	status, settled := "Analyzed", false
	lastTick := steadyArrivals + 20*steadyLongPassTicks
	for tick := 0; tick < lastTick && !settled; tick++ {
		if long != nil && tick >= long.releaseAt {
			long.finish(t)
			long = nil
		}
		drainSync(tick)
		if tick < steadyArrivals {
			status = map[string]string{"Analyzed": "Rejected", "Rejected": "Analyzed"}[status]
			c.setSourceStatus(status)
			c.rerun(convergenceSource)
			drainSync(tick)
			continue
		}
		settled = long == nil && c.openSupplyChainItems() == 0
	}
	var violations []string
	if !settled {
		violations = append(violations, fmt.Sprintf("did not settle within %d ticks after arrivals stopped (long pass in flight=%v)", lastTick-steadyArrivals, long != nil))
		if long != nil {
			long.finish(t)
		}
	}
	if runs > steadyMaxLongRuns {
		violations = append(violations, fmt.Sprintf("long holder ran %d times (%d full) over %d arrivals one tick apart with a %d-tick full pass, want at most %d: each full rerun lasts long enough to be flagged again",
			runs, fullRuns, steadyArrivals, steadyLongPassTicks, steadyMaxLongRuns))
	}
	want := map[string]string{"Rejected": "advisory_retracted", "Analyzed": "active"}[status]
	for scope, state := range c.copies() {
		if state != want {
			violations = append(violations, fmt.Sprintf("accuracy: %s copy of %s is %q after the last arrival (%s), want %q", scope, convergenceCVE, state, status, want))
		}
	}
	t.Logf("STEADY seeded=%v arrivals=%d long_pass_ticks=%d long_runs=%d full=%d violations=%d", seeded, steadyArrivals, steadyLongPassTicks, runs, fullRuns, len(violations))
	return violations
}

// TestSupplyChainImpactQuiescesAfterOneChangeLive is test A of the #10972
// re-run contract: 30 holders of one CVE, 10 unrelated multi-CVE scopes, 6 of
// them in flight, one source change. The queue drains within one source pass
// plus one pass per holder plus one per pass in flight; no unrelated scope runs
// a full pass or rewrites a finding row; an identical rescan then costs the
// source pass alone, reopens nothing and stages no winners dirty mark.
func TestSupplyChainImpactQuiescesAfterOneChangeLive(t *testing.T) {
	for _, v := range runQuiesceAfterOneChange(t, false) {
		t.Errorf("requeue budget: %s", v)
	}
}

// TestSupplyChainImpactSteadyArrivalsConvergeLive is test B: changed commits
// arrive one tick apart for 30 ticks while an unrelated holder's full pass
// lasts 10 ticks. The holder runs at most twice: its genuine pass, then one
// rerun that is not a full re-derivation and so is not flagged again.
func TestSupplyChainImpactSteadyArrivalsConvergeLive(t *testing.T) {
	for _, v := range runSteadyArrivals(t, false) {
		t.Errorf("requeue budget: %s", v)
	}
}

// TestSupplyChainImpactRequeueBudgetCatchesTheStatusOnlyInFlightFlagLive is
// gate E's seeded violation: with the status-only in-flight flag (its own
// status-only read, reopens that name no change) both budget scenarios must
// report a violation, so the gate is proven able to fail.
func TestSupplyChainImpactRequeueBudgetCatchesTheStatusOnlyInFlightFlagLive(t *testing.T) {
	if v := runQuiesceAfterOneChange(t, true); len(v) == 0 {
		t.Fatal("test A reported no violation under the status-only in-flight flag: the budget cannot fail")
	}
	if v := runSteadyArrivals(t, true); len(v) == 0 {
		t.Fatal("test B reported no violation under the status-only in-flight flag: the budget cannot fail")
	}
}
