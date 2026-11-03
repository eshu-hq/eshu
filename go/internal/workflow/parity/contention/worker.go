// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// zombie is a claim its worker abandoned on purpose (§5 step 4). After the
// reaper expires it and another worker re-claims the item, the worker writes
// once with the old token (§5 step 9) and expects the fence rejection.
type zombie struct {
	item      workflow.WorkItem
	claim     workflow.Claim
	worker    int
	pinned    bool
	fired     bool
	firedCode parity.Code
	op        string
}

// worker is one claim loop, the production shape: one claim at a time per
// owner, W of them concurrently, with no rotation salt (production callers
// leave it empty, so the server draws per call). Before windowEnd it runs the chaos mix;
// after it, only claim and complete until the drain is confirmed.
func (r *runner) worker(w int) {
	// #nosec G115, G404 -- deterministic chaos RNG: Seed is refused unless a positive integer (spec.go positiveInt), unset means 0, and w is a non-negative worker index; the PCG stream is simulation randomness, never security material.
	rng := rand.New(rand.NewPCG(uint64(r.spec.Seed), uint64(w)+1))
	owner := r.ids.owner(w)
	quotaOn := r.leg.Quota && r.spec.QuotaBudget > 0
	for n := 0; ; n++ {
		select {
		case <-r.workCtx.Done():
			return
		case <-r.drainDone:
			return
		default:
		}
		r.fireZombies(w)
		chaos := time.Now().Before(r.windowEnd)
		kind := r.opt.CollectorKind
		if quotaOn && rng.Float64() < r.opt.Mix.QuotaShare {
			kind = r.opt.QuotaKind
		}
		sel := workflow.ClaimSelector{
			CollectorKind: kind, CollectorInstanceID: InstanceID,
			OwnerID: owner, ClaimID: r.ids.claim(w, n),
		}
		item, claim, issued, ok := r.claim(sel)
		if !ok {
			return
		}
		if !issued {
			r.idle()
			continue
		}
		if chaos {
			r.scopes.add(item.ScopeID)
		}
		if kind == r.opt.QuotaKind || !chaos {
			r.end(item, claim, endComplete, false)
			continue
		}
		r.chaosStep(rng, w, item, claim)
	}
}

// claim issues one ClaimNextEligible and feeds the grant to the tracker. It
// returns ok=false when the worker must stop (abort or cancelled run).
func (r *runner) claim(sel workflow.ClaimSelector) (workflow.WorkItem, workflow.Claim, bool, bool) {
	ctx, cancel := context.WithTimeout(r.workCtx, r.opt.CallTimeout)
	defer cancel()
	t0 := time.Now()
	item, claim, issued, err := r.leg.Backend.ClaimNextEligible(ctx, sel, t0.UTC(), r.spec.LeaseTTL)
	t1 := time.Now()
	op := "claim_idle"
	switch {
	case issued:
		op = "claim_issued"
	case parity.Classify(err) == parity.CodePaced:
		// A quota or pacing deferral is the throttle answer, not a fault:
		// the satellite reports an at-budget scope this way (§5 step 5).
		op = "claim_paced"
	}
	r.rec.observe(op, t1.Sub(t0), err)
	if op == "claim_paced" {
		r.idle()
		return item, claim, false, true
	}
	if err != nil {
		if r.workCtx.Err() != nil {
			return item, claim, false, false
		}
		if isDeadlock(err) {
			r.abort(violation(TriggerDeadlock, "claim: %v", err))
			return item, claim, false, false
		}
		r.anomalies.addErr("claim_fault", err)
		r.idle()
		return item, claim, false, true
	}
	if !issued {
		return item, claim, false, true
	}
	if v := r.tracker.onGrant(item, claim, t0, t1); v != nil {
		r.abort(v)
		return item, claim, false, false
	}
	return item, claim, true, true
}

func (r *runner) idle() {
	select {
	case <-time.After(r.opt.IdlePoll):
	case <-r.workCtx.Done():
	}
}

