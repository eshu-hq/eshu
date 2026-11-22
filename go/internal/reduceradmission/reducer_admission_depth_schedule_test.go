// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reduceradmission

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// These tests pin the exact deferring read schedule of the #11233 shared
// depth read: with K concurrent waiters, and with both gate signals on and
// reads that take virtual time. An at-most bound would pass a backoff that
// grows K times per read, so these compare the whole schedule.

// latencyDepthReader returns fixed depths after a virtual-clock delay per
// signal and records the virtual offset at which each read started. The delay
// models a statement that takes real time on the server, so the gate loop
// sees reads that end after they start.
type latencyDepthReader struct {
	clock           *virtualClock
	start           time.Time
	queueLatency    time.Duration
	pressureLatency time.Duration
	queueDepth      int64
	pressureDepth   int64

	mu            sync.Mutex
	queueReads    []time.Duration
	pressureReads []time.Duration
}

func (r *latencyDepthReader) QueueDepths(ctx context.Context) (map[string]map[string]int64, error) {
	r.mu.Lock()
	r.queueReads = append(r.queueReads, r.clock.Now().Sub(r.start))
	r.mu.Unlock()
	if err := r.clock.Sleep(ctx, r.queueLatency); err != nil {
		return nil, err
	}
	return map[string]map[string]int64{"reducer": {"pending": r.queueDepth}}, nil
}

func (r *latencyDepthReader) ReducerGraphWriteTimeoutDepth(ctx context.Context) (int64, error) {
	r.mu.Lock()
	r.pressureReads = append(r.pressureReads, r.clock.Now().Sub(r.start))
	r.mu.Unlock()
	if err := r.clock.Sleep(ctx, r.pressureLatency); err != nil {
		return 0, err
	}
	return r.pressureDepth, nil
}

func (r *latencyDepthReader) schedules() (queue, pressure []time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.queueReads), slices.Clone(r.pressureReads)
}

// at builds a virtual offset of whole seconds, extra milliseconds and
// nanoseconds; each backoff step wakes one nanosecond past its window, so the
// nanoseconds count the steps.
func at(seconds, millis, nanos int) time.Duration {
	return time.Duration(seconds)*time.Second + time.Duration(millis)*time.Millisecond + time.Duration(nanos)
}

// TestReducerAdmissionSharedDepthConcurrentWaitersFollowOneSchedule proves the
// backoff advances once per read, not once per waiter: eight waiters deferring
// at the mark for a 60 s max wait read on exactly the schedule of one waiter.
// A backoff advanced by every waiter that defers on the same read reaches the
// cap after one read and skips the 1, 3 and 7 s steps.
func TestReducerAdmissionSharedDepthConcurrentWaitersFollowOneSchedule(t *testing.T) {
	t.Parallel()

	const k = 8
	const maxWait = 60 * time.Second
	clock := newVirtualClock()
	var mu sync.Mutex
	var reads []time.Duration
	reader := newClockedDepthReader(clock, func(elapsed time.Duration) int64 {
		mu.Lock()
		reads = append(reads, elapsed)
		mu.Unlock()
		return sharedDepthMark
	})
	writer, inner := newSharedDepthWriter(reader, clock, sharedDepthConfig(maxWait))

	for i, o := range runEnqueuers(t, writer, clock, k) {
		if !errors.Is(o.err, ErrReducerAdmissionTimeout) || o.elapsed != maxWait {
			t.Fatalf("enqueuer %d = (%v after %s), want %v after exactly %s", i, o.err, o.elapsed, ErrReducerAdmissionTimeout, maxWait)
		}
	}
	want := []time.Duration{
		at(0, 0, 0), at(1, 0, 1), at(3, 0, 2), at(7, 0, 3), at(15, 0, 4),
		at(25, 0, 5), at(35, 0, 6), at(45, 0, 7), at(55, 0, 8),
	}
	mu.Lock()
	got := slices.Clone(reads)
	mu.Unlock()
	if !slices.Equal(got, want) {
		t.Fatalf("queue depth reads of %d deferring waiters at\n  %v\nwant exactly\n  %v\n(one backoff step per read for the process)", k, got, want)
	}
	if got := inner.calls.Load(); got != 0 {
		t.Fatalf("inner enqueue count = %d, want 0 while depth holds at the mark", got)
	}
}

