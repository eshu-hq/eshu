// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests pin the worst cases of the #11233 shared depth read: a fall
// just after each read that saw the mark, the backoff reset on the first
// admit, and a read that a newer one overtook.

// holderBackoff returns the holder's current read interval while deferring.
func holderBackoff(h *admissionDepthHolder) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.backoff
}

// deferringReadSchedule runs one enqueuer at the mark for span and returns
// the virtual offset of every queue-depth read it issued. It is the real
// schedule of the production loop, so the worst-case falls below come from
// the code under test, not from a copy of its arithmetic.
func deferringReadSchedule(t *testing.T, span time.Duration) []time.Duration {
	t.Helper()
	clock := newVirtualClock()
	var mu sync.Mutex
	var reads []time.Duration
	reader := newClockedDepthReader(clock, func(elapsed time.Duration) int64 {
		mu.Lock()
		reads = append(reads, elapsed)
		mu.Unlock()
		return sharedDepthMark
	})
	writer, _ := newSharedDepthWriter(reader, clock, sharedDepthConfig(span))
	runEnqueuers(t, writer, clock, 1)
	mu.Lock()
	defer mu.Unlock()
	return append([]time.Duration(nil), reads...)
}

// TestReducerAdmissionSharedDepthAdmitsWithinBoundAfterEachRead proves the
// admit bound at its worst case: depth falls just after a read that saw the
// mark, so only the next read can see it. That read must come within the
// backoff cap of the fall. A wake that lands on the window boundary takes
// the held value and loses one more poll interval per step, which breaks the
// bound once the backoff reaches the cap.
func TestReducerAdmissionSharedDepthAdmitsWithinBoundAfterEachRead(t *testing.T) {
	t.Parallel()

	schedule := deferringReadSchedule(t, 90*time.Second)
	if len(schedule) < 8 {
		t.Fatalf("deferring read schedule = %v, want at least 8 reads over 90 s", schedule)
	}
	for _, readAt := range schedule {
		fall := readAt + time.Millisecond
		t.Run(fmt.Sprintf("fall %s", fall), func(t *testing.T) {
			t.Parallel()
			clock := newVirtualClock()
			reader := newClockedDepthReader(clock, func(elapsed time.Duration) int64 {
				if elapsed < fall {
					return sharedDepthMark
				}
				return sharedDepthMark - 1
			})
			writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))

			o := runEnqueuers(t, writer, clock, 1)[0]
			if o.err != nil {
				t.Fatalf("Enqueue() error = %v, want nil", o.err)
			}
			if bound := fall + reducerAdmissionBackoffCap; o.elapsed > bound {
				t.Fatalf("admitted after %s, want within %s (fall %s just after the read at %s, plus the %s backoff cap)",
					o.elapsed, bound, fall, readAt, reducerAdmissionBackoffCap)
			}
			if got := inner.calls.Load(); got != 1 {
				t.Fatalf("inner enqueue count = %d, want 1", got)
			}
		})
	}
}

// TestReducerAdmissionSharedDepthBackoffResetsOnFirstAdmit proves the first
// admit after a deferral resets the backoff, so the next decision past the
// max age A reads fresh instead of trusting a depth up to the old backoff
// old.
func TestReducerAdmissionSharedDepthBackoffResetsOnFirstAdmit(t *testing.T) {
	t.Parallel()

	const fall = 3500 * time.Millisecond
	clock := newVirtualClock()
	reader := newClockedDepthReader(clock, func(elapsed time.Duration) int64 {
		if elapsed < fall {
			return sharedDepthMark
		}
		return sharedDepthMark - 1
	})
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(5*time.Minute))

	o := runEnqueuers(t, writer, clock, 1)[0]
	if o.err != nil {
		t.Fatalf("Enqueue() error = %v, want nil", o.err)
	}
	if o.elapsed < fall {
		t.Fatalf("admitted after %s, before depth fell below the mark at %s", o.elapsed, fall)
	}
	if got := holderBackoff(writer.holder); got != 0 {
		t.Fatalf("backoff after the first admit = %s, want 0 (reset)", got)
	}
	readsAtAdmit := reader.queueReads.Load()

	clock.Advance(time.Second + time.Nanosecond)
	if _, err := writer.Enqueue(context.Background(), oneIntent); err != nil {
		t.Fatalf("Enqueue() past the max age error = %v, want nil", err)
	}
	if got := reader.queueReads.Load(); got != readsAtAdmit+1 {
		t.Fatalf("queue depth reads = %d past the max age after an admit, want %d (a fresh read; the held depth is older than A)", got, readsAtAdmit+1)
	}
	if got := inner.calls.Load(); got != 2 {
		t.Fatalf("inner enqueue count = %d, want 2", got)
	}
}

// TestReducerAdmissionSharedReadOvertakenReadDoesNotReplaceNewer proves the
// overtake guard. A read whose last waiter left is cancelled, but Postgres
// cancellation is not instant, so its statement can still return a value
// after a newer read started and finished. The older value must not replace
// the newer held one.
func TestReducerAdmissionSharedReadOvertakenReadDoesNotReplaceNewer(t *testing.T) {
	t.Parallel()

	type pendingRead struct {
		release chan int64
	}
	started := make(chan pendingRead, 2)
	shared := &admissionSharedRead[int64]{
		seq: &atomic.Uint64{},
		// The read ignores its context, like a statement that finishes
		// before the cancel reaches the server.
		read: func(context.Context) (int64, error) {
			p := pendingRead{release: make(chan int64)}
			started <- p
			return <-p.release, nil
		},
	}
	t0 := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	const maxAge = time.Second

	oldCtx, cancelOld := context.WithCancel(context.Background())
	oldDone := make(chan error, 1)
	go func() {
		_, _, err := shared.get(oldCtx, t0, maxAge)
		oldDone <- err
	}()
	oldRead := <-started
	cancelOld()
	if err := <-oldDone; err == nil {
		t.Fatal("cancelled waiter error = nil, want the context error")
	}
	if got := shared.waiting(); got != 0 {
		t.Fatalf("waiters after the last waiter left = %d, want 0 (read forgotten)", got)
	}

	t1 := t0.Add(500 * time.Millisecond)
	newDone := make(chan admissionHeldValue[int64], 1)
	go func() {
		held, _, err := shared.get(context.Background(), t1, maxAge)
		if err != nil {
			t.Errorf("newer read error = %v, want nil", err)
		}
		newDone <- held
	}()
	newRead := <-started
	newRead.release <- 2
	if held := <-newDone; held.value != 2 {
		t.Fatalf("newer read value = %d, want 2", held.value)
	}

	oldRead.release <- 1
	for deadline := time.Now().Add(2 * time.Second); shared.seq.Load() != 2; {
		if time.Now().After(deadline) {
			t.Fatal("overtaken read did not finish")
		}
		time.Sleep(100 * time.Microsecond)
	}

	held, outcome, err := shared.get(context.Background(), t1.Add(100*time.Millisecond), maxAge)
	if err != nil {
		t.Fatalf("get() after both reads error = %v, want nil", err)
	}
	if held.value != 2 || !held.readAt.Equal(t1) {
		t.Fatalf("held value = %d read at %s, want 2 read at %s (the overtaken read replaced the newer one)", held.value, held.readAt, t1)
	}
	if outcome != "held" {
		t.Fatalf("outcome = %q, want held", outcome)
	}
}
