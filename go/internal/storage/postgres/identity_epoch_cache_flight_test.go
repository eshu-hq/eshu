// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// flightQueryer is a query-text-routing fake for the shared-flight tests of
// IdentityEpochCache (#7805). The epoch probe returns whatever epoch the test
// last stored; the single load page blocks on gate so a test can pile callers
// onto one in-flight load, move the epoch, and only then let the load finish.
// Each load returns one row whose fact id names the load that produced it
// ("fact-load-1", "fact-load-2", ...), so a test can tell which flight a caller
// was served from.
type flightQueryer struct {
	epoch       atomic.Int64
	probeCalls  atomic.Int64
	loadCalls   atomic.Int64
	loadStarted chan struct{}
	gate        chan struct{}
	// loadGates overrides gate for the numbered load (1-based). A test sets it
	// before the first call and never mutates it afterwards.
	loadGates map[int64]chan struct{}
	loadErr   error
	// loadPanic makes a load panic once its gate opens.
	loadPanic bool
	// parkProbeCall, when non-zero, parks the numbered epoch probe (1-based)
	// AFTER it has read the epoch value: it signals probeReached, waits for
	// probeRelease, then returns the value it read. A test uses it to hold a
	// leader between its validating post-load probe and the end of its flight.
	parkProbeCall int64
	probeReached  chan struct{}
	probeRelease  chan struct{}
	// failProbeAfterLoad makes the first epoch probe issued after a load page
	// has been served return an error (once). That is the leader's validating
	// post-load probe; waiters probe before the load is released.
	failProbeAfterLoad bool
	loadsServed        atomic.Int64
	probeFailed        atomic.Bool
}

func newFlightQueryer(epoch int64) *flightQueryer {
	q := &flightQueryer{
		loadStarted: make(chan struct{}, 16),
		gate:        make(chan struct{}),
	}
	q.epoch.Store(epoch)
	return q
}

func (q *flightQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("flightQueryer: unexpected ExecContext call")
}

func (q *flightQueryer) QueryContext(ctx context.Context, query string, _ ...any) (db.Rows, error) {
	if !strings.Contains(query, "LIMIT") {
		n := q.probeCalls.Add(1)
		if q.failProbeAfterLoad && q.loadsServed.Load() > 0 && q.probeFailed.CompareAndSwap(false, true) {
			return nil, errors.New("probe unavailable")
		}
		epoch := q.epoch.Load()
		if q.parkProbeCall != 0 && n == q.parkProbeCall {
			q.probeReached <- struct{}{}
			<-q.probeRelease
		}
		return &queueFakeRows{rows: [][]any{{
			epoch,
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			"",
		}}}, nil
	}

	n := q.loadCalls.Add(1)
	q.loadStarted <- struct{}{}
	gate := q.gate
	if g, ok := q.loadGates[n]; ok {
		gate = g
	}
	select {
	case <-gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if q.loadPanic {
		panic("identity load exploded")
	}
	q.loadsServed.Add(1)
	if q.loadErr != nil {
		return nil, q.loadErr
	}
	return &queueFakeRows{rows: [][]any{{
		fmt.Sprintf("fact-load-%d", n), "scope-1", "gen-1",
		"oci_registry.image_tag_observation", "stable-key-1", "1.0.0",
		"oci_registry", int64(0), "reported", "oci_registry",
		"source-key-1", "uri://example", "rec-1",
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		false,
		[]byte(`{}`),
	}}}, nil
}

// flightCaller is the outcome of one concurrent get() caller.
type flightCaller struct {
	rows []facts.Envelope
	err  error
}

// startFlightCaller runs ListActiveContainerImageIdentityFacts on its own
// goroutine and delivers the outcome on the returned channel.
func startFlightCaller(ctx context.Context, store *FactStore) <-chan flightCaller {
	out := make(chan flightCaller, 1)
	go func() {
		rows, err := store.ListActiveContainerImageIdentityFacts(ctx)
		out <- flightCaller{rows: rows, err: err}
	}()
	return out
}

// awaitFlightWaiters blocks until want callers are parked on the in-flight
// load, so a test releases the load only after every waiter has joined.
func awaitFlightWaiters(t *testing.T, cache *IdentityEpochCache, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		cache.mu.Lock()
		got := 0
		if cache.loading != nil {
			got = cache.loading.waiters
		}
		cache.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("flight never reached %d waiters", want)
}

func awaitLoadStarted(t *testing.T, q *flightQueryer) {
	t.Helper()
	select {
	case <-q.loadStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("load never started")
	}
}

func collectFlightCaller(t *testing.T, name string, ch <-chan flightCaller) flightCaller {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never returned", name)
		return flightCaller{}
	}
}

