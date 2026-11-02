// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// cleanupBudget bounds the reversal (drain wait, prefix deletes, residue)
// on its own context, so cleanup still runs after the wall-clock cap fired.
const cleanupBudget = 15 * time.Minute

// runner holds one seed and leg's live state.
type runner struct {
	spec Spec
	leg  Leg
	opt  Options
	ids  ids

	tracker *tracker
	rec     *recorder
	scopes  scopeGrants

	workCtx   context.Context
	cancel    context.CancelFunc
	windowEnd time.Time
	drainDone chan struct{}

	abortMu   sync.Mutex
	violation *Violation

	created  atomic.Int64
	maxItems int
	// baselineDirty is set when the baseline already held harness rows:
	// the run did not create them, so the reversal must not delete them.
	baselineDirty bool
	quotaHeldMax  int
	zombiesMu     sync.Mutex
	zombies       []*zombie
	advisory      advisoryTally
	reapPassMax   atomic.Int64
	samples       sampleLog
	anomalies     anomalyTally
	pins          pinTally
}

// Run executes the §5 mix for one seed on one leg and always returns a
// report, including after an abort: the reversal (stop, drain, prefix
// delete, residue) runs on every exit path. The error is non-nil when the
// run could not start or when cleanup itself failed; criteria failures and
// aborts are in the report, not the error.
func Run(ctx context.Context, spec Spec, leg Leg, opt Options) (*Report, error) {
	if leg.Backend == nil || leg.Observer == nil {
		return nil, errors.New("contention leg needs a backend and an observer")
	}
	if spec.Target == TargetLive && opt.SamplerDisabled {
		return nil, errors.New("the sampler cannot be disabled on the live target: overhead measurement is local only")
	}
	opt = opt.withDefaults(spec)
	r := &runner{
		spec: spec, leg: leg, opt: opt, ids: ids{seed: spec.Seed},
		tracker:   newTracker(spec.LeaseTTL, leg.ClockSkew),
		rec:       newRecorder(),
		drainDone: make(chan struct{}),
	}
	// Every write goes through the guard: synthetic ids only, this
	// harness's own instance only, no global routes.
	r.leg.Backend = newGuardedBackend(leg.Backend, leg.HarnessReaper, r.abort)
	r.maxItems = r.plannedItems()
	report := newReport(spec, leg, opt)
	report.MaxItems = r.maxItems
	report.CleanupBudget = cleanupBudget.String()
	capCtx, capCancel := context.WithTimeout(ctx, spec.WallClockCap)
	defer capCancel()
	r.workCtx, r.cancel = context.WithCancel(capCtx)
	defer r.cancel()
	go func() {
		<-capCtx.Done()
		switch {
		case errors.Is(capCtx.Err(), context.DeadlineExceeded):
			r.abort(violation(TriggerWallClock, "wall_clock_cap %s expired", spec.WallClockCap))
		case ctx.Err() != nil:
			// The caller cancelled (SIGINT or SIGTERM in the entry tests):
			// stop and reverse like any other abort.
			r.abort(violation(TriggerExternal, "run interrupted: %v", ctx.Err()))
		}
	}()

	report.Phases.Start = time.Now().UTC()
	watch := r.startWatcher(report)
	runErr := r.execute(report)
	report.Phases.WorkEnd = time.Now().UTC()
	r.cancel()

	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), cleanupBudget)
	defer cleanupCancel()
	r.analyze(cleanupCtx, report)
	cleanupErr := r.reverse(cleanupCtx, report)
	watch.stopWatcher()
	report.Phases.End = time.Now().UTC()
	r.finish(report)
	if runErr != nil {
		return report, runErr
	}
	return report, cleanupErr
}