// chaosStep draws the §5 behaviour for one main-kind claim.
func (r *runner) chaosStep(rng *rand.Rand, w int, item workflow.WorkItem, claim workflow.Claim) {
	mix := r.opt.Mix
	if rng.Float64() < mix.HeartbeatSkip {
		z := &zombie{item: item, claim: claim, worker: w}
		if r.leg.Pin && rng.Float64() < mix.ZombiePin {
			// The pin outlives the lease: the reaper must hold off until
			// the commit deadline, then reclaim.
			deadline := time.Now().UTC().Add(r.spec.LeaseTTL + r.spec.ReapInterval)
			z.pinned = r.pin(item, claim, deadline, "pin_zombie") == parity.CodeOK
		}
		r.tracker.onAbandon(item.WorkItemID, claim.FencingToken)
		r.zombiesMu.Lock()
		r.zombies = append(r.zombies, z)
		r.zombiesMu.Unlock()
		return
	}
	if rng.Float64() < mix.LongHold && !r.hold(item, claim) {
		return
	}
	if r.leg.Pin {
		switch draw := rng.Float64(); {
		case draw < mix.Pin:
			r.pin(item, claim, time.Now().UTC().Add(r.spec.LeaseTTL), "pin")
		case draw < mix.Pin+mix.PinOutlive:
			// The pin deadline passes while the lease is still live, so the
			// completion that follows must still be accepted.
			short := min(250*time.Millisecond, r.spec.LeaseTTL/8)
			r.pin(item, claim, time.Now().UTC().Add(short), "pin_outlive")
			r.sleep(2 * short)
		}
	}
	kind := pickEnd(rng.Float64(), mix)
	r.end(item, claim, kind, kind.terminal() && rng.Float64() < mix.Replay)
}

func pickEnd(draw float64, mix Mix) endKind {
	switch {
	case draw < mix.Complete:
		return endComplete
	case draw < mix.Complete+mix.Release:
		return endRelease
	case draw < mix.Complete+mix.Release+mix.FailRetryable:
		return endFailRetryable
	default:
		return endFailTerminal
	}
}

func (r *runner) sleep(d time.Duration) {
	select {
	case <-time.After(d):
	case <-r.workCtx.Done():
	}
}

func (r *runner) mutation(item workflow.WorkItem, claim workflow.Claim) workflow.ClaimMutation {
	now := time.Now().UTC()
	return workflow.ClaimMutation{
		WorkItemID: item.WorkItemID, ClaimID: claim.ClaimID, FencingToken: claim.FencingToken,
		OwnerID: claim.OwnerID, ObservedAt: now, LeaseDuration: r.spec.LeaseTTL, VisibleAt: now,
		FailureClass: "synthetic_contention", FailureMessage: "seeded contention failure",
	}
}

// call routes one ending or heartbeat mutation to the backend. The wrap
// keeps errors.Is and errors.As, so parity.Classify still sees the sentinel.
func (r *runner) call(ctx context.Context, op string, m workflow.ClaimMutation) error {
	b := r.leg.Backend
	var err error
	switch op {
	case "heartbeat":
		err = b.HeartbeatClaim(ctx, m)
	case "complete":
		err = b.CompleteClaim(ctx, m)
	case "release":
		err = b.ReleaseClaim(ctx, m)
	case "fail_retryable":
		err = b.FailClaimRetryable(ctx, m)
	default:
		err = b.FailClaimTerminal(ctx, m)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w", op, m.WorkItemID, err)
	}
	return nil
}

func endOp(kind endKind) string {
	return [...]string{"complete", "release", "fail_retryable", "fail_terminal"}[kind]
}

// end runs one ending mutation. With replay set (§5 step 8) the first call
// gets an injected timeout and is then replayed once on a fresh deadline:
// the replay must ack whether or not the first attempt applied.
func (r *runner) end(item workflow.WorkItem, claim workflow.Claim, kind endKind, replay bool) {
	base := endOp(kind)
	op := base
	m := r.mutation(item, claim)
	if replay {
		first, cancel := context.WithTimeout(r.workCtx, r.opt.ReplayTimeout)
		t0 := time.Now()
		err := r.call(first, base, m)
		cancel()
		r.rec.observe(base+"_replay_first", time.Since(t0), err)
		op = base + "_replay"
	}
	ctx, cancel := context.WithTimeout(r.workCtx, r.opt.CallTimeout)
	defer cancel()
	t0 := time.Now()
	err := r.call(ctx, base, m)
	t1 := time.Now()
	code := r.rec.observe(op, t1.Sub(t0), err)
	switch {
	case code == parity.CodeOK:
		if v := r.tracker.onEnd(item.WorkItemID, claim.FencingToken, kind, t0, t1); v != nil {
			r.abort(v)
		}
	case r.workCtx.Err() != nil:
	case isDeadlock(err):
		r.abort(violation(TriggerDeadlock, "%s: %v", op, err))
	case replay:
		r.anomalies.addErr("replay_not_acked", err)
	case code == parity.CodeRejected:
		r.anomalies.addErr("live_claim_rejected", err)
		if v := r.tracker.onRejected(item.WorkItemID, claim.FencingToken, t0); v != nil {
			r.abort(v)
		}
	default:
		r.anomalies.addErr(op+"_fault", err)
	}
}