func requireFactID(t *testing.T, name string, got flightCaller, want string) {
	t.Helper()
	if got.err != nil {
		t.Fatalf("%s: unexpected error: %v", name, got.err)
	}
	if len(got.rows) != 1 || got.rows[0].FactID != want {
		t.Fatalf("%s: rows = %+v, want one row with fact id %q", name, got.rows, want)
	}
}

// startFlightWaiters starts n callers behind an in-flight load and waits until
// every one is parked on it.
func startFlightWaiters(t *testing.T, store *FactStore, n int) []<-chan flightCaller {
	t.Helper()
	followers := make([]<-chan flightCaller, n)
	for i := range followers {
		followers[i] = startFlightCaller(context.Background(), store)
	}
	awaitFlightWaiters(t, store.identityCache, n)
	return followers
}

// TestIdentityEpochCacheSharedFlightServesEveryWaiterWhenEpochStable pins the
// stable-epoch case: N callers that arrive while one load runs are served by
// that one load, and the result is cached.
func TestIdentityEpochCacheSharedFlightServesEveryWaiterWhenEpochStable(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	followers := startFlightWaiters(t, store, 7)
	close(q.gate)

	requireFactID(t, "leader", collectFlightCaller(t, "leader", leader), "fact-load-1")
	for i, ch := range followers {
		name := fmt.Sprintf("waiter %d", i)
		requireFactID(t, name, collectFlightCaller(t, name, ch), "fact-load-1")
	}
	if got := q.loadCalls.Load(); got != 1 {
		t.Fatalf("loader executions = %d, want 1 (one flight serves every caller)", got)
	}

	// The stable epoch means the flight was cached: the next call is a hit.
	next := collectFlightCaller(t, "next", startFlightCaller(context.Background(), store))
	requireFactID(t, "next", next, "fact-load-1")
	if got := q.loadCalls.Load(); got != 1 {
		t.Fatalf("loader executions after cache hit = %d, want 1", got)
	}
}

// TestIdentityEpochCacheRetriesInsideFlightWhenEpochMovesDuringLoad is the
// #7805 regression for a moving epoch. The paged load is many READ COMMITTED
// statements, so a commit that lands mid-load can tear the set it returns. The
// leader must not hand that set to the waiters that joined the flight, and the
// waiters must not each queue behind their own serial load: the flight loads
// once more from the moved epoch, and every caller receives that second set,
// which is also cached because the epoch held steady through it.
func TestIdentityEpochCacheRetriesInsideFlightWhenEpochMovesDuringLoad(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	followers := startFlightWaiters(t, store, 7)

	q.epoch.Store(2) // a commit lands while the first load is still running
	close(q.gate)

	requireFactID(t, "leader", collectFlightCaller(t, "leader", leader), "fact-load-2")
	for i, ch := range followers {
		name := fmt.Sprintf("waiter %d", i)
		requireFactID(t, name, collectFlightCaller(t, name, ch), "fact-load-2")
	}
	if got := q.loadCalls.Load(); got != 2 {
		t.Fatalf("loader executions = %d, want 2 (the torn first set is retried once inside the flight)", got)
	}

	// The retry started from the moved epoch and it held, so it was cached.
	next := collectFlightCaller(t, "next", startFlightCaller(context.Background(), store))
	requireFactID(t, "next", next, "fact-load-2")
	if got := q.loadCalls.Load(); got != 2 {
		t.Fatalf("loader executions after the next call = %d, want 2 (cache hit)", got)
	}
}

