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
	loadCalls   atomic.Int64
	loadStarted chan struct{}
	gate        chan struct{}
	loadErr     error
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
		return &queueFakeRows{rows: [][]any{{
			q.epoch.Load(),
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			"",
		}}}, nil
	}

	n := q.loadCalls.Add(1)
	q.loadStarted <- struct{}{}
	select {
	case <-q.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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

// TestIdentityEpochCacheSharedFlightServesWaitersWhenEpochMovesDuringLoad is
// the #7805 regression. The epoch moves while the load runs, so the result must
// not be cached, but the leader and every waiter that joined the flight must
// still receive that flight's rows instead of each queueing behind a new serial
// load. The next call after the flight starts a new load.
func TestIdentityEpochCacheSharedFlightServesWaitersWhenEpochMovesDuringLoad(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	followers := startFlightWaiters(t, store, 7)

	q.epoch.Store(2) // a commit lands while the load is still running
	close(q.gate)

	requireFactID(t, "leader", collectFlightCaller(t, "leader", leader), "fact-load-1")
	for i, ch := range followers {
		name := fmt.Sprintf("waiter %d", i)
		requireFactID(t, name, collectFlightCaller(t, name, ch), "fact-load-1")
	}
	if got := q.loadCalls.Load(); got != 1 {
		t.Fatalf("loader executions = %d, want 1 (waiters must receive the flight's rows)", got)
	}

	// The moved epoch means nothing was cached: the next call loads again.
	next := collectFlightCaller(t, "next", startFlightCaller(context.Background(), store))
	requireFactID(t, "next", next, "fact-load-2")
	if got := q.loadCalls.Load(); got != 2 {
		t.Fatalf("loader executions after the next call = %d, want 2 (uncached flight)", got)
	}
}

// TestIdentityEpochCacheLateCallerDoesNotJoinStaleFlight guards accuracy: a
// caller that arrives after the epoch moved past the flight's start epoch could
// need rows the flight may have missed, so it must not be served by that flight.
// It waits for the flight, then loads for itself.
func TestIdentityEpochCacheLateCallerDoesNotJoinStaleFlight(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	store := newFactStoreWithCache(q, 0)

	leader := startFlightCaller(context.Background(), store)
	awaitLoadStarted(t, q)
	q.epoch.Store(2)
	late := startFlightWaiters(t, store, 1)[0]
	close(q.gate)

	requireFactID(t, "leader", collectFlightCaller(t, "leader", leader), "fact-load-1")
	awaitLoadStarted(t, q) // the late caller leads a second, fresh load
	requireFactID(t, "late caller", collectFlightCaller(t, "late caller", late), "fact-load-2")
	if got := q.loadCalls.Load(); got != 2 {
		t.Fatalf("loader executions = %d, want 2 (late caller loads its own fresh set)", got)
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
