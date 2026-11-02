// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow/parity"
)

// analyze reads the post-run state before cleanup: zero loss and drain,
// the full fencing check, reclaim latencies, quota buckets, and the
// recorder and sampler aggregates.
func (r *runner) analyze(ctx context.Context, report *Report) {
	report.Created = r.created.Load()
	report.Terminal = r.tracker.terminalCount()
	latency, codes, total := r.rec.summaries()
	report.ClientLatency, report.Codes, report.OpsTotal = latency, codes, total
	report.SlowestCalls = r.rec.slowCalls()
	if !report.Phases.WindowStart.IsZero() {
		if secs := report.Phases.WorkEnd.Sub(report.Phases.WindowStart).Seconds(); secs > 0 {
			report.OpsPerSecond = float64(total) / secs
		}
	}
	report.Anomalies = r.anomalies.snapshot()
	report.AnomalySamples = r.anomalies.texts()
	report.Pins = r.pins.snapshot()
	r.analyzeItems(ctx, report)
	r.analyzeReclaim(ctx, report)
	report.StaleWrites = r.staleTally()
	r.advisory.mu.Lock()
	report.Advisory = map[string]any{
		"rounds": r.advisory.rounds, "clean_rounds": r.advisory.cleanRound,
		"duplicates": r.advisory.duplicates, "errors": r.advisory.errors, "goroutines": r.opt.AdvisoryGoroutines,
	}
	r.advisory.mu.Unlock()
	if buckets, err := r.leg.Observer.QuotaBuckets(ctx); err == nil {
		report.Quota = buckets
	}
	granted := r.scopes.snapshot()
	for k := range r.spec.BacklogScopes {
		if granted[r.ids.scope(k)] == 0 {
			report.UnclaimedScopes = append(report.UnclaimedScopes, r.ids.scope(k))
		}
	}
	r.samples.mu.Lock()
	report.Samples = r.samples.records
	query := summarize(r.samples.queryMS)
	report.Sampler = map[string]any{
		"samples": r.samples.count, "interval": r.opt.SampleInterval.String(),
		"disabled": r.opt.SamplerDisabled, "query_ms": query, "errors": r.samples.errors,
	}
	if r.samples.headSeen {
		peak := r.samples.maxHead
		report.Headroom.Peak = &peak
	}
	r.samples.mu.Unlock()
}

// analyzeItems is zero loss and drain to zero after the run: every created
// item still exists, every client-acked terminal is terminal in the
// database, and only held quota items remain non-terminal.
func (r *runner) analyzeItems(ctx context.Context, report *Report) {
	snap, err := r.leg.Observer.Snapshot(ctx)
	if err == nil && int64(snap.ItemTotal) < report.Created {
		r.abortPostRun(violation(TriggerLostItem, "after the run %d synthetic items exist, %d created", snap.ItemTotal, report.Created))
	}
	if err == nil && snap.ItemTotal > r.maxItems {
		r.abortPostRun(violation(TriggerQueueDepth, "after the run %d synthetic items exist, cap %d", snap.ItemTotal, r.maxItems))
	}
	if bad, err := r.leg.Observer.FencingViolations(ctx); err == nil && len(bad) > 0 {
		r.abortPostRun(violation(TriggerFencingRegression, "final fencing check: %v", bad))
	}
	if twice, err := r.leg.Observer.DoubleTerminal(ctx); err == nil && len(twice) > 0 {
		r.abortPostRun(violation(TriggerExactlyOnce, "final check: items with two terminal claims: %v", twice))
	}
	left, err := r.leg.Observer.NonTerminalItems(ctx, 1000)
	if err != nil {
		report.NonTerminal = []string{"ERROR: " + err.Error()}
		return
	}
	for _, entry := range left {
		if r.isQuotaHeldEntry(entry) {
			report.QuotaHeld++
			continue
		}
		id, _, _ := strings.Cut(entry, ":")
		if r.tracker.isTerminal(id) {
			r.abortPostRun(violation(TriggerLostItem, "item %s acked terminal but is %s in the database", id, entry))
		}
		if len(report.NonTerminal) < 50 {
			report.NonTerminal = append(report.NonTerminal, entry)
		}
	}
}

