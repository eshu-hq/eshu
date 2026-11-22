// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// These tests pin the #11233 contract of the shared queue-depth read: one
// process-wide read of bounded age, single-flight across concurrent callers,
// and a process-wide backoff while the gate defers. The statement and the
// meaning of queue depth do not change; only how often the gate reads it.

const sharedDepthMark int64 = 10_000

func sharedDepthConfig(maxWait time.Duration) reducerAdmissionConfig {
	return reducerAdmissionConfig{
		HighWaterMark: sharedDepthMark,
		PollInterval:  time.Second,
		MaxWait:       maxWait,
	}
}

// concurrentIntentWriter is a race-safe inner writer that counts enqueues.
type concurrentIntentWriter struct {
	calls atomic.Int64
}

func (w *concurrentIntentWriter) Enqueue(_ context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error) {
	w.calls.Add(1)
	return runtime.IntentResult{Count: len(intents)}, nil
}

func newSharedDepthWriter(reader reducerAdmissionDepthReader, clock *virtualClock, config reducerAdmissionConfig) (reducerAdmissionWriter, *concurrentIntentWriter) {
	inner := &concurrentIntentWriter{}
	return reducerAdmissionWriter{
		inner:       inner,
		depthReader: reader,
		config:      config,
		sleep:       clock.Sleep,
		now:         clock.Now,
		deferral:    newAdmissionDeferralState(),
		holder:      newAdmissionDepthHolder(reader, config, clock.Now, nil),
	}, inner
}

var oneIntent = []runtime.ReducerIntent{{Domain: reducer.DomainWorkloadIdentity}}

type enqueueOutcome struct {
	err     error
	elapsed time.Duration
}

// runEnqueuers starts k concurrent Enqueue calls at the current virtual
// instant, drives virtual time until all return, and reports each outcome.
func runEnqueuers(t *testing.T, writer reducerAdmissionWriter, clock *virtualClock, k int) []enqueueOutcome {
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
			_, err := writer.Enqueue(context.Background(), oneIntent)
			outcomes[i] = enqueueOutcome{err: err, elapsed: clock.Now().Sub(start)}
		}()
	}
	clock.drive(t, &alive)
	wg.Wait()
	return outcomes
}

// TestReducerAdmissionSharedDepthDeferringEnqueuersReadOncePerBackoffStep
// proves D1 and D2 together: K enqueuers deferring at the mark for a whole
// max wait issue at most one read per backoff step for the process. Before
// #11233 each of the K waiters read every poll interval (K reads per second).
func TestReducerAdmissionSharedDepthDeferringEnqueuersReadOncePerBackoffStep(t *testing.T) {
	t.Parallel()

	const k = 8
	const maxWait = 60 * time.Second
	clock := newVirtualClock()
	reader := newClockedDepthReader(clock, func(time.Duration) int64 { return sharedDepthMark })
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(maxWait))

	outcomes := runEnqueuers(t, writer, clock, k)

	for i, o := range outcomes {
		if !errors.Is(o.err, ErrReducerAdmissionTimeout) {
			t.Fatalf("enqueuer %d error = %v, want %v", i, o.err, ErrReducerAdmissionTimeout)
		}
		if o.elapsed != maxWait {
			t.Fatalf("enqueuer %d returned after %s, want exactly the %s max wait", i, o.elapsed, maxWait)
		}
	}
	want := expectedBackoffReads(maxWait, time.Second, reducerAdmissionBackoffCap)
	if got := reader.queueReads.Load(); got > int64(want) {
		t.Fatalf("queue depth reads = %d over %s for %d deferring enqueuers, want at most %d (one per backoff step)", got, maxWait, k, want)
	}
	if got := inner.calls.Load(); got != 0 {
		t.Fatalf("inner enqueue count = %d, want 0 while depth holds at the mark", got)
	}
}

