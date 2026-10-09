// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/containerimage"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// tearGates builds one gate per load so a test can release the loads of a
// flight one at a time.
func tearGates(n int) map[int64]chan struct{} {
	gates := make(map[int64]chan struct{}, n)
	for i := 1; i <= n; i++ {
		gates[int64(i)] = make(chan struct{})
	}
	return gates
}

// tearFlight drives the flight whose first load is load number first through
// both of its attempts with an epoch move during each, so the flight ends torn.
// epoch is the epoch the flight started from.
func tearFlight(t *testing.T, cache *IdentityEpochCache, q *flightQueryer, gates map[int64]chan struct{}, first, epoch int64) {
	t.Helper()
	awaitLoadStarted(t, q)          // attempt 1
	awaitFlightWaiters(t, cache, 1) // the waiter under test has joined this flight
	q.epoch.Store(epoch + 1)
	close(gates[first])
	awaitLoadStarted(t, q) // attempt 2, started from the moved epoch
	q.epoch.Store(epoch + 2)
	close(gates[first+1])
}

// TestContainerImageIdentityHandlerGivesUpAfterThreeTornFlights is the #7805
// lease-safety regression for waiters. A work item that joins three flights in a
// row, each of which tears, must stop waiting and fail with the retryable
// identity_epoch_unstable error. It must not decide anything, and the cache must
// not have held it past the bound. Before the bound, such a waiter re-probed and
// re-joined forever while only the leaders consumed claim attempts.
func TestContainerImageIdentityHandlerGivesUpAfterThreeTornFlights(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	gates := tearGates(6)
	q.loadGates = gates
	store := newFactStoreWithCache(q, 0)
	writer := &countingIdentityWriter{}
	handler := containerimage.ContainerImageIdentityHandler{
		FactLoader: handlerFactLoader{store: store},
		Writer:     writer,
	}

	// The waiter's first flight is led by a plain caller. After each flight
	// tears, a hook on the waiter starts the leader of the next flight and waits
	// until that flight is loading, so the waiter joins a new flight every time.
	cache := store.identityCache
	var leaders []<-chan flightCaller
	cache.afterUnservedFlight = func(waited int) {
		if waited >= maxIdentityWaiterFlights {
			return
		}
		leaders = append(leaders, startFlightCaller(context.Background(), store))
		// Wait for the next flight's first load to start. The test goroutine
		// does not tear that flight until this waiter has joined it.
		wantLoads := int64(2*waited + 1)
		deadline := time.Now().Add(10 * time.Second)
		for q.loadCalls.Load() < wantLoads {
			if time.Now().After(deadline) {
				t.Error("the next flight never started")
				return
			}
			time.Sleep(time.Millisecond)
		}
	}

	leaders = append(leaders, startFlightCaller(context.Background(), store))
	awaitLoadStarted(t, q)
	q.loadStarted <- struct{}{} // put the token back; tearFlight consumes it

	type outcome struct {
		result reducercontract.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := handler.Handle(context.Background(), reducercontract.Intent{
			IntentID:     "intent-waiter",
			ScopeID:      "scope-1",
			GenerationID: "gen-1",
			SourceSystem: "git",
			Domain:       reducercontract.DomainContainerImageIdentity,
			Cause:        "container image references observed",
		})
		done <- outcome{result: result, err: err}
	}()
	awaitFlightWaiters(t, cache, 1)

	tearFlight(t, cache, q, gates, 1, 1) // flight 1: epochs 1 -> 2 -> 3
	tearFlight(t, cache, q, gates, 3, 3) // flight 2: epochs 3 -> 4 -> 5
	tearFlight(t, cache, q, gates, 5, 5) // flight 3: epochs 5 -> 6 -> 7

	var got outcome
	select {
	case got = <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("the waiter never gave up after three torn flights")
	}
	if got.err == nil {
		t.Fatal("Handle() error = nil, want a retryable identity_epoch_unstable error")
	}
	if !reducercontract.IsRetryable(got.err) {
		t.Fatalf("Handle() error %v is not retryable", got.err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(got.err, &classified) || classified.FailureClass() != IdentityEpochUnstableFailureClass {
		t.Fatalf("Handle() error %v lacks failure class %q", got.err, IdentityEpochUnstableFailureClass)
	}
	if writer.writes != 0 {
		t.Fatalf("decision writes = %d, want 0 (no decision after giving up)", writer.writes)
	}
	if got.result.Status == reducercontract.ResultStatusSucceeded {
		t.Fatalf("Handle() result status = %q, want not succeeded", got.result.Status)
	}
	if loads := q.loadCalls.Load(); loads != 6 {
		t.Fatalf("loader executions = %d, want 6 (three flights of two attempts; the waiter never led)", loads)
	}
	for i, ch := range leaders {
		l := collectFlightCaller(t, "leader", ch)
		if l.err == nil || !strings.Contains(l.err.Error(), "did not stabilize") {
			t.Fatalf("leader %d err = %v, want the unstable error", i, l.err)
		}
	}
}

// fakeWaitClock is an injected clock for the wall-clock bound. Timers it hands
// out record the duration they were asked for and fire only when the test says.
type fakeWaitClock struct {
	mu        sync.Mutex
	current   time.Time
	requested []time.Duration
	fire      []chan time.Time
}

func (f *fakeWaitClock) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.current
}

func (f *fakeWaitClock) newTimer(d time.Duration) (<-chan time.Time, func() bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	f.requested = append(f.requested, d)
	f.fire = append(f.fire, ch)
	return ch, func() bool { return true }
}

func (f *fakeWaitClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.current = f.current.Add(d)
}

func (f *fakeWaitClock) timersRequested() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.requested...)
}

// fireTimer fires the numbered timer (0-based) at the fake current time.
func (f *fakeWaitClock) fireTimer(i int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fire[i] <- f.current
}