// TestReducerAdmissionSharedDepthScheduleWithBothSignalsAndReadLatency pins
// the deferring schedule with the production signals: the graph-write-pressure
// gate on at its default marks (500 and 100, below them here) and the queue
// at the high-water mark, with instant reads and with reads that take virtual
// time (700 ms is the QA mean of the queue statement). Instant reads keep the
// one-signal schedule. Otherwise each step starts from the oldest read of the
// decision (the pressure read), the queue read lags it by the pressure read
// latency, and a decision slower than its step sleeps one more poll interval.
// The read count over a full 5 m max wait does not change: 33 per signal.
//
// One waiter only: the virtual clock jumps when every live goroutine is
// parked in Sleep, and a waiter blocked on a read in flight is not parked.
func TestReducerAdmissionSharedDepthScheduleWithBothSignalsAndReadLatency(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                    string
		queueLat, pressureLat   time.Duration
		wantQueue, wantPressure []time.Duration
	}{
		{
			name: "instant reads",
			wantQueue: []time.Duration{
				at(0, 0, 0), at(1, 0, 1), at(3, 0, 2), at(7, 0, 3), at(15, 0, 4),
				at(25, 0, 5), at(35, 0, 6), at(45, 0, 7), at(55, 0, 8),
			},
			wantPressure: []time.Duration{
				at(0, 0, 0), at(1, 0, 1), at(3, 0, 2), at(7, 0, 3), at(15, 0, 4),
				at(25, 0, 5), at(35, 0, 6), at(45, 0, 7), at(55, 0, 8),
			},
		},
		{
			name:     "queue 700 ms, pressure 100 ms",
			queueLat: 700 * time.Millisecond, pressureLat: 100 * time.Millisecond,
			wantQueue: []time.Duration{
				at(0, 100, 0), at(1, 100, 1), at(3, 100, 2), at(7, 100, 3), at(15, 100, 4),
				at(25, 100, 5), at(35, 100, 6), at(45, 100, 7), at(55, 100, 8),
			},
			wantPressure: []time.Duration{
				at(0, 0, 0), at(1, 0, 1), at(3, 0, 2), at(7, 0, 3), at(15, 0, 4),
				at(25, 0, 5), at(35, 0, 6), at(45, 0, 7), at(55, 0, 8),
			},
		},
		{
			// The first decision ends at 1.4 s, past its 1 s step, so it
			// sleeps one poll interval and every later step shifts by 1.4 s.
			name:     "queue 700 ms, pressure 700 ms",
			queueLat: 700 * time.Millisecond, pressureLat: 700 * time.Millisecond,
			wantQueue: []time.Duration{
				at(0, 700, 0), at(3, 100, 0), at(5, 100, 1), at(9, 100, 2), at(17, 100, 3),
				at(27, 100, 4), at(37, 100, 5), at(47, 100, 6), at(57, 100, 7),
			},
			wantPressure: []time.Duration{
				at(0, 0, 0), at(2, 400, 0), at(4, 400, 1), at(8, 400, 2), at(16, 400, 3),
				at(26, 400, 4), at(36, 400, 5), at(46, 400, 6), at(56, 400, 7),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			for _, maxWait := range []time.Duration{60 * time.Second, 5 * time.Minute} {
				clock := newVirtualClock()
				reader := &latencyDepthReader{
					clock: clock, start: clock.Now(),
					queueLatency: tc.queueLat, pressureLatency: tc.pressureLat,
					queueDepth: sharedDepthMark, pressureDepth: 1,
				}
				config := sharedDepthConfig(maxWait)
				config.RetryingHighWaterMark = defaultReducerAdmissionRetryingHighWaterMark
				config.RetryingLowWaterMark = defaultReducerAdmissionRetryingLowWaterMark
				writer, _ := newSharedDepthWriter(reader, clock, config)

				o := runEnqueuers(t, writer, clock, 1)[0]
				if !errors.Is(o.err, ErrReducerAdmissionTimeout) || o.elapsed != maxWait {
					t.Fatalf("max wait %s: Enqueue() = (%v after %s), want %v after exactly %s", maxWait, o.err, o.elapsed, ErrReducerAdmissionTimeout, maxWait)
				}
				queue, pressure := reader.schedules()
				if maxWait == 5*time.Minute {
					if len(queue) != 33 || len(pressure) != 33 {
						t.Fatalf("reads over 5 m = %d queue, %d pressure, want 33 each", len(queue), len(pressure))
					}
					continue
				}
				if !slices.Equal(queue, tc.wantQueue) {
					t.Fatalf("queue depth reads at\n  %v\nwant exactly\n  %v", queue, tc.wantQueue)
				}
				if !slices.Equal(pressure, tc.wantPressure) {
					t.Fatalf("graph-write-timeout depth reads at\n  %v\nwant exactly\n  %v", pressure, tc.wantPressure)
				}
			}
		})
	}
}
