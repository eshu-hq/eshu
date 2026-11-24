// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// simClock is a virtual clock for the idle backoff proofs: a worker's sleep blocks
// until the test advances time to its deadline, so 60 simulated seconds run in
// milliseconds and the poll counts are exact.
type simClock struct {
	mu        sync.Mutex
	now       time.Time
	waiters   []*simWaiter
	intervals []simWait
}

type simWaiter struct {
	at time.Time
	ch chan struct{}
}

type simWait struct {
	at       time.Duration // simulated time since the start when the sleep began
	interval time.Duration
}

var simStart = time.Date(2026, time.November, 24, 12, 0, 0, 0, time.UTC)

func newSimClock() *simClock { return &simClock{now: simStart} }

func (c *simClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *simClock) elapsed() time.Duration { return c.Now().Sub(simStart) }

// Sleep is the Service.Wait seam: it registers a waiter and blocks until the clock
// reaches its deadline or the context ends.
func (c *simClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	w := &simWaiter{at: c.now.Add(d), ch: make(chan struct{})}
	c.waiters = append(c.waiters, w)
	c.intervals = append(c.intervals, simWait{at: c.now.Sub(simStart), interval: d})
	c.mu.Unlock()
	defer c.drop(w)
	select {
	case <-w.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *simClock) drop(w *simWaiter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, other := range c.waiters {
		if other == w {
			c.waiters = append(c.waiters[:i], c.waiters[i+1:]...)
			return
		}
	}
}

func (c *simClock) blocked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waiters)
}

// advance moves time to the earliest deadline and releases every waiter due by then.
func (c *simClock) advance() {
	c.mu.Lock()
	defer c.mu.Unlock()
	var next *simWaiter
	for _, w := range c.waiters {
		if next == nil || w.at.Before(next.at) {
			next = w
		}
	}
	if next == nil {
		return
	}
	c.now = next.at
	remaining := c.waiters[:0]
	for _, w := range c.waiters {
		if !w.at.After(c.now) {
			close(w.ch)
		} else {
			remaining = append(remaining, w)
		}
	}
	c.waiters = remaining
}

// runFor drives the virtual clock until limit has elapsed: it waits for every worker to be
// asleep, then advances to the earliest deadline.
func (c *simClock) runFor(t *testing.T, workers int, limit time.Duration) {
	t.Helper()
	for c.elapsed() < limit {
		deadline := time.Now().Add(5 * time.Second)
		for c.blocked() != workers {
			if time.Now().After(deadline) {
				t.Fatalf("workers did not all go to sleep at %v (blocked=%d)", c.elapsed(), c.blocked())
			}
			time.Sleep(50 * time.Microsecond)
		}
		c.advance()
	}
}

// simSource hands out one item per `every` of simulated time (every = 0: never) and
// records the virtual time of every claim call.
type simSource struct {
	clock *simClock
	every time.Duration
	mu    sync.Mutex
	next  time.Duration
	calls []time.Duration
	n     atomic.Int64
}

func (s *simSource) Claim(context.Context) (ScopeGenerationWork, bool, error) {
	now := s.clock.elapsed()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, now)
	if s.every > 0 && now >= s.next {
		s.next = now + s.every
		return idleWakeWork(int(s.n.Add(1))), true, nil
	}
	return ScopeGenerationWork{}, false, nil
}

func (s *simSource) callsUpTo(limit time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, at := range s.calls {
		if at <= limit {
			n++
		}
	}
	return n
}

func simService(clock *simClock, source ProjectorWorkSource, workers int) Service {
	return Service{
		PollInterval:       time.Second,
		WorkSource:         source,
		FactStore:          &stubFactStore{facts: []facts.Envelope{{FactID: "f", FactKind: "source_node"}}},
		Runner:             &holdingRunner{release: closedChan()},
		WorkSink:           &stubProjectorWorkSink{},
		DeltaBaselineFence: &fakeDeltaBaselineFence{},
		WriteMarker:        &fakeWriteMarker{},
		Workers:            workers,
		Wait:               clock.Sleep,
		Now:                clock.Now,
	}
}