// TestReducerAdmissionSharedDepthAdmittedEnqueuesWithinMaxAgeReadOnce proves
// D1 on the admit side: N single-intent enqueues inside one max age share one
// read, and the first decision past the max age reads again.
func TestReducerAdmissionSharedDepthAdmittedEnqueuesWithinMaxAgeReadOnce(t *testing.T) {
	t.Parallel()

	const n = 16
	clock := newVirtualClock()
	reader := newClockedDepthReader(clock, func(time.Duration) int64 { return 5 })
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))

	for i, o := range runEnqueuers(t, writer, clock, n) {
		if o.err != nil {
			t.Fatalf("enqueuer %d error = %v, want nil", i, o.err)
		}
	}
	if got := reader.queueReads.Load(); got != 1 {
		t.Fatalf("queue depth reads = %d for %d admitted enqueues within the max age, want 1", got, n)
	}

	clock.Advance(time.Second)
	if _, err := writer.Enqueue(context.Background(), oneIntent); err != nil {
		t.Fatalf("Enqueue() at exactly the max age error = %v, want nil", err)
	}
	if got := reader.queueReads.Load(); got != 1 {
		t.Fatalf("queue depth reads = %d at an age equal to the max age, want 1 (held)", got)
	}

	clock.Advance(time.Nanosecond)
	if _, err := writer.Enqueue(context.Background(), oneIntent); err != nil {
		t.Fatalf("Enqueue() past the max age error = %v, want nil", err)
	}
	if got := reader.queueReads.Load(); got != 2 {
		t.Fatalf("queue depth reads = %d past the max age, want 2 (fresh read)", got)
	}
	if got := inner.calls.Load(); got != n+2 {
		t.Fatalf("inner enqueue count = %d, want %d", got, n+2)
	}
}

// TestReducerAdmissionSharedDepthHoldsGraphWritePressureSignal proves the
// graph-write-timeout depth goes through the same holder: admitted enqueues
// within one max age read each signal once.
func TestReducerAdmissionSharedDepthHoldsGraphWritePressureSignal(t *testing.T) {
	t.Parallel()

	clock := newVirtualClock()
	reader := newClockedDepthReader(clock, func(time.Duration) int64 { return 5 })
	reader.graphWriteTimeout = func(time.Duration) int64 { return 1 }
	config := sharedDepthConfig(5 * time.Minute)
	config.RetryingHighWaterMark = 500
	config.RetryingLowWaterMark = 100
	writer, _ := newSharedDepthWriter(reader, clock, config)

	for range 8 {
		if _, err := writer.Enqueue(context.Background(), oneIntent); err != nil {
			t.Fatalf("Enqueue() error = %v, want nil", err)
		}
	}
	if got := reader.pressureReads.Load(); got != 1 {
		t.Fatalf("graph-write-timeout depth reads = %d, want 1 within the max age", got)
	}
	if got := reader.queueReads.Load(); got != 1 {
		t.Fatalf("queue depth reads = %d, want 1 within the max age", got)
	}
}

// TestReducerAdmissionSharedDepthDefersAtMarkAndAdmitsBelowWithinBound
// proves the restated contract: a held depth at the mark defers, and once
// depth falls below the mark a waiter is admitted within the backoff cap
// plus one read (reads are instant here, so within the cap).
func TestReducerAdmissionSharedDepthDefersAtMarkAndAdmitsBelowWithinBound(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		fall time.Duration
	}{
		{name: "below at first read", fall: 0},
		{name: "falls inside the first step", fall: 500 * time.Millisecond},
		{name: "falls mid backoff", fall: 12500 * time.Millisecond},
		{name: "falls just after a capped read", fall: 15*time.Second + time.Millisecond},
		{name: "falls late", fall: 40 * time.Second},
		// 30.001 s and 41.001 s each follow a read that saw the mark under the
		// inclusive-window schedule that lost one poll interval per step.
		{name: "falls just after the 30 s read", fall: 30*time.Second + time.Millisecond},
		{name: "falls just after the 41 s read", fall: 41*time.Second + time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := newVirtualClock()
			reader := newClockedDepthReader(clock, func(elapsed time.Duration) int64 {
				if elapsed < tc.fall {
					return sharedDepthMark
				}
				return sharedDepthMark - 1
			})
			writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))

			o := runEnqueuers(t, writer, clock, 1)[0]
			if o.err != nil {
				t.Fatalf("Enqueue() error = %v, want nil", o.err)
			}
			if o.elapsed < tc.fall {
				t.Fatalf("admitted after %s, before depth fell below the mark at %s", o.elapsed, tc.fall)
			}
			if bound := tc.fall + reducerAdmissionBackoffCap; o.elapsed > bound {
				t.Fatalf("admitted after %s, want within %s (fall %s plus the %s backoff cap)", o.elapsed, bound, tc.fall, reducerAdmissionBackoffCap)
			}
			if want := expectedBackoffReads(o.elapsed, time.Second, reducerAdmissionBackoffCap); reader.queueReads.Load() > int64(want) {
				t.Fatalf("queue depth reads = %d over %s, want at most %d (one per backoff step)", reader.queueReads.Load(), o.elapsed, want)
			}
			if got := inner.calls.Load(); got != 1 {
				t.Fatalf("inner enqueue count = %d, want 1", got)
			}
		})
	}
}

