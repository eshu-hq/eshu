// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// idleWake lets the first claim after the whole process was idle wake the sleeping
// workers out of the idle poll backoff (#11186).
//
// The backoff (poll_backoff.go) keeps an idle projector cheap (#10869): a worker
// that polls an empty queue sleeps 1, 2, 4, then up to 5 seconds. Two rules keep that
// saving without leaving a burst to one awake worker. While any worker claimed within
// the last 5 s (processActivity), every worker polls at the base interval, so work that
// lands behind a busy worker is picked up by its siblings within a poll. And the first
// claim after the process was idle broadcasts once, so workers asleep in the long
// backoff poll at once. The wake is a hint: the queue stays the source of truth, a
// missed wake costs at most the backoff the worker already had, and no claim SQL
// changes.
type idleWake struct {
	mu        sync.Mutex
	ch        chan struct{}
	last      time.Time
	minPeriod time.Duration
	now       func() time.Time
}

// newIdleWake returns a broadcaster that sends at most one wake per minPeriod, so
// a burst of claims costs the siblings one extra poll each, not one per claim.
func newIdleWake(minPeriod time.Duration, now func() time.Time) *idleWake {
	return &idleWake{ch: make(chan struct{}), minPeriod: minPeriod, now: now}
}

// channel returns the channel the next broadcast closes. A worker reads it before
// its claim, so a broadcast that lands between the claim and the sleep still ends
// that sleep at once.
func (w *idleWake) channel() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.ch
}

// broadcast wakes every worker holding the current channel and reports whether it
// sent. A call inside minPeriod of the previous send is suppressed.
func (w *idleWake) broadcast() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	if !w.last.IsZero() && now.Sub(w.last) < w.minPeriod {
		return false
	}
	w.last = now
	close(w.ch)
	w.ch = make(chan struct{})
	return true
}

func (s Service) pollInterval() time.Duration {
	if s.PollInterval <= 0 {
		return defaultPollInterval
	}

	return s.PollInterval
}

func (s Service) wait(ctx context.Context, interval time.Duration) error {
	if s.Wait != nil {
		return s.Wait(ctx, interval)
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// waitIdle sleeps one idle backoff interval and returns early, with a nil error,
// when woken is closed: the process was idle and a worker claimed (#11186). Shutdown
// still returns the context error. The sleep runs on a child context the wake
// cancels, so an injected Wait that blocks on its context ends too.
func (s Service) waitIdle(ctx context.Context, interval time.Duration, woken <-chan struct{}) error {
	sleepCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-woken:
			cancel()
		case <-sleepCtx.Done():
		}
	}()
	err := s.wait(sleepCtx, interval)
	if err != nil && ctx.Err() == nil && sleepCtx.Err() != nil {
		return nil
	}
	return err
}

// recordIdleWake counts one wake broadcast and logs it at debug level.
func (s Service) recordIdleWake(ctx context.Context, workerID int) {
	if s.Instruments != nil && s.Instruments.ProjectorIdleWakes != nil {
		s.Instruments.ProjectorIdleWakes.Add(ctx, 1)
	}
	if s.Logger != nil {
		s.Logger.DebugContext(ctx, "projector worker claimed after the process was idle and woke its siblings",
			slog.Int("worker_id", workerID))
	}
}

// processActivity records the last claim any worker of this process made, so an
// empty-poll wait can tell a busy process from an idle one (#11186).
//
// The idle backoff exists to make an idle projector cheap (#10869). It must not
// apply to a worker whose siblings are claiming: while any worker has claimed within
// the last maxProjectorPollInterval the pool is active, every worker keeps polling at
// the base interval, and only a process with no claim for that long runs the 1, 2, 4,
// 5 s sequence. The timestamp is one atomic word shared by all workers.
type processActivity struct {
	lastClaim atomic.Int64 // unix nanoseconds of the last claim; 0 = none yet
	now       func() time.Time
}

func newProcessActivity(now func() time.Time) *processActivity {
	return &processActivity{now: now}
}

// noteClaim records a claim and reports whether the process had been idle: no claim
// before it, or none within the last maxProjectorPollInterval.
func (a *processActivity) noteClaim() (wasIdle bool) {
	now := a.now().UnixNano()
	prev := a.lastClaim.Swap(now)
	return prev == 0 || time.Duration(now-prev) >= maxProjectorPollInterval
}

// active reports whether any worker claimed within the last maxProjectorPollInterval.
func (a *processActivity) active() bool {
	last := a.lastClaim.Load()
	return last != 0 && time.Duration(a.now().UnixNano()-last) < maxProjectorPollInterval
}

// now returns the service clock.
func (s Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
