// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// SampleRecord is one sampler tick kept in the report.
type SampleRecord struct {
	At           time.Time       `json:"at"`
	Items        map[string]int  `json:"items_by_status"`
	ActiveClaims int             `json:"active_claims"`
	Created      int64           `json:"created"`
	Headroom     *HeadroomSample `json:"headroom,omitempty"`
	QueryMS      float64         `json:"query_ms"`
}

// sampleLog keeps the first MaxSamples records plus aggregate counters.
type sampleLog struct {
	mu       sync.Mutex
	records  []SampleRecord
	count    int
	errors   []string
	queryMS  []time.Duration
	maxHead  HeadroomSample
	headSeen bool
}

func (l *sampleLog) add(rec SampleRecord, limit int, query time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count++
	l.queryMS = append(l.queryMS, query)
	if len(l.records) < limit {
		l.records = append(l.records, rec)
	}
	if h := rec.Headroom; h != nil {
		if !l.headSeen {
			l.maxHead, l.headSeen = *h, true
			return
		}
		l.maxHead.Connections = max(l.maxHead.Connections, h.Connections)
		l.maxHead.LockWaiters = max(l.maxHead.LockWaiters, h.LockWaiters)
		l.maxHead.MaxLockWaitMS = max(l.maxHead.MaxLockWaitMS, h.MaxLockWaitMS)
		l.maxHead.DeadlocksTotal = max(l.maxHead.DeadlocksTotal, h.DeadlocksTotal)
		l.maxHead.ReplicationLagSec = max(l.maxHead.ReplicationLagSec, h.ReplicationLagSec)
	}
}

func (l *sampleLog) fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.errors) < 50 {
		l.errors = append(l.errors, err.Error())
	}
}

// sample is the §6 per-second invariant sampler. Each tick checks the
// cheap invariants (dual live claim, zero loss, headroom, abort file); every
// CensusEvery ticks it diffs non-synthetic rows against the baseline (a
// non-synthetic id touched), and every FencingEvery ticks it runs the full
// fencing check. It reads through the observer's own connection pool.
func (r *runner) sample(baseline Census, deadlockBase float64, stop <-chan struct{}) {
	ticker := time.NewTicker(r.opt.SampleInterval)
	defer ticker.Stop()
	for tick := 1; ; tick++ {
		select {
		case <-stop:
			return
		case <-r.workCtx.Done():
			return
		case <-ticker.C:
		}
		if v := r.sampleOnce(tick, baseline, deadlockBase); v != nil {
			r.abort(v)
			return
		}
	}
}

func (r *runner) sampleOnce(tick int, baseline Census, deadlockBase float64) *Violation {
	if r.opt.AbortFile != "" {
		if body, err := os.ReadFile(r.opt.AbortFile); err == nil {
			return violation(TriggerExternal, "abort file %s: %s", r.opt.AbortFile, strings.TrimSpace(string(body)))
		}
	}
	ctx, cancel := context.WithTimeout(r.workCtx, sampleTimeout)
	defer cancel()
	t0 := time.Now()
	created := r.created.Load()
	snap, err := r.leg.Observer.Snapshot(ctx)
	if err != nil {
		r.samples.fail(err)
		return nil
	}
	rec := SampleRecord{At: t0.UTC(), Items: snap.ItemsByStatus, ActiveClaims: snap.ActiveClaims, Created: created}
	if len(snap.DualLive) > 0 {
		return violation(TriggerDualClaim, "sampled %d items with two live claims: %v", len(snap.DualLive), snap.DualLive)
	}
	if int64(snap.ItemTotal) < created {
		return violation(TriggerLostItem, "sampled %d synthetic items, %d created", snap.ItemTotal, created)
	}
	if snap.ItemTotal > r.maxItems {
		return violation(TriggerQueueDepth, "sampled %d synthetic items, cap %d", snap.ItemTotal, r.maxItems)
	}
	if tick%r.opt.FencingEvery == 0 {
		bad, err := r.leg.Observer.FencingViolations(ctx)
		if err != nil {
			r.samples.fail(err)
		} else if len(bad) > 0 {
			return violation(TriggerFencingRegression, "fencing check: %v", bad)
		}
		twice, err := r.leg.Observer.DoubleTerminal(ctx)
		if err != nil {
			r.samples.fail(err)
		} else if len(twice) > 0 {
			return violation(TriggerExactlyOnce, "items with two terminal claims: %v", twice)
		}
	}
	if tick%r.opt.CensusEvery == 0 {
		census, err := r.leg.Observer.Census(ctx)
		if err != nil {
			r.samples.fail(err)
		} else if delta := census.otherDelta(baseline); len(delta) > 0 {
			return violation(TriggerNonSynthetic, "non-synthetic rows changed: %v", describeDelta(delta, census, baseline))
		}
	}
	if r.leg.Headroom != nil {
		h, err := r.leg.Headroom.Sample(ctx)
		if err != nil {
			r.samples.fail(err)
		} else {
			rec.Headroom = &h
			if v := headroomCheck(h, r.spec.Headroom, r.spec.LockWaitBound, deadlockBase); v != nil {
				return v
			}
		}
	}
	if r.rec.deadlockCount() > 0 {
		return violation(TriggerDeadlock, "a call failed with SQLSTATE 40P01")
	}
	query := time.Since(t0)
	rec.QueryMS = ms(query)
	r.samples.add(rec, r.opt.MaxSamples, query)
	return nil
}

