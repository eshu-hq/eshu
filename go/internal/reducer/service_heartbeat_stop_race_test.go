// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// tickHeartbeater lets the pre-heartbeat succeed, then holds the first
// periodic tick in flight until the test decides how it ends. It models a
// heartbeat UPDATE that is still on the wire when the handler returns.
type tickHeartbeater struct {
	mu        sync.Mutex
	calls     int
	tickEntry chan struct{} // closed once the first periodic tick is in flight
	tickErr   error         // when non-nil, the tick fails with it immediately
}

func newTickHeartbeater(tickErr error) *tickHeartbeater {
	return &tickHeartbeater{tickEntry: make(chan struct{}), tickErr: tickErr}
}

func (h *tickHeartbeater) Heartbeat(ctx context.Context, _ Intent) error {
	h.mu.Lock()
	h.calls++
	call := h.calls
	h.mu.Unlock()
	if call < 2 {
		return nil // immediate pre-heartbeat
	}
	if h.tickErr != nil {
		// Fail before announcing the tick so the handler only returns after the
		// failure is on record.
		if call == 2 {
			defer close(h.tickEntry)
		}
		return h.tickErr
	}
	if call == 2 {
		close(h.tickEntry) // announce the in-flight tick, then block below
	}
	<-ctx.Done() // the UPDATE only ends when its context is cancelled
	return ctx.Err()
}

// afterTickExecutor returns success as soon as the first periodic heartbeat
// tick is in flight, so the handler finishes inside the tick's round trip.
type afterTickExecutor struct {
	tickEntry <-chan struct{}
}

func (e *afterTickExecutor) Execute(ctx context.Context, intent Intent) (Result, error) {
	select {
	case <-e.tickEntry:
		return Result{IntentID: intent.IntentID, Domain: intent.Domain, Status: ResultStatusSucceeded}, nil
	case <-ctx.Done():
		return Result{}, ctx.Err()
	}
}

func stopRaceIntent() Intent {
	return Intent{
		IntentID:     "intent-stop-race",
		ScopeID:      "scope-123",
		GenerationID: "generation-456",
		SourceSystem: "git",
		Domain:       DomainSemanticEntityMaterialization,
		Cause:        "projector emitted semantic entity work",
		EntityKeys:   []string{"repo:eshu"},
		EnqueuedAt:   time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 23, 17, 0, 0, 0, time.UTC),
		Status:       IntentStatusPending,
	}
}

// stopRaceShape builds a Service for one dispatch variant of the reducer main
// loop and reports how many acks and fails the sink recorded.
type stopRaceShape struct {
	name  string
	build func(hb WorkHeartbeater, exec Executor) (Service, func() (acked, failed int))
}

func stopRaceShapes() []stopRaceShape {
	intent := stopRaceIntent()
	wait := func(context.Context, time.Duration) error { return context.Canceled }
	perItem := func(workers int) func(WorkHeartbeater, Executor) (Service, func() (int, int)) {
		return func(hb WorkHeartbeater, exec Executor) (Service, func() (int, int)) {
			sink := &stubReducerWorkSink{}
			return Service{
					PollInterval:      10 * time.Millisecond,
					WorkSource:        &stubReducerWorkSource{intents: []Intent{intent}},
					Executor:          exec,
					WorkSink:          sink,
					Heartbeater:       hb,
					HeartbeatInterval: 5 * time.Millisecond,
					Workers:           workers,
					Wait:              wait,
				}, func() (int, int) {
					sink.mu.Lock()
					defer sink.mu.Unlock()
					return sink.ackCalls, sink.failCalls
				}
		}
	}
	return []stopRaceShape{
		{name: "sequential", build: perItem(1)},
		{name: "per_item_concurrent", build: perItem(2)},
		{name: "batch_concurrent", build: func(hb WorkHeartbeater, exec Executor) (Service, func() (int, int)) {
			sink := &fakeBatchWorkSink{}
			return Service{
					PollInterval:      10 * time.Millisecond,
					WorkSource:        &fakeBatchWorkSource{intents: []Intent{intent}},
					Executor:          exec,
					WorkSink:          sink,
					Heartbeater:       hb,
					HeartbeatInterval: 5 * time.Millisecond,
					Workers:           2,
					BatchClaimSize:    2,
					Wait:              wait,
				}, func() (int, int) {
					sink.mu.Lock()
					defer sink.mu.Unlock()
					return sink.acked, sink.failed
				}
		}},
	}
}