// TestIdentityEpochCacheWaiterGivesUpAfterOneHeartbeatInterval covers the
// wall-clock half of the waiter bound against an injected heartbeat interval and
// a fake clock, with no sleeping. A waiter parked on a flight that does not
// finish is handed a timer for exactly one heartbeat interval (30 s here, the
// reducer's default: claim lease of one minute, heartbeat at half of it). When
// that timer fires, the waiter's item fails with the retryable
// identity_epoch_unstable error. The leader is not cut off by wall time: it is
// bounded by its load attempts, so a slow healthy load still finishes.
func TestIdentityEpochCacheWaiterGivesUpAfterOneHeartbeatInterval(t *testing.T) {
	t.Parallel()

	const heartbeat = 30 * time.Second
	clock := &fakeWaitClock{current: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	q := newFlightQueryer(1)
	cache, err := NewIdentityEpochCache(testInstruments(), 0, WithHeartbeatInterval(heartbeat))
	if err != nil {
		t.Fatalf("NewIdentityEpochCache: %v", err)
	}
	cache.newTimer = clock.newTimer
	cache.now = clock.now
	store := &FactStore{database: q, identityCache: cache}

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	waiter := startFlightWaiters(t, store, 1)[0]

	deadline := time.Now().Add(10 * time.Second)
	for len(clock.timersRequested()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the waiter never asked for its wall-clock timer")
		}
		time.Sleep(time.Millisecond)
	}
	if got := clock.timersRequested(); len(got) != 1 || got[0] != heartbeat {
		t.Fatalf("waiter timers = %v, want exactly one heartbeat interval (%v)", got, heartbeat)
	}

	clock.advance(heartbeat)
	clock.fireTimer(0)
	got := collectFlightCaller(t, "waiter", waiter)
	var retryable interface{ Retryable() bool }
	if !errors.As(got.err, &retryable) || !retryable.Retryable() || len(got.rows) != 0 {
		t.Fatalf("waiter = rows %v err %v, want no rows and a retryable error", got.rows, got.err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(got.err, &classified) || classified.FailureClass() != IdentityEpochUnstableFailureClass {
		t.Fatalf("waiter err %v lacks failure class %q", got.err, IdentityEpochUnstableFailureClass)
	}
	if !strings.Contains(got.err.Error(), identityGaveUpWallClock) {
		t.Fatalf("waiter err = %v, want the wall-clock reason", got.err)
	}

	// The leader's flight was never cut off: it finishes and serves its set.
	close(q.gate)
	requireFactID(t, "leader", collectFlightCaller(t, "leader", leader), "fact-load-1")
}

// TestIdentityEpochCacheWaiterWaitBudgetIsCumulative pins that the heartbeat
// interval bounds the waiter's TOTAL time on flights, not each wait: after a
// flight that took 20 s of a 30 s budget did not serve the waiter, its next wait
// is handed only the remaining 10 s.
func TestIdentityEpochCacheWaiterWaitBudgetIsCumulative(t *testing.T) {
	t.Parallel()

	const heartbeat = 30 * time.Second
	clock := &fakeWaitClock{current: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	cache, err := NewIdentityEpochCache(testInstruments(), 0, WithHeartbeatInterval(heartbeat))
	if err != nil {
		t.Fatalf("NewIdentityEpochCache: %v", err)
	}
	cache.newTimer = clock.newTimer
	cache.now = clock.now

	flight := &identityFlight{done: make(chan struct{})}
	var waited time.Duration

	finished := make(chan error, 1)
	go func() { finished <- cache.waitForFlight(context.Background(), flight, &waited) }()
	for len(clock.timersRequested()) < 1 {
		time.Sleep(time.Millisecond)
	}
	clock.advance(20 * time.Second)
	close(flight.done)
	if err := <-finished; err != nil {
		t.Fatalf("first wait err = %v, want nil", err)
	}
	if waited != 20*time.Second {
		t.Fatalf("waited = %v, want 20s", waited)
	}

	second := &identityFlight{done: make(chan struct{})}
	go func() { finished <- cache.waitForFlight(context.Background(), second, &waited) }()
	for len(clock.timersRequested()) < 2 {
		time.Sleep(time.Millisecond)
	}
	if got := clock.timersRequested(); got[1] != 10*time.Second {
		t.Fatalf("second wait timer = %v, want the remaining 10s", got[1])
	}
	clock.advance(10 * time.Second)
	clock.fireTimer(1)
	if err := <-finished; err == nil || !strings.Contains(err.Error(), identityGaveUpWallClock) {
		t.Fatalf("second wait err = %v, want the wall-clock give-up", err)
	}
	// With the budget spent, a further wait gives up without waiting at all.
	if err := cache.waitForFlight(context.Background(), &identityFlight{done: make(chan struct{})}, &waited); err == nil {
		t.Fatal("a wait with no budget left returned nil")
	}
}

// TestIdentityEpochCacheCallerCancelIsNotAGiveUp keeps the bound from rewriting
// a caller's own cancellation: when the caller's context ends first, it gets its
// own context error, not the retryable unstable error.
func TestIdentityEpochCacheCallerCancelIsNotAGiveUp(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	cache, err := NewIdentityEpochCache(testInstruments(), 0, WithHeartbeatInterval(time.Hour))
	if err != nil {
		t.Fatalf("NewIdentityEpochCache: %v", err)
	}
	store := &FactStore{database: q, identityCache: cache}

	ctx, cancel := context.WithCancel(context.Background())
	caller := startFlightCaller(ctx, store)
	awaitLoadStarted(t, q)
	cancel()
	got := collectFlightCaller(t, "caller", caller)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("caller err = %v, want context.Canceled", got.err)
	}
}
