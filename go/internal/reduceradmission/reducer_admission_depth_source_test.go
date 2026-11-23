// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// These tests pin the #11233 shared depth read on the per-source leg
// (#10882): enqueueBySource takes both global signals from the holder,
// advances the backoff on a global deferral, sleeps until the held depth
// leaves the window (never past the max wait), and resets the backoff on a
// round that passes the global gates, even when the source latch then defers
// the group.

// runSourceEnqueuers starts k concurrent Enqueue calls of intents at the
// current virtual instant and drives virtual time until all return.
func runSourceEnqueuers(t *testing.T, writer reducerAdmissionWriter, clock *virtualClock, k int, intents []runtime.ReducerIntent) []enqueueOutcome {
	t.Helper()
	start := clock.Now()
	outcomes := make([]enqueueOutcome, k)
	var alive atomic.Int64
	alive.Store(int64(k))
	var wg sync.WaitGroup
	for i := range k {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer alive.Add(-1)
			_, err := writer.Enqueue(context.Background(), intents)
			outcomes[i] = enqueueOutcome{err: err, elapsed: clock.Now().Sub(start)}
		}()
	}
	clock.drive(t, &alive)
	wg.Wait()
	return outcomes
}

// recordingDepthReader returns queueDepth as a function of virtual time and
// records the virtual offset of every queue-depth read.
func recordingDepthReader(clock *virtualClock, queueDepth func(time.Duration) int64) (*clockedDepthReader, func() []time.Duration) {
	var mu sync.Mutex
	var reads []time.Duration
	reader := newClockedDepthReader(clock, func(elapsed time.Duration) int64 {
		mu.Lock()
		reads = append(reads, elapsed)
		mu.Unlock()
		return queueDepth(elapsed)
	})
	return reader, func() []time.Duration {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(reads)
	}
}

// TestReducerAdmissionSharedDepthSourceLegFollowsTheBackoffSchedule proves
// the source leg reads the queue on the holder's schedule: eight waiters
// whose batches carry a source, deferring at the mark for a 60 s max wait,
// read on exactly the one-waiter backoff schedule and time out at exactly
// the max wait. Without the holder's backoff the leg polls every second.
func TestReducerAdmissionSharedDepthSourceLegFollowsTheBackoffSchedule(t *testing.T) {
	t.Parallel()

	const k = 8
	const maxWait = 60 * time.Second
	clock := newVirtualClock()
	reader, reads := recordingDepthReader(clock, func(time.Duration) int64 { return sharedDepthMark })
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(maxWait))
	writer.sourceChecker = &pacedChecker{}
	writer.sourceConfig = admissionSourceTestConfig()

	for i, o := range runSourceEnqueuers(t, writer, clock, k, gitIntent()) {
		if !errors.Is(o.err, ErrReducerAdmissionTimeout) || o.elapsed != maxWait {
			t.Fatalf("enqueuer %d = (%v after %s), want %v after exactly %s", i, o.err, o.elapsed, ErrReducerAdmissionTimeout, maxWait)
		}
	}
	want := []time.Duration{
		at(0, 0, 0), at(1, 0, 1), at(3, 0, 2), at(7, 0, 3), at(15, 0, 4),
		at(25, 0, 5), at(35, 0, 6), at(45, 0, 7), at(55, 0, 8),
	}
	if got := reads(); !slices.Equal(got, want) {
		t.Fatalf("queue depth reads on the source leg at\n  %v\nwant exactly\n  %v", got, want)
	}
	if got := inner.calls.Load(); got != 0 {
		t.Fatalf("inner enqueue count = %d, want 0 while depth holds at the mark", got)
	}
}

// TestReducerAdmissionSharedDepthSourceBehindRoundResetsTheBackoff proves a
// round whose global gates pass resets the backoff even when the source
// latch defers the group: the paced rounds that follow read the queue fresh
// once the held depth is older than the poll interval. Depth falls below the
// mark at 3.5 s; the reads at 0, 1 and 3 s defer (backoff 1, 2, 4 s), the read
// at 7 s passes, and the latch defers two rounds 2 s apart. A backoff left at
// 4 s would serve the 7 s depth to both paced rounds and skip their reads.
func TestReducerAdmissionSharedDepthSourceBehindRoundResetsTheBackoff(t *testing.T) {
	t.Parallel()

	const fall = 3500 * time.Millisecond
	clock := newVirtualClock()
	reader, reads := recordingDepthReader(clock, func(elapsed time.Duration) int64 {
		if elapsed < fall {
			return sharedDepthMark
		}
		return sharedDepthMark - 1
	})
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))
	checker := &pacedChecker{retryAfter: 2 * time.Second, slowedRounds: 2}
	writer.sourceChecker = checker
	writer.sourceConfig = admissionSourceTestConfig()

	o := runSourceEnqueuers(t, writer, clock, 1, gitIntent())[0]
	if o.err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", o.err)
	}
	want := []time.Duration{at(0, 0, 0), at(1, 0, 1), at(3, 0, 2), at(7, 0, 3), at(9, 0, 3), at(11, 0, 3)}
	if got := reads(); !slices.Equal(got, want) {
		t.Fatalf("queue depth reads at\n  %v\nwant exactly\n  %v", got, want)
	}
	if o.elapsed != at(11, 0, 3) {
		t.Fatalf("admitted after %s, want %s (the third consult clears the latch)", o.elapsed, at(11, 0, 3))
	}
	if got := holderBackoff(writer.holder); got != 0 {
		t.Fatalf("backoff after the admit = %s, want 0", got)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", got)
	}
}