// analyzeReclaim measures reap time minus the later of lease expiry and the
// commit pin for each abandoned claim.
func (r *runner) analyzeReclaim(ctx context.Context, report *Report) {
	r.zombiesMu.Lock()
	zs := append([]*zombie(nil), r.zombies...)
	r.zombiesMu.Unlock()
	report.Reclaim.Abandoned = len(zs)
	bound := r.spec.ReapInterval + time.Duration(r.reapPassMax.Load()) + time.Second + r.leg.ClockSkew
	report.Reclaim.BoundMS = ms(bound)
	if len(zs) == 0 {
		return
	}
	idList := make([]string, 0, len(zs))
	for _, z := range zs {
		idList = append(idList, z.claim.ClaimID)
	}
	rows, err := r.leg.Observer.ClaimRows(ctx, idList)
	if err != nil {
		report.Reclaim.NotReaped = []string{"ERROR: " + err.Error()}
		return
	}
	var delays []time.Duration
	for _, z := range zs {
		row, ok := rows[z.claim.ClaimID]
		if !ok || row.Status != "expired" || row.FinishedAt.IsZero() {
			report.Reclaim.NotReaped = append(report.Reclaim.NotReaped, fmt.Sprintf("%s:%s", z.claim.ClaimID, row.Status))
			continue
		}
		report.Reclaim.Reaped++
		expiry := row.LeaseExpiresAt
		if row.PinnedUntil.After(expiry) {
			expiry = row.PinnedUntil
		}
		if !row.PinnedUntil.IsZero() && row.FinishedAt.Before(row.PinnedUntil.Add(-r.leg.ClockSkew)) {
			report.Reclaim.PinEarly = append(report.Reclaim.PinEarly, z.claim.ClaimID)
		}
		delay := row.FinishedAt.Sub(expiry)
		delays = append(delays, delay)
		if delay > bound {
			report.Reclaim.Late = append(report.Reclaim.Late, fmt.Sprintf("%s:%s", z.claim.ClaimID, delay))
		}
	}
	report.Reclaim.Latency = summarize(delays)
}

func (r *runner) staleTally() map[string]int {
	r.zombiesMu.Lock()
	defer r.zombiesMu.Unlock()
	out := map[string]int{"armed": len(r.zombies)}
	for _, z := range r.zombies {
		if z.fired {
			out["fired"]++
			out[z.op+"="+string(z.firedCode)]++
			out["code="+string(z.firedCode)]++
		}
	}
	return out
}

// finish fills the §6 criteria and the verdict.
func (r *runner) finish(report *Report) {
	r.abortMu.Lock()
	report.Abort = r.violation
	r.abortMu.Unlock()
	trig := Trigger("")
	if report.Abort != nil {
		trig = report.Abort.Trigger
	}
	c := report.Criteria
	set := func(name string, ok bool, detail string, args ...any) {
		status := StatusPass
		if !ok {
			status = StatusFail
		}
		c[name] = Criterion{Status: status, Detail: fmt.Sprintf(detail, args...)}
	}
	set("exactly_once", trig != TriggerDualClaim && trig != TriggerExactlyOnce,
		"%d items acked terminal; tracker and 1 s sampler found no dual live claim or double terminal", report.Terminal)
	set("fencing_monotonicity", trig != TriggerFencingRegression,
		"client happens-before order plus database token check (item token >= every claim token, no duplicate)")
	stale := report.StaleWrites
	set("stale_fence_rejection", stale["fired"] > 0 && stale["code="+string(parity.CodeRejected)] == stale["fired"] &&
		trig != TriggerStaleWriteAccept, "%d stale writes fired, %d rejected (by op: %v)",
		stale["fired"], stale["code="+string(parity.CodeRejected)], stale)
	rc := report.Reclaim
	set("reclaim", rc.Abandoned > 0 && rc.Reaped == rc.Abandoned && len(rc.Late) == 0 && len(rc.PinEarly) == 0,
		"%d/%d abandoned leases reaped; late %d, pin violated %d, bound %.0fms", rc.Reaped, rc.Abandoned,
		len(rc.Late), len(rc.PinEarly), rc.BoundMS)
	replays := 0
	for op, codes := range report.Codes {
		if strings.HasSuffix(op, "_replay") {
			replays += codes[string(parity.CodeOK)] + codes[string(parity.CodeRejected)] + codes[string(parity.CodeFault)]
		}
	}
	set("replay_idempotence", replays > 0 && report.Anomalies["replay_not_acked"] == 0,
		"%d replays after an injected timeout, %d not acked", replays, report.Anomalies["replay_not_acked"])
	claimMax := report.ClientLatency["claim_issued"].MaxMS
	idleMax := report.ClientLatency["claim_idle"].MaxMS
	set("starvation_bound", max(claimMax, idleMax) <= ms(r.spec.StarvationBound) && len(report.UnclaimedScopes) == 0,
		"max claim wait %.1fms (bound %s); %d backlog scopes unclaimed in the window", max(claimMax, idleMax),
		r.spec.StarvationBound, len(report.UnclaimedScopes))
	r.quotaCriterion(report)
	set("zero_deadlocks", trig != TriggerDeadlock && trig != TriggerLockWait && r.rec.deadlockCount() == 0,
		"40P01 errors %d; lock wait bound %s (peak %s)", r.rec.deadlockCount(), r.spec.LockWaitBound, peakLockWait(report))
	set("zero_loss", trig != TriggerLostItem, "created %d; sampled created <= rows at every tick", report.Created)
	set("drain_to_zero", report.Phases.Drained && len(report.NonTerminal) == 0,
		"drained=%v, %d non-terminal, %d quota-held", report.Phases.Drained, len(report.NonTerminal), report.QuotaHeld)
	single := report.Codes["heartbeat"][string(parity.CodeOK)]
	set("single_holder_heartbeat", single > 0 && report.Anomalies["heartbeat_lost_live_lease"] == 0,
		"%d heartbeats acked on live leases, %d lost", single, report.Anomalies["heartbeat_lost_live_lease"])
	set("no_unexpected_outcomes", len(report.Anomalies) == 0, "anomalies %v", report.Anomalies)
	res := report.Residue
	set("residue", res.FirstZero == 0 && len(res.SecondZeroDelta) == 0 && (!res.ThirdZero.Checked || res.ThirdZero.Rows == 0),
		"synthetic %d, non-synthetic deltas %v, core %s", res.FirstZero, res.SecondZeroDelta, res.ThirdZero.Status)
	if r.leg.Advisory {
		rounds, _ := report.Advisory["rounds"].(int)
		set("advisory_admission", rounds > 0 && len(r.advisory.duplicates) == 0 && len(r.advisory.errors) == 0,
			"%d rounds of %d goroutines; duplicates %v", rounds, r.opt.AdvisoryGoroutines, r.advisory.duplicates)
	} else {
		c["advisory_admission"] = Criterion{Status: StatusNotApplicable, Detail: report.NotApplicable["step6_advisory"]}
	}
	if r.leg.Pin {
		ok := report.Pins["pin="+string(parity.CodeOK)]
		set("pin", ok > 0 && report.Pins["pin="+string(parity.CodeRejected)] == 0,
			"pins %v; stale pins are judged in stale_fence_rejection", report.Pins)
	} else {
		c["pin"] = Criterion{Status: StatusNotApplicable, Detail: report.NotApplicable["step7_pin"]}
	}
	report.Verdict = verdict(report, r)
}

