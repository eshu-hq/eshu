// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// idleWake lets a worker that claims work after an idle spell wake its sibling
// workers out of the idle poll backoff (#11186).
//
// The backoff (poll_backoff.go) keeps an idle projector cheap: a worker that
// polls an empty queue sleeps 1, 2, 4, then up to 5 seconds. The cost is a
// burst: the one worker that is awake serves the whole queue while the others
// sleep out their timers, so work that arrives behind a paced re-claim wave waits
// for that worker. A worker that claims after an idle spell proves work is
// arriving, so it broadcasts once; sleeping siblings poll at once, and those that
// find nothing go back to their backoff. The wake is a hint: the queue stays the
// source of truth, a missed wake costs at most the backoff the worker already had,
// and no claim SQL changes.
type idleWake struct {
	mu        sync.Mutex
	ch        chan struct{}
	last      time.Time
	minPeriod time.Duration
	now       func() time.Time
}

// newIdleWake returns a broadcaster that sends at most one wake per minPeriod, so
// a burst of claims after idle spells costs the siblings one extra poll each, not
// one per claim.
func newIdleWake(minPeriod time.Duration) *idleWake {
	return &idleWake{ch: make(chan struct{}), minPeriod: minPeriod, now: time.Now}
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
// when woken is closed: a sibling claimed after an idle spell (#11186). Shutdown
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
		s.Logger.DebugContext(ctx, "projector worker claimed after an idle spell and woke its siblings",
			slog.Int("worker_id", workerID))
	}
}