func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func runSim(t *testing.T, service Service, clock *simClock, workers int, limit time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- service.Run(ctx) }()
	clock.runFor(t, workers, limit)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

// TestActivePoolKeepsEveryWorkerPollingAtTheBaseInterval pins the #11186 backoff gate:
// one worker claims every base interval, so the pool is active, and no worker may sleep
// longer than the base interval. On main the three workers that find nothing back off
// to 2, 4, then 5 s, so a burst that lands behind the busy worker waits for it.
func TestActivePoolKeepsEveryWorkerPollingAtTheBaseInterval(t *testing.T) {
	t.Parallel()

	clock := newSimClock()
	source := &simSource{clock: clock, every: time.Second}
	service := simService(clock, source, 4)
	runSim(t, service, clock, 4, 30*time.Second)

	clock.mu.Lock()
	defer clock.mu.Unlock()
	for _, w := range clock.intervals {
		if w.at >= 2*time.Second && w.interval > service.pollInterval() {
			t.Fatalf("a worker slept %v at %v while a sibling was claiming every second, want at most %v",
				w.interval, w.at, service.pollInterval())
		}
	}
	if got := source.callsUpTo(30 * time.Second); got < 3*28 {
		t.Fatalf("claim polls in 30 simulated seconds = %d, want at least %d (three idle workers polling once a second)", got, 3*28)
	}
}

// TestIdleProcessPollsExactlyAsBefore pins that a process with no claim for the whole
// run keeps the 1, 2, 4, then 5 s sequence of #10869: over 60 simulated seconds the
// four workers poll exactly as many times as that sequence says.
func TestIdleProcessPollsExactlyAsBefore(t *testing.T) {
	t.Parallel()

	clock := newSimClock()
	source := &simSource{clock: clock}
	service := simService(clock, source, 4)
	runSim(t, service, clock, 4, 61*time.Second)

	perWorker := 0
	for at, n := time.Duration(0), 1; at <= 60*time.Second; n++ {
		perWorker++
		at += projectorPollBackoff(service.pollInterval(), n)
	}
	if want, got := 4*perWorker, source.callsUpTo(60*time.Second); got != want {
		t.Fatalf("claim polls in 60 idle simulated seconds = %d, want %d (the unchanged backoff sequence)", got, want)
	}
}

// TestBackoffResumesAfterTheProcessGoesQuiet pins the end of an active spell: once no
// worker has claimed for longer than the cap, the waits grow again to the cap, so the
// saving of #10869 returns.
func TestBackoffResumesAfterTheProcessGoesQuiet(t *testing.T) {
	t.Parallel()

	clock := newSimClock()
	source := &stoppingSource{simSource: simSource{clock: clock, every: time.Second}, stopAfter: 10 * time.Second}
	service := simService(clock, source, 4)
	runSim(t, service, clock, 4, 60*time.Second)

	clock.mu.Lock()
	defer clock.mu.Unlock()
	longest := time.Duration(0)
	for _, w := range clock.intervals {
		if w.at > 25*time.Second && w.interval > longest {
			longest = w.interval
		}
	}
	if longest != maxProjectorPollInterval {
		t.Fatalf("longest sleep after the process went quiet = %v, want the %v cap", longest, maxProjectorPollInterval)
	}
}

// stoppingSource stops handing out work after stopAfter of simulated time.
type stoppingSource struct {
	simSource
	stopAfter time.Duration
}

func (s *stoppingSource) Claim(ctx context.Context) (ScopeGenerationWork, bool, error) {
	if s.clock.elapsed() >= s.stopAfter {
		s.mu.Lock()
		s.calls = append(s.calls, s.clock.elapsed())
		s.mu.Unlock()
		return ScopeGenerationWork{}, false, nil
	}
	return s.simSource.Claim(ctx)
}