func (r *runner) quotaCriterion(report *Report) {
	if _, na := report.NotApplicable["step5_quota"]; na {
		report.Criteria["no_overshoot"] = Criterion{Status: StatusNotApplicable, Detail: report.NotApplicable["step5_quota"]}
		return
	}
	// Held items are informational: a run that crosses an hour opens a new
	// bucket and may drain them legitimately. The criterion is no bucket
	// above budget, with at least one bucket driven to budget so the
	// reservation path ran under overload.
	var over []string
	atBudget := 0
	for _, b := range report.Quota {
		if b.Grants > r.spec.QuotaBudget {
			over = append(over, fmt.Sprintf("%s@%s=%d", b.ScopeID, b.WindowStart.Format(time.RFC3339), b.Grants))
		}
		if b.Grants == r.spec.QuotaBudget {
			atBudget++
		}
	}
	status := StatusPass
	if len(over) > 0 || atBudget == 0 {
		status = StatusFail
	}
	report.Criteria["no_overshoot"] = Criterion{Status: status, Detail: fmt.Sprintf(
		"budget %d per (kind, scope, hour); %d buckets, %d at budget, over budget %v; %d items held at the end",
		r.spec.QuotaBudget, len(report.Quota), atBudget, over, report.QuotaHeld)}
}

func peakLockWait(report *Report) string {
	if report.Headroom.Peak == nil || report.Headroom.Peak.MaxLockWaitMS == notAvailable {
		return StatusNotAvailable
	}
	return fmt.Sprintf("%.0fms", report.Headroom.Peak.MaxLockWaitMS)
}

func verdict(report *Report, r *runner) string {
	switch {
	case r.opt.SamplerDisabled:
		return VerdictOverhead
	case report.Abort != nil:
		return VerdictAborted
	}
	names := make([]string, 0, len(report.Criteria))
	for name := range report.Criteria {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if report.Criteria[name].Status == StatusFail {
			return VerdictFailed
		}
	}
	if reason := r.incomplete(report); reason != "" {
		report.Incomplete = reason
		return VerdictIncomplete
	}
	return VerdictClean
}

// incomplete names why a run without failures still cannot be CLEAN: the
// sampler never produced a reading, more reads failed than tolerated, or a
// live run lacks the headroom record or the core third zero.
func (r *runner) incomplete(report *Report) string {
	r.samples.mu.Lock()
	count, errTotal, headSeen, peak := r.samples.count, r.samples.errTotal, r.samples.headSeen, r.samples.maxHead
	r.samples.mu.Unlock()
	switch {
	case count == 0:
		return "the database sampler produced no reading"
	case errTotal > r.opt.MaxSampleErrors:
		return fmt.Sprintf("%d failed sampler or headroom reads, %d tolerated", errTotal, r.opt.MaxSampleErrors)
	case r.spec.Target == TargetLive && !headSeen:
		return "no pg_headroom reading on the live target"
	case r.spec.Target == TargetLive && missingCeilings(peak, r.spec.Headroom) != nil:
		return "an enforced pg_headroom ceiling was never measured: " + missingCeilings(peak, r.spec.Headroom).Error()
	case r.spec.Target == TargetLive && !report.Residue.ThirdZero.Checked:
		return "core third zero not checked on the live target"
	}
	return ""
}

// abortPostRun records a violation found by the end-of-run analysis.
func (r *runner) abortPostRun(v *Violation) {
	v.Source = SourcePostRun
	r.abort(v)
}