// TestReducerAdmissionSharedDepthMaxWaitFiresOnTimeUnderBackoff proves the
// max wait still ends a wait on time: no sleep runs past the time left, so
// the timeout fires at the max wait even when the backoff step is longer
// than the remaining wait.
func TestReducerAdmissionSharedDepthMaxWaitFiresOnTimeUnderBackoff(t *testing.T) {
	t.Parallel()

	const maxWait = 20 * time.Second
	clock := newVirtualClock()
	start := clock.Now()
	reader := newClockedDepthReader(clock, func(time.Duration) int64 { return sharedDepthMark })
	writer, _ := newSharedDepthWriter(reader, clock, sharedDepthConfig(maxWait))

	o := runEnqueuers(t, writer, clock, 1)[0]
	if !errors.Is(o.err, ErrReducerAdmissionTimeout) {
		t.Fatalf("Enqueue() error = %v, want %v", o.err, ErrReducerAdmissionTimeout)
	}
	if o.elapsed != maxWait {
		t.Fatalf("timeout fired after %s, want exactly %s", o.elapsed, maxWait)
	}
	for _, s := range clock.recordedSleeps() {
		if end := s.at.Add(s.d); end.After(start.Add(maxWait)) {
			t.Fatalf("sleep of %s requested at %s ends %s past the max wait", s.d, s.at.Sub(start), end.Sub(start.Add(maxWait)))
		}
	}
	if want := expectedBackoffReads(maxWait, time.Second, reducerAdmissionBackoffCap); reader.queueReads.Load() > int64(want) {
		t.Fatalf("queue depth reads = %d over %s, want at most %d", reader.queueReads.Load(), maxWait, want)
	}
}

// waitForSharedReadWaiters polls until n callers wait on the in-flight
// queue-depth read, or fails after a real-time bound.
func waitForSharedReadWaiters(t *testing.T, holder *admissionDepthHolder, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for holder.queueDepth.waiting() != n {
		if time.Now().After(deadline) {
			t.Fatalf("shared read waiters = %d, want %d (callers did not join one in-flight read)", holder.queueDepth.waiting(), n)
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// TestReducerAdmissionSharedDepthReadErrorReturnedAndNotHeld proves a failed
// read reaches every caller waiting on it, is never held, and the next
// decision reads again.
func TestReducerAdmissionSharedDepthReadErrorReturnedAndNotHeld(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("queue depth read failed")
	clock := newVirtualClock()
	reader := newGatedDepthReader()
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))

	const callers = 4
	errs := make(chan error, callers)
	go func() {
		_, err := writer.Enqueue(context.Background(), oneIntent)
		errs <- err
	}()
	<-reader.started
	for range callers - 1 {
		go func() {
			_, err := writer.Enqueue(context.Background(), oneIntent)
			errs <- err
		}()
	}
	waitForSharedReadWaiters(t, writer.holder, callers)
	reader.release <- gatedDepthResult{err: errBoom}
	for range callers {
		if err := <-errs; !errors.Is(err, errBoom) {
			t.Fatalf("Enqueue() error = %v, want the read error %v", err, errBoom)
		}
	}
	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("queue depth reads = %d for %d concurrent callers, want 1 shared read", got, callers)
	}

	reader.release <- gatedDepthResult{depth: 5}
	if _, err := writer.Enqueue(context.Background(), oneIntent); err != nil {
		t.Fatalf("Enqueue() after a failed read error = %v, want nil", err)
	}
	if got := reader.calls.Load(); got != 2 {
		t.Fatalf("queue depth reads = %d, want 2 (the failed read was not held)", got)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", got)
	}
}