// hold keeps a claim past the heartbeat interval, heartbeating on schedule
// (§5 step 2). It returns false when the lease was lost.
func (r *runner) hold(item workflow.WorkItem, claim workflow.Claim) bool {
	until := time.Now().Add(r.spec.HeartbeatInterval * 3 / 2)
	for time.Now().Before(until) {
		r.sleep(min(r.spec.HeartbeatInterval, time.Until(until)))
		if r.workCtx.Err() != nil || !time.Now().Before(until) {
			break
		}
		m := r.mutation(item, claim)
		ctx, cancel := context.WithTimeout(r.workCtx, r.opt.CallTimeout)
		t0 := time.Now()
		err := r.leg.Backend.HeartbeatClaim(ctx, m)
		cancel()
		if code := r.rec.observe("heartbeat", time.Since(t0), err); code != parity.CodeOK {
			if r.workCtx.Err() == nil {
				r.anomalies.addErr("heartbeat_lost_live_lease", err)
			}
			if code == parity.CodeRejected {
				if v := r.tracker.onRejected(item.WorkItemID, claim.FencingToken, t0); v != nil {
					r.abort(v)
				}
			}
			return false
		}
		r.tracker.onRenew(item.WorkItemID, claim.FencingToken, m.ObservedAt)
	}
	return r.workCtx.Err() == nil
}

// pin calls ValidateAndPinClaimForCommit on the live epoch (§5 step 7).
func (r *runner) pin(item workflow.WorkItem, claim workflow.Claim, deadline time.Time, op string) parity.Code {
	ctx, cancel := context.WithTimeout(r.workCtx, r.opt.CallTimeout)
	defer cancel()
	t0 := time.Now()
	_, err := r.leg.Backend.PinForCommit(ctx, claim.ClaimID, claim.FencingToken, item.ScopeID,
		item.SourceRunID, item.GenerationID, deadline)
	code := r.rec.observe(op, time.Since(t0), err)
	r.pins.add(op, code)
	return code
}

// fireZombies runs the step-9 stale write for each of this worker's
// abandoned claims whose item was re-granted at a higher token.
func (r *runner) fireZombies(w int) {
	r.zombiesMu.Lock()
	var ready []*zombie
	for _, z := range r.zombies {
		if z.worker == w && !z.fired && r.tracker.hasNewerGrant(z.item.WorkItemID, z.claim.FencingToken) {
			z.fired = true
			ready = append(ready, z)
		}
	}
	r.zombiesMu.Unlock()
	for i, z := range ready {
		r.staleWrite(z, i)
	}
}

// staleWrite writes once with the superseded token. Success is a fencing
// failure and aborts; any code other than the fence rejection is recorded.
func (r *runner) staleWrite(z *zombie, rotation int) {
	ops := []string{"heartbeat", "complete", "fail_terminal"}
	if r.leg.Pin {
		ops = append(ops, "pin")
	}
	op := ops[(int(z.claim.FencingToken)+rotation)%len(ops)]
	ctx, cancel := context.WithTimeout(r.workCtx, r.opt.CallTimeout)
	defer cancel()
	t0 := time.Now()
	var err error
	if op == "pin" {
		_, err = r.leg.Backend.PinForCommit(ctx, z.claim.ClaimID, z.claim.FencingToken, z.item.ScopeID,
			z.item.SourceRunID, z.item.GenerationID, time.Now().UTC().Add(r.spec.LeaseTTL))
	} else {
		err = r.call(ctx, op, r.mutation(z.item, z.claim))
	}
	code := r.rec.observe("stale_"+op, time.Since(t0), err)
	r.zombiesMu.Lock()
	z.firedCode, z.op = code, op
	r.zombiesMu.Unlock()
	if code == parity.CodeOK {
		r.abort(violation(TriggerStaleWriteAccept, "stale %s on item %s accepted at superseded token %d",
			op, z.item.WorkItemID, z.claim.FencingToken))
	}
}

// pendingZombies counts abandoned claims whose stale write has not fired.
func (r *runner) pendingZombies() int {
	r.zombiesMu.Lock()
	defer r.zombiesMu.Unlock()
	n := 0
	for _, z := range r.zombies {
		if !z.fired {
			n++
		}
	}
	return n
}
