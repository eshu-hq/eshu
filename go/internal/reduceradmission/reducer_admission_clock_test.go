// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// virtualClock is a test clock for the admission gate. Sleepers block until
// the driver advances virtual time to their wake time, so a span of minutes
// runs in milliseconds and every read and sleep lands on an exact virtual
// instant. The gate takes its clock and sleep by injection, so the
// production loop runs unchanged against it.
type virtualClock struct {
	mu       sync.Mutex
	now      time.Time
	sleepers []*virtualSleeper
	// sleeps records every requested sleep with the virtual instant it was
	// requested at, so tests can check no sleep overran the time left.
	sleeps []virtualSleep
}

type virtualSleeper struct {
	wake time.Time
	done chan struct{}
}

type virtualSleep struct {
	at time.Time
	d  time.Duration
}

func newVirtualClock() *virtualClock {
	return &virtualClock{now: time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)}
}

// Now returns the current virtual instant.
func (c *virtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Sleep blocks until virtual time reaches now+d or ctx ends.
func (c *virtualClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, virtualSleep{at: c.now, d: d})
	if d <= 0 {
		c.mu.Unlock()
		return ctx.Err()
	}
	sleeper := &virtualSleeper{wake: c.now.Add(d), done: make(chan struct{})}
	c.sleepers = append(c.sleepers, sleeper)
	c.mu.Unlock()
	select {
	case <-sleeper.done:
		return nil
	case <-ctx.Done():
		c.mu.Lock()
		for i, s := range c.sleepers {
			if s == sleeper {
				c.sleepers = append(c.sleepers[:i], c.sleepers[i+1:]...)
				break
			}
		}
		c.mu.Unlock()
		return ctx.Err()
	}
}

// Advance moves virtual time forward by d and wakes due sleepers.
func (c *virtualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.wakeDueLocked()
}

func (c *virtualClock) wakeDueLocked() {
	kept := c.sleepers[:0]
	for _, s := range c.sleepers {
		if !s.wake.After(c.now) {
			close(s.done)
			continue
		}
		kept = append(kept, s)
	}
	c.sleepers = kept
}

// recordedSleeps returns a copy of every requested sleep.
func (c *virtualClock) recordedSleeps() []virtualSleep {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]virtualSleep(nil), c.sleeps...)
}

// drive runs virtual time until alive reaches zero. Whenever every live
// goroutine is parked in Sleep it jumps to the earliest wake time. It fails
// the test if the goroutines neither finish nor park within a real-time
// bound, which is how a goroutine stuck outside Sleep shows up.
func (c *virtualClock) drive(t *testing.T, alive *atomic.Int64) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		if alive.Load() == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("virtual clock: %d goroutines neither finished nor parked in Sleep", alive.Load())
		}
		c.mu.Lock()
		parked := int64(len(c.sleepers))
		if parked > 0 && parked == alive.Load() {
			earliest := c.sleepers[0].wake
			for _, s := range c.sleepers[1:] {
				if s.wake.Before(earliest) {
					earliest = s.wake
				}
			}
			c.now = earliest
			c.wakeDueLocked()
		}
		c.mu.Unlock()
		time.Sleep(50 * time.Microsecond)
	}
}

// clockedDepthReader returns reducer depth as a function of virtual time and
// counts each signal's reads. The function form lets a test drop depth below
// the mark at an exact virtual instant.
type clockedDepthReader struct {
	clock             *virtualClock
	queueDepth        func(elapsed time.Duration) int64
	graphWriteTimeout func(elapsed time.Duration) int64
	start             time.Time
	queueReads        atomic.Int64
	pressureReads     atomic.Int64
}

func newClockedDepthReader(clock *virtualClock, queueDepth func(time.Duration) int64) *clockedDepthReader {
	return &clockedDepthReader{clock: clock, queueDepth: queueDepth, start: clock.Now()}
}

func (r *clockedDepthReader) QueueDepths(context.Context) (map[string]map[string]int64, error) {
	r.queueReads.Add(1)
	return map[string]map[string]int64{
		"reducer": {"pending": r.queueDepth(r.clock.Now().Sub(r.start))},
	}, nil
}

func (r *clockedDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	r.pressureReads.Add(1)
	if r.graphWriteTimeout == nil {
		return 0, nil
	}
	return r.graphWriteTimeout(r.clock.Now().Sub(r.start)), nil
}

// gatedDepthReader blocks each QueueDepths call until the test releases it,
// so a test can hold one read in flight while other callers join it.
type gatedDepthReader struct {
	calls atomic.Int64
	// finished counts returned calls, so a test can wait for a cancelled
	// read to return before it releases the next result.
	finished atomic.Int64
	started  chan context.Context
	release  chan gatedDepthResult
}

type gatedDepthResult struct {
	depth int64
	err   error
}

func newGatedDepthReader() *gatedDepthReader {
	return &gatedDepthReader{
		started: make(chan context.Context, 16),
		release: make(chan gatedDepthResult, 16),
	}
}

func (r *gatedDepthReader) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	r.calls.Add(1)
	defer r.finished.Add(1)
	r.started <- ctx
	select {
	case res := <-r.release:
		if res.err != nil {
			return nil, res.err
		}
		return map[string]map[string]int64{"reducer": {"pending": res.depth}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *gatedDepthReader) ReducerGraphWriteTimeoutDepth(context.Context) (int64, error) {
	return 0, nil
}

// expectedBackoffReads counts the reads one deferring process issues over
// span when the read interval starts at poll and doubles to backoffCap: one
// read at each step start inside the span.
func expectedBackoffReads(span, poll, backoffCap time.Duration) int {
	reads := 0
	var elapsed, step time.Duration
	for elapsed <= span {
		reads++
		if step == 0 {
			step = poll
		} else {
			step = min(2*step, backoffCap)
		}
		elapsed += step
	}
	return reads
}