// execute runs the phases before the reversal. A returned error means the
// run could not proceed (setup); a violation means it aborted.
func (r *runner) execute(report *Report) error {
	ctx := r.workCtx
	if r.aborted() {
		return nil
	}
	if err := r.isolationProbe(ctx); err != nil {
		return err
	}
	report.IsolationProbe = r.probeRecord()
	baseline, err := r.leg.Observer.Census(ctx)
	if err != nil {
		return fmt.Errorf("baseline census: %w", err)
	}
	report.Baseline = baseline
	if baseline.prefixedTotal() != 0 {
		r.baselineDirty = true
		r.abort(violation(TriggerResidue, "baseline already holds %d synthetic rows", baseline.prefixedTotal()))
		return nil
	}
	// The database sampler starts before the seed and runs through the
	// drain, so loss, dual claims, the queue cap, and non-synthetic changes
	// are watched while the backlog is written and during the stats wait.
	var bg sync.WaitGroup
	stopBG := make(chan struct{})
	defer func() {
		close(stopBG)
		bg.Wait()
	}()
	if !r.opt.SamplerDisabled {
		bg.Add(1)
		go func() { defer bg.Done(); r.sample(baseline, stopBG) }()
	}
	seedStart := time.Now()
	if err := r.seed(ctx); err != nil {
		if r.aborted() {
			return nil
		}
		return fmt.Errorf("seed backlog: %w", err)
	}
	report.Phases.SeedSeconds = time.Since(seedStart).Seconds()
	r.opt.Logf("%s: seeded %d items in %.1fs", r.leg.Name, r.created.Load(), report.Phases.SeedSeconds)
	r.waitForStats(report, seedStart)
	if r.aborted() {
		return nil
	}
	if r.leg.HarnessReaper {
		bg.Add(1)
		go func() { defer bg.Done(); r.reapLoop(stopBG) }()
	}
	r.windowEnd = time.Now().Add(r.spec.Window)
	report.Phases.WindowStart = time.Now().UTC()
	if r.leg.Advisory {
		bg.Add(1)
		go func() { defer bg.Done(); r.advisoryLoop() }()
	}
	var workers sync.WaitGroup
	for w := range r.spec.Workers() {
		workers.Add(1)
		go func(w int) { defer workers.Done(); r.worker(w) }(w)
	}
	r.drain(report)
	workers.Wait()
	return nil
}

// plannedItems is the queue-depth cap: the backlog, the quota workload, and
// at most one admitted advisory round per interval (plus one for timer
// slack). The sampler aborts when synthetic rows exceed it.
func (r *runner) plannedItems() int {
	n := r.spec.BacklogItems
	if r.leg.Quota && r.spec.QuotaBudget > 0 {
		n += r.spec.QuotaBudget + max(2, r.spec.QuotaBudget/2) + r.spec.QuotaBudget - 1
	}
	if r.leg.Advisory && r.opt.AdvisoryInterval > 0 {
		n += (int(r.spec.Window/r.opt.AdvisoryInterval) + 1) * r.opt.AdvisoryItems
	}
	return n
}

// waitForStats waits, read-only, until workflow_work_items was analyzed
// after the seed began, or until StatsWait runs out.
func (r *runner) waitForStats(report *Report, seedStart time.Time) {
	start := time.Now()
	deadline := start.Add(r.opt.StatsWait)
	for {
		at, err := r.leg.Observer.AnalyzedAt(r.workCtx)
		if err == nil && at.After(seedStart) {
			report.Phases.StatsReady = true
			break
		}
		if r.workCtx.Err() != nil || time.Now().After(deadline) {
			r.opt.Logf("%s: no ANALYZE of workflow_work_items within %s; the window runs on cold statistics", r.leg.Name, r.opt.StatsWait)
			break
		}
		r.sleep(time.Second)
	}
	report.Phases.StatsWaitSeconds = time.Since(start).Seconds()
}

// isolationProbe is the §4 dry claim: before any write, a claim on each
// harness selector must come back idle. An issued claim means the selector
// meets rows the harness did not create, so the run stops.
func (r *runner) isolationProbe(ctx context.Context) error {
	for _, kind := range []string{string(r.opt.CollectorKind), string(r.opt.QuotaKind)} {
		sel := workflow.ClaimSelector{
			CollectorKind: workflowKind(kind), CollectorInstanceID: InstanceID,
			OwnerID: r.ids.owner(0), ClaimID: Prefix + "claim-probe-" + kind,
		}
		item, _, issued, err := r.leg.Backend.ClaimNextEligible(ctx, sel, time.Now().UTC(), r.spec.LeaseTTL)
		if err != nil {
			return fmt.Errorf("isolation dry claim on %s: %w", kind, err)
		}
		if issued {
			r.abort(violation(TriggerNonSynthetic, "isolation dry claim on %s issued item %q before any write", kind, item.WorkItemID))
			return fmt.Errorf("isolation dry claim issued %q", item.WorkItemID)
		}
	}
	return nil
}

// probeRecord states what the §4 dry claim proves on this target. The
// harness instance is not an enumerated instance and holds no rows, so the
// idle answer is vacuous: it cannot show that the selector excludes real
// work (reading isolation_probe).
func (r *runner) probeRecord() Criterion {
	if r.spec.Target == TargetLive {
		return Criterion{Status: StatusVacuous, Detail: "dry claim idle on " + InstanceID +
			", which is not an enumerated live instance (synth-8875-tfstate, synth-8875-pkgreg) and has no rows; " +
			"#510 claims_enabled does not apply to it. It shows no row names this instance, not that real work is excluded."}
	}
	return Criterion{Status: StatusVacuous, Detail: "dry claim idle on " + InstanceID + "; a local target holds no real work to exclude"}
}