// TestServiceRunAcksWhenHeartbeatTickIsCancelledByStop reproduces #7109: the
// handler succeeds while a periodic heartbeat UPDATE is in flight, the stop
// function cancels the heartbeat context, and the interrupted UPDATE returns
// context canceled. That self-inflicted error must not be reported as a
// heartbeat failure: the item is acked and the process keeps running.
func TestServiceRunAcksWhenHeartbeatTickIsCancelledByStop(t *testing.T) {
	t.Parallel()

	for _, shape := range stopRaceShapes() {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			hb := newTickHeartbeater(nil)
			service, outcome := shape.build(hb, &afterTickExecutor{tickEntry: hb.tickEntry})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil: a stop-cancelled heartbeat tick is not a lease failure", err)
			}
			acked, failed := outcome()
			if acked != 1 || failed != 0 {
				t.Fatalf("acked=%d failed=%d, want acked=1 failed=0", acked, failed)
			}
		})
	}
}

// TestServiceRunStillSurfacesRealHeartbeatFailureAfterSuccessfulExecute pins
// that the #7109 guard only forgives the stop-induced cancellation: a tick that
// fails for a real reason (lease lost, database error) still surfaces and the
// item is not acked.
func TestServiceRunStillSurfacesRealHeartbeatFailureAfterSuccessfulExecute(t *testing.T) {
	t.Parallel()

	errLeaseLost := errors.New("reducer claim rejected")
	for _, shape := range stopRaceShapes() {
		t.Run(shape.name, func(t *testing.T) {
			t.Parallel()

			hb := newTickHeartbeater(errLeaseLost)
			service, outcome := shape.build(hb, &afterTickExecutor{tickEntry: hb.tickEntry})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := service.Run(ctx)
			if !errors.Is(err, errLeaseLost) {
				t.Fatalf("Run() error = %v, want it to wrap the real heartbeat failure", err)
			}
			if acked, _ := outcome(); acked != 0 {
				t.Fatalf("acked=%d, want 0: an item whose lease heartbeat failed must not be acked", acked)
			}
		})
	}
}

// TestStartHeartbeatSurfacesParentCancelledTick pins that only a self-stop is
// forgiven: when the parent context is cancelled while a tick is in flight,
// the tick's context canceled error is still reported by stop.
func TestStartHeartbeatSurfacesParentCancelledTick(t *testing.T) {
	t.Parallel()

	hb := newTickHeartbeater(nil)
	// The missed-heartbeat log fires once the tick's error is on record, so
	// waiting on it orders the parent cancellation strictly before stop().
	recorded := make(chan struct{}, 1)
	service := Service{
		Heartbeater:       hb,
		HeartbeatInterval: 5 * time.Millisecond,
		Logger:            slog.New(signalHandler{recorded: recorded}),
	}
	parent, cancelParent := context.WithCancel(context.Background())
	defer cancelParent()

	_, stop := service.startHeartbeat(parent, stopRaceIntent(), 0)
	<-hb.tickEntry
	cancelParent()
	select {
	case <-recorded:
	case <-time.After(5 * time.Second):
		t.Fatal("parent-cancelled tick was never recorded as a heartbeat failure")
	}

	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("stop() error = %v, want context.Canceled from the parent-cancelled tick", err)
	}
}

// signalHandler signals on every ERROR record and drops all output.
type signalHandler struct {
	recorded chan struct{}
}

func (signalHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h signalHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		select {
		case h.recorded <- struct{}{}:
		default:
		}
	}
	return nil
}

func (h signalHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h signalHandler) WithGroup(string) slog.Handler      { return h }