// TestReducerAdmissionSharedDepthCancelledWaiterLeavesOthersIntact proves one
// caller's cancellation neither cancels the shared read nor fails the callers
// still waiting on it.
func TestReducerAdmissionSharedDepthCancelledWaiterLeavesOthersIntact(t *testing.T) {
	t.Parallel()

	clock := newVirtualClock()
	reader := newGatedDepthReader()
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := writer.Enqueue(leaderCtx, oneIntent)
		leaderErr <- err
	}()
	readCtx := <-reader.started

	const followers = 3
	errs := make(chan error, followers)
	for range followers {
		go func() {
			_, err := writer.Enqueue(context.Background(), oneIntent)
			errs <- err
		}()
	}
	waitForSharedReadWaiters(t, writer.holder, followers+1)

	cancelLeader()
	select {
	case err := <-leaderErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled leader error = %v, want %v", err, context.Canceled)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled leader did not return while the shared read was in flight")
	}
	if err := readCtx.Err(); err != nil {
		t.Fatalf("shared read context error = %v after one waiter cancelled, want nil", err)
	}
	waitForSharedReadWaiters(t, writer.holder, followers)

	reader.release <- gatedDepthResult{depth: 5}
	for range followers {
		if err := <-errs; err != nil {
			t.Fatalf("follower Enqueue() error = %v, want nil", err)
		}
	}
	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("queue depth reads = %d, want 1 shared read", got)
	}
	if got := inner.calls.Load(); got != followers {
		t.Fatalf("inner enqueue count = %d, want %d", got, followers)
	}
}

// TestReducerAdmissionSharedDepthLastWaiterCancelStopsRead proves a read no
// caller waits on any more is cancelled, and the next caller starts a fresh
// read instead of joining the cancelled one.
func TestReducerAdmissionSharedDepthLastWaiterCancelStopsRead(t *testing.T) {
	t.Parallel()

	clock := newVirtualClock()
	reader := newGatedDepthReader()
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := writer.Enqueue(ctx, oneIntent)
		done <- err
	}()
	readCtx := <-reader.started
	waitForSharedReadWaiters(t, writer.holder, 1)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Enqueue() error = %v, want %v", err, context.Canceled)
	}
	select {
	case <-readCtx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("shared read was not cancelled after its last waiter left")
	}
	for deadline := time.Now().Add(2 * time.Second); reader.finished.Load() != 1; {
		if time.Now().After(deadline) {
			t.Fatal("cancelled shared read did not return")
		}
		time.Sleep(100 * time.Microsecond)
	}

	reader.release <- gatedDepthResult{depth: 5}
	if _, err := writer.Enqueue(context.Background(), oneIntent); err != nil {
		t.Fatalf("Enqueue() after the cancelled read error = %v, want nil", err)
	}
	if got := reader.calls.Load(); got != 2 {
		t.Fatalf("queue depth reads = %d, want 2 (a fresh read after the cancelled one)", got)
	}
	if got := inner.calls.Load(); got != 1 {
		t.Fatalf("inner enqueue count = %d, want 1", got)
	}
}

// TestReducerIntentWriterWithAdmissionSharesOneDepthHolder pins the wiring:
// the production constructor gives the gate one shared holder, so every
// worker's decisions go through it.
func TestReducerIntentWriterWithAdmissionSharesOneDepthHolder(t *testing.T) {
	t.Parallel()

	writer, err := reducerIntentWriterWithAdmission(postgres.SQLDB{}, &recordingReducerIntentWriter{}, mapGetenv(nil), nil, nil)
	if err != nil {
		t.Fatalf("reducerIntentWriterWithAdmission() error = %v, want nil", err)
	}
	admission, ok := writer.(reducerAdmissionWriter)
	if !ok {
		t.Fatalf("writer type = %T, want reducerAdmissionWriter", writer)
	}
	if admission.holder == nil {
		t.Fatal("production admission writer has no shared depth holder; every decision would read queue depth")
	}
}