// TestIdentityEpochCacheNeverDeliversTornSetToWaiters pins the F6 and F17
// contract: if the epoch is still moving after the flight's bounded attempts,
// nobody decides on either unvalidated set. The waiters re-probe and are served
// by a fresh, validated load, and the leader's own item fails with a retryable
// error so the queue re-runs it.
func TestIdentityEpochCacheNeverDeliversTornSetToWaiters(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	secondGate := make(chan struct{})
	q.loadGates = map[int64]chan struct{}{2: secondGate}
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	followers := startFlightWaiters(t, store, 3)

	q.epoch.Store(2)
	close(q.gate) // load 1 ends; its post-load probe sees epoch 2, so load 2 starts
	awaitLoadStarted(t, q)
	q.epoch.Store(3) // the epoch moves again during the retry
	close(secondGate)

	gotLeader := collectFlightCaller(t, "leader", leader)
	var retryable interface{ Retryable() bool }
	if !errors.As(gotLeader.err, &retryable) || !retryable.Retryable() || len(gotLeader.rows) != 0 {
		t.Fatalf("leader = rows %v err %v, want no rows and a retryable error", gotLeader.rows, gotLeader.err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(gotLeader.err, &classified) || classified.FailureClass() != IdentityEpochUnstableFailureClass {
		t.Fatalf("leader err %v lacks the identity_epoch_unstable failure class", gotLeader.err)
	}
	for i, ch := range followers {
		name := fmt.Sprintf("waiter %d", i)
		requireFactID(t, name, collectFlightCaller(t, name, ch), "fact-load-3")
	}
	if got := q.loadCalls.Load(); got != 3 {
		t.Fatalf("loader executions = %d, want 3 (two bounded attempts, then one validated load for the waiters)", got)
	}
}

// TestIdentityEpochCacheLateCallerAfterValidatingProbeDoesNotJoinFlight pins
// the joinable rule on its own (#7805, arbiter ruling). The leader's validating
// post-load probe has already returned the start epoch, so the flight is
// cacheable and no in-flight retry can run. A commit then lands before the
// flight finishes (the window includes sizing the whole set for the cache cap).
// A caller that probes inside that window sees the moved epoch. It must not be
// served the flight's set, which may lack its own trigger; it must be served by
// a load that starts after its probe. Without the joinable check the late
// caller receives the first flight's rows, so this test fails under that
// mutation.
func TestIdentityEpochCacheLateCallerAfterValidatingProbeDoesNotJoinFlight(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	q.parkProbeCall = 2 // call 1 is the leader's pre-load probe, call 2 its validating probe
	q.probeReached = make(chan struct{}, 1)
	q.probeRelease = make(chan struct{})
	close(q.gate) // the first load does not block
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	select {
	case <-q.probeReached:
	case <-time.After(10 * time.Second):
		t.Fatal("the leader never reached its validating probe")
	}
	// The leader holds a validated epoch of 1. A commit lands now.
	q.epoch.Store(2)
	late := startFlightWaiters(t, store, 1)[0]
	close(q.probeRelease)

	requireFactID(t, "leader", collectFlightCaller(t, "leader", leader), "fact-load-1")
	requireFactID(t, "late caller", collectFlightCaller(t, "late caller", late), "fact-load-2")
	if got := q.loadCalls.Load(); got != 2 {
		t.Fatalf("loader executions = %d, want 2 (the late caller loads after its own probe)", got)
	}
}

// TestIdentityEpochCacheFailedPostLoadProbeIsTornNotServed pins the rule that a
// set whose post-load probe could not run is unvalidated and is never decided
// on (#7805, arbiter ruling). The leader's item fails with the retryable
// identity_epoch_unstable error, the joined waiter re-probes and is served by a
// fresh validated load, and nothing is cached from the unvalidated set.
func TestIdentityEpochCacheFailedPostLoadProbeIsTornNotServed(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	q.failProbeAfterLoad = true
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	waiter := startFlightWaiters(t, store, 1)[0]
	close(q.gate)

	gotLeader := collectFlightCaller(t, "leader", leader)
	var retryable interface{ Retryable() bool }
	if !errors.As(gotLeader.err, &retryable) || !retryable.Retryable() || len(gotLeader.rows) != 0 {
		t.Fatalf("leader = rows %v err %v, want no rows and a retryable error", gotLeader.rows, gotLeader.err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(gotLeader.err, &classified) || classified.FailureClass() != IdentityEpochUnstableFailureClass {
		t.Fatalf("leader err %v lacks failure class %q", gotLeader.err, IdentityEpochUnstableFailureClass)
	}
	awaitLoadStarted(t, q) // the waiter re-probed and leads a fresh load
	requireFactID(t, "waiter", collectFlightCaller(t, "waiter", waiter), "fact-load-2")
}

// TestIdentityEpochCacheLateCallerDoesNotJoinStaleFlight guards accuracy: a
// caller that arrives after the epoch moved past the flight's start epoch could
// need rows the flight may have missed, so it must not be served by that
// flight's first set. It waits for the flight and is then served by a set
// loaded from the epoch it observed.
func TestIdentityEpochCacheLateCallerDoesNotJoinStaleFlight(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	q.epoch.Store(2)
	late := startFlightWaiters(t, store, 1)[0]
	close(q.gate)

	requireFactID(t, "leader", collectFlightCaller(t, "leader", leader), "fact-load-2")
	requireFactID(t, "late caller", collectFlightCaller(t, "late caller", late), "fact-load-2")
	if got := q.loadCalls.Load(); got != 2 {
		t.Fatalf("loader executions = %d, want 2 (the stale first set never reaches the late caller)", got)
	}
}

// TestIdentityEpochCacheLeaderCancelDoesNotFailWaiters keeps a cancelled leader
// from poisoning the callers that joined its flight: they retry rather than
// inherit a context error that belongs to someone else.
func TestIdentityEpochCacheLeaderCancelDoesNotFailWaiters(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	store := newFactStoreWithCache(q, 0)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leader := startFlightCaller(leaderCtx, store)
	awaitLoadStarted(t, q)
	waiter := startFlightWaiters(t, store, 1)[0]

	cancelLeader()
	got := collectFlightCaller(t, "leader", leader)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("leader err = %v, want context.Canceled", got.err)
	}

	awaitLoadStarted(t, q) // the waiter retried and leads a second load
	close(q.gate)
	requireFactID(t, "waiter", collectFlightCaller(t, "waiter", waiter), "fact-load-2")
}

// TestIdentityEpochCacheLoadErrorIsSharedWithWaiters keeps a failing load from
// fanning out: every caller of that flight sees its error rather than each
// queueing behind another slow serial load against a failing database.
func TestIdentityEpochCacheLoadErrorIsSharedWithWaiters(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	q.loadErr = errors.New("load page failed")
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	waiter := startFlightWaiters(t, store, 1)[0]
	close(q.gate)

	for name, ch := range map[string]<-chan flightCaller{"leader": leader, "waiter": waiter} {
		got := collectFlightCaller(t, name, ch)
		if got.err == nil || !strings.Contains(got.err.Error(), "load page failed") {
			t.Fatalf("%s err = %v, want the load error", name, got.err)
		}
	}
	if got := q.loadCalls.Load(); got != 1 {
		t.Fatalf("loader executions = %d, want 1 (the error is shared)", got)
	}
}