func describeDelta(keys []string, now, base Census) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s %d->%d", k, base.Other[k], now.Other[k]))
	}
	return out
}

// reapLoop is §5 step 4 on legs where the harness may reap: one full pass
// every reap_interval, each pass reaping until a call returns under limit.
// Every reaped claim must be synthetic; the pass time feeds the reclaim
// bound.
func (r *runner) reapLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(r.spec.ReapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-r.workCtx.Done():
			return
		case <-ticker.C:
		}
		pass := time.Now()
		for {
			ctx, cancel := context.WithTimeout(r.workCtx, r.opt.CallTimeout)
			t0 := time.Now()
			reaped, err := r.leg.Backend.ReapExpiredClaims(ctx, t0.UTC(), r.opt.ReapLimit, r.opt.RequeueDelay)
			cancel()
			r.rec.observe("reap", time.Since(t0), err)
			if err != nil {
				if isDeadlock(err) {
					r.abort(violation(TriggerDeadlock, "reap: %v", err))
					return
				}
				break
			}
			for _, c := range reaped {
				if !IsSynthetic(c.WorkItemID) || !IsSynthetic(c.ClaimID) {
					r.abort(violation(TriggerNonSynthetic, "reaper expired non-synthetic claim %q on %q", c.ClaimID, c.WorkItemID))
					return
				}
			}
			if len(reaped) < r.opt.ReapLimit {
				break
			}
		}
		if d := int64(time.Since(pass)); d > r.reapPassMax.Load() {
			r.reapPassMax.Store(d)
		}
	}
}

// advisoryTally records §5 step 6 rounds.
type advisoryTally struct {
	mu         sync.Mutex
	rounds     int
	cleanRound int
	duplicates []string
	errors     []string
}

// advisoryLoop is §5 step 6 (in-tree only): every AdvisoryInterval inside
// the window, AdvisoryGoroutines goroutines race CreateRunWithWorkItems
// IfNoOpenTargets for the same (kind, instance) and the same fresh target
// scopes. Exactly one may insert; the rest must see zero eligible targets.
func (r *runner) advisoryLoop() {
	for round := 0; ; round++ {
		select {
		case <-r.workCtx.Done():
			return
		case <-time.After(r.opt.AdvisoryInterval):
		}
		if !time.Now().Before(r.windowEnd) {
			return
		}
		r.advisoryRound(round)
	}
}

func (r *runner) advisoryRound(round int) {
	g := r.opt.AdvisoryGoroutines
	inserted := make([]int, g)
	errs := make([]error, g)
	start := make(chan struct{})
	var wg sync.WaitGroup
	now := time.Now().UTC()
	for i := range g {
		run := newRun(r.ids.advisoryRun(round, i), now)
		items := make([]workflow.WorkItem, 0, r.opt.AdvisoryItems)
		for n := range r.opt.AdvisoryItems {
			item := newItem(r.ids.advisoryItem(round, i, n), run.RunID,
				r.ids.advisoryScope(round, n), r.opt.CollectorKind, now)
			// The guard keys a target on (kind, instance, scope, tenant,
			// workspace, subject, policy, acceptance unit): every goroutine
			// must plan the same targets under its own run and item ids.
			item.AcceptanceUnitID = r.ids.advisoryTarget(round, n)
			items = append(items, item)
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			ctx, cancel := context.WithTimeout(r.workCtx, r.opt.CallTimeout)
			defer cancel()
			t0 := time.Now()
			admission, err := r.leg.Backend.AdmitGuarded(ctx, run, items)
			r.rec.observe("admit_guarded", time.Since(t0), err)
			inserted[i], errs[i] = admission.InsertedWorkItems, err
		}(i)
	}
	close(start)
	wg.Wait()
	if r.workCtx.Err() != nil {
		return
	}
	winners, total := 0, 0
	r.advisory.mu.Lock()
	defer r.advisory.mu.Unlock()
	r.advisory.rounds++
	for i := range g {
		if errs[i] != nil {
			if isDeadlock(errs[i]) {
				r.abort(violation(TriggerDeadlock, "advisory round %d: %v", round, errs[i]))
			}
			if r.workCtx.Err() == nil && len(r.advisory.errors) < 20 {
				r.advisory.errors = append(r.advisory.errors, errs[i].Error())
			}
			continue
		}
		if inserted[i] > 0 {
			winners++
		}
		total += inserted[i]
	}
	r.created.Add(int64(total))
	if winners == 1 && total == r.opt.AdvisoryItems {
		r.advisory.cleanRound++
		return
	}
	r.advisory.duplicates = append(r.advisory.duplicates,
		fmt.Sprintf("round %d: %d admitting goroutines, %d items inserted (want 1 and %d)", round, winners, total, r.opt.AdvisoryItems))
}