// drain waits out the window, then waits until every non-quota synthetic
// item is terminal, no claim is live, and every armed stale writer fired.
func (r *runner) drain(report *Report) {
	defer close(r.drainDone)
	select {
	case <-time.After(time.Until(r.windowEnd)):
	case <-r.workCtx.Done():
		return
	}
	report.Phases.DrainStart = time.Now().UTC()
	deadline := time.Now().Add(r.opt.DrainTimeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.workCtx.Done():
			return
		case <-ticker.C:
		}
		if r.drained() {
			report.Phases.Drained = true
			return
		}
		if time.Now().After(deadline) {
			r.opt.Logf("%s: drain timeout %s reached", r.leg.Name, r.opt.DrainTimeout)
			return
		}
	}
}

// drained reports the §6 drain-to-zero condition, leaving out only the
// quota items held above budget by design.
func (r *runner) drained() bool {
	if r.pendingZombies() > 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(r.workCtx, sampleTimeout)
	defer cancel()
	snap, err := r.leg.Observer.Snapshot(ctx)
	if err != nil || snap.ActiveClaims > 0 || snap.nonTerminal() > r.quotaHeldMax {
		return false
	}
	left, err := r.leg.Observer.NonTerminalItems(ctx, r.quotaHeldMax+1)
	if err != nil {
		return false
	}
	for _, entry := range left {
		if !r.isQuotaHeldEntry(entry) {
			return false
		}
	}
	return true
}

// abort records the first violation and stops all work.
func (r *runner) abort(v *Violation) {
	if v == nil {
		return
	}
	r.abortMu.Lock()
	if r.violation == nil {
		r.violation = v
		r.opt.Logf("%s: ABORT %s: %s", r.leg.Name, v.Trigger, v.Detail)
	}
	r.abortMu.Unlock()
	r.cancel()
}

func (r *runner) aborted() bool {
	r.abortMu.Lock()
	defer r.abortMu.Unlock()
	return r.violation != nil
}

// reverse is spec §8 steps 1-3 and 5: workers are already stopped; wait
// for live claims to drain (bounded), delete by prefix child-first in
// batches, then rerun the residue queries.
func (r *runner) reverse(ctx context.Context, report *Report) error {
	bound := r.spec.LeaseTTL + 2*r.spec.ReapInterval + r.opt.RequeueDelay
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		snap, err := r.leg.Observer.Snapshot(ctx)
		if err == nil && snap.ActiveClaims == 0 {
			break
		}
		if r.leg.HarnessReaper {
			_, _ = r.leg.Backend.ReapExpiredClaims(ctx, time.Now().UTC(), r.opt.ReapLimit, r.opt.RequeueDelay)
		}
		time.Sleep(time.Second)
	}
	if r.baselineDirty {
		report.CleanupSkipped = "the baseline already held harness rows this run did not create; nothing was deleted"
	} else {
		deleted, err := r.leg.Observer.DeletePrefixed(ctx, r.spec.Seed, r.opt.DeleteBatch)
		report.Deleted = deleted
		if err != nil {
			return fmt.Errorf("prefix cleanup: %w", err)
		}
	}
	final, err := r.leg.Observer.Census(ctx)
	if err != nil {
		return fmt.Errorf("residue census: %w", err)
	}
	report.Final = final
	report.Residue.FirstZero = final.prefixedTotal()
	report.Residue.SecondZeroDelta = final.otherDelta(report.Baseline)
	report.Residue.ThirdZero = r.coreResidue(ctx)
	if report.Residue.FirstZero != 0 || len(report.Residue.SecondZeroDelta) != 0 ||
		(report.Residue.ThirdZero.Checked && report.Residue.ThirdZero.Rows != 0) {
		v := violation(TriggerResidue, "residue after cleanup: synthetic=%d non-synthetic deltas=%v core=%+v",
			report.Residue.FirstZero, report.Residue.SecondZeroDelta, report.Residue.ThirdZero)
		v.Source = SourceResidue
		r.abort(v)
	}
	return nil
}

func (r *runner) coreResidue(ctx context.Context) CoreResidue {
	if r.opt.CoreResidue == nil {
		if r.spec.Target == TargetLocal {
			return CoreResidue{Status: NotApplicable, Detail: "local target has no core database"}
		}
		return CoreResidue{Status: "NOT_CHECKED", Detail: "no core read-only DSN supplied"}
	}
	n, err := r.opt.CoreResidue(ctx)
	if err != nil {
		return CoreResidue{Status: "ERROR", Detail: err.Error()}
	}
	return CoreResidue{Status: "CHECKED", Checked: true, Rows: n}
}
