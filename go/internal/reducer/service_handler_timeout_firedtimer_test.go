// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sync"
	"testing"
	"time"
)

// firedTimerService builds a Service whose bound timer is already fired AND
// whose handler outcome is already buffered when executeHandler reaches its
// select, so both the done arm and the timer arm are ready and Go picks one at
// random. The timer seam blocks until the outcomeBuffered seam reports that
// the handler goroutine has sent its outcome on the done channel, then hands
// back a zero-duration timer: the runtime treats an expired timer's channel as
// ready when the select polls it. Waiting on the send itself, not on Execute
// returning plus a settle sleep, is what keeps a descheduled handler goroutine
// from turning the buffered-outcome recheck into an abandonment (#11239).
func firedTimerService(exec Executor, bound time.Duration) Service {
	buffered := make(chan struct{})
	var once sync.Once
	return Service{
		Executor:        exec,
		HandlerTimeout:  bound,
		outcomeBuffered: func() { once.Do(func() { close(buffered) }) },
		newBoundTimer: func(time.Duration) *time.Timer {
			<-buffered
			return time.NewTimer(0)
		},
	}
}

// TestExecuteHandlerFiredTimerAttributesFromBoundNotTimer is the #11193 F1
// regression. Both select arms are ready (timer fired, outcome buffered), so
// Go chooses at random; the class must not depend on that choice. A
// cooperative return after the bound's own deadline is always the timeout
// class; a DeadlineExceeded that is not our bound's (downstream, or a parent
// deadline at or before the bound) is never relabeled. Each direction runs 400
// times so the random select is exercised on both arms.
func TestExecuteHandlerFiredTimerAttributesFromBoundNotTimer(t *testing.T) {
	t.Parallel()

	const runs = 400
	cases := []struct {
		name        string
		bound       time.Duration
		execCtx     func(t *testing.T) context.Context
		executor    Executor
		wantTimeout bool
	}{
		{
			name:        "own deadline expired before cooperative return",
			bound:       2 * time.Millisecond,
			execCtx:     func(*testing.T) context.Context { return context.Background() },
			executor:    &ctxDoneReducerExecutor{},
			wantTimeout: true,
		},
		{
			name:        "downstream deadline well before the bound",
			bound:       time.Hour,
			execCtx:     func(*testing.T) context.Context { return context.Background() },
			executor:    &deadlineExceededReducerExecutor{},
			wantTimeout: false,
		},
		{
			name:  "parent deadline before the bound",
			bound: time.Hour,
			execCtx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			executor:    &ctxDoneReducerExecutor{},
			wantTimeout: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			relabeled := 0
			for i := 0; i < runs; i++ {
				service := firedTimerService(tc.executor, tc.bound)
				_, err, abandoned := callExecuteHandlerWithWatchdog(t, service, tc.execCtx(t), context.Background(), abandonTestIntent("intent-fired-timer"))
				if abandoned {
					t.Fatalf("run %d: abandoned = true, want false: the outcome was already buffered", i)
				}
				if err == nil {
					t.Fatalf("run %d: error = nil, want a deadline outcome", i)
				}
				if reducerExecutionFailureClass(err) == wantTimeoutFailureClass {
					relabeled++
				}
			}
			want := 0
			if tc.wantTimeout {
				want = runs
			}
			if relabeled != want {
				t.Fatalf("timeout class on %d/%d runs, want %d/%d: attribution must come from the bound's own deadline, not from which select arm won", relabeled, runs, want, runs)
			}
		})
	}
}

// TestExecuteWithTelemetryCooperativeReturnBeforeTimerCountsTimeout pins the
// #11193 observability claim: a cooperative handler that returns at the bound
// before the timer fires takes the non-abandoned branch, and the existing
// timed-out counter still increments once for the domain. The injected timer
// never fires, so only the bound's own deadline can drive the attribution.
func TestExecuteWithTelemetryCooperativeReturnBeforeTimerCountsTimeout(t *testing.T) {
	t.Parallel()

	intent := abandonTestIntent("intent-timeout-cooperative-counter")
	state := &sharedAbandonState{}
	sink := &abandonSingleSink{state: state}
	instruments, reader := timeoutTestInstruments(t)

	service := Service{
		Executor:          &ctxDoneReducerExecutor{},
		WorkSink:          sink,
		Heartbeater:       &abandonObservingHeartbeater{state: state},
		HeartbeatInterval: 2 * time.Millisecond,
		Instruments:       instruments,
		HandlerTimeout:    50 * time.Millisecond,
		newBoundTimer:     func(time.Duration) *time.Timer { return time.NewTimer(time.Hour) },
	}

	done := make(chan error, 1)
	go func() { done <- service.executeWithTelemetry(context.Background(), intent, 0) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeWithTelemetry() error = %v, want nil: the timeout tail releases the item through Fail", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("executeWithTelemetry() did not return within 10s of the bound expiring")
	}

	acks, fails, failedWith := sink.counts()
	if fails != 1 || acks != 0 {
		t.Fatalf("fail/ack calls = %d/%d, want 1/0: a cooperative timeout is failed once, never acked", fails, acks)
	}
	assertTimeoutFailure(t, failedWith)

	rm := collectTimeoutMetrics(t, reader)
	want := map[string]string{"domain": string(intent.Domain)}
	if got := counterSum(rm, "eshu_dp_reducer_handler_timed_out_total", want); got != 1 {
		t.Fatalf("handler timed-out counter = %d, want 1 with domain %q", got, intent.Domain)
	}
	if got := counterSum(rm, "eshu_dp_reducer_handler_abandoned_total", want); got != 0 {
		t.Fatalf("handler abandoned counter = %d, want 0: the handler returned, it was not abandoned", got)
	}
}
