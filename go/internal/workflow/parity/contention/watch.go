// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"os"
	"strings"
	"time"
)

// watcher checks the signals that must hold for the whole run, from before
// the isolation probe until the reversal has finished: the abort file, the
// pg_headroom ceilings and the deadlock counter, and SQLSTATE 40P01 on any
// call. It is independent of the database sampler, so a long seed, the
// statistics wait, and cleanup are all covered.
type watcher struct {
	r            *runner
	deadlockBase float64
	fails        failures
	stop         chan struct{}
	done         chan struct{}
}

// startWatcher takes the first reading synchronously, so a server already
// over a ceiling stops the run before any write, then keeps sampling every
// SampleInterval until stopWatcher.
func (r *runner) startWatcher(report *Report) *watcher {
	w := &watcher{
		r: r, deadlockBase: notAvailable, fails: failures{limit: r.opt.MaxSampleFailures},
		stop: make(chan struct{}), done: make(chan struct{}),
	}
	if r.leg.Headroom != nil {
		ctx, cancel := context.WithTimeout(r.workCtx, sampleTimeout)
		s, err := r.leg.Headroom.Sample(ctx)
		cancel()
		if err == nil && r.spec.Target == TargetLive {
			err = missingCeilings(s, r.spec.Headroom)
		}
		if err == nil {
			w.deadlockBase = s.DeadlocksTotal
			report.Headroom.Baseline = &s
		} else if r.spec.Target == TargetLive {
			// The live target needs one complete reading before the
			// isolation probe: otherwise the first writes run blind.
			r.abortFrom(SourceWatcher, violation(TriggerSamplerUnavailable,
				"no complete headroom reading before the first write: %v", err))
		}
	} else if r.spec.Target == TargetLive {
		r.abortFrom(SourceWatcher, violation(TriggerSamplerUnavailable, "the live target has no headroom source"))
	}
	w.tick()
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(r.opt.SampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				w.tick()
			}
		}
	}()
	return w
}

// stopWatcher ends the watch after the reversal.
func (w *watcher) stopWatcher() {
	close(w.stop)
	<-w.done
}

func (w *watcher) tick() {
	r := w.r
	if v := w.check(); v != nil {
		v.Source = SourceWatcher
		r.abort(v)
	}
}

func (w *watcher) check() *Violation {
	r := w.r
	if r.opt.AbortFile != "" {
		if body, err := os.ReadFile(r.opt.AbortFile); err == nil {
			return violation(TriggerExternal, "abort file %s: %s", r.opt.AbortFile, strings.TrimSpace(string(body)))
		}
	}
	if r.rec.deadlockCount() > 0 {
		return violation(TriggerDeadlock, "a call failed with SQLSTATE 40P01")
	}
	if r.leg.Headroom == nil {
		return nil
	}
	// The watcher outlives the work context (it covers cleanup), so its
	// reads take their own deadline.
	ctx, cancel := context.WithTimeout(context.Background(), sampleTimeout)
	defer cancel()
	h, err := r.leg.Headroom.Sample(ctx)
	if err == nil && r.spec.Target == TargetLive {
		// On the live target a reading without an enforced series (the
		// wrong port-forward still answers a valid Prometheus page) is a
		// failed read, not a pass.
		err = missingCeilings(h, r.spec.Headroom)
	}
	if v := w.fails.observe(&r.samples, err, "headroom watcher"); v != nil || err != nil {
		return v
	}
	r.samples.addHead(h)
	if w.deadlockBase == notAvailable {
		w.deadlockBase = h.DeadlocksTotal
	}
	return headroomCheck(h, r.spec.Headroom, r.spec.LockWaitBound, w.deadlockBase)
}
