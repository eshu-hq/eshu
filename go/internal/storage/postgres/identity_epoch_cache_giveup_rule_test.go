// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestIdentityEpochCacheWaiterWithWallBudgetSpentDoesNotLead pins that a caller
// that has used up its wall-clock wait budget never leads (#7805). The waiter
// joins a flight that tears; the fake clock moves a full heartbeat interval
// while it waits, but the flight ends on its own before the timer fires. On its
// re-probe there is no flight and nothing cached, which would make it the leader
// of a new load. It must instead fail with the retryable identity_epoch_unstable
// error and start no load; the next caller, with a fresh budget, leads. Without
// the rule such a waiter led a second load after spending its wait.
func TestIdentityEpochCacheWaiterWithWallBudgetSpentDoesNotLead(t *testing.T) {
	t.Parallel()

	const heartbeat = 30 * time.Second
	clock := &fakeWaitClock{current: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	q := newFlightQueryer(1)
	secondGate := make(chan struct{})
	q.loadGates = map[int64]chan struct{}{2: secondGate}
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

	q.epoch.Store(2)
	close(q.gate) // attempt 1 ends torn; attempt 2 starts
	awaitLoadStarted(t, q)
	q.epoch.Store(3) // attempt 2 ends torn as well
	clock.advance(heartbeat)
	close(secondGate)

	requireUnstableError(t, "leader", collectFlightCaller(t, "leader", leader))
	requireUnstableError(t, "waiter", collectFlightCaller(t, "waiter", waiter))
	if got := q.loadCalls.Load(); got != 2 {
		t.Fatalf("loader executions = %d, want 2: a waiter with its wall budget spent must not start a load", got)
	}

	// The next caller arrives with a fresh budget and leads the third load.
	next := collectFlightCaller(t, "next caller", startFlightCaller(context.Background(), store))
	requireFactID(t, "next caller", next, "fact-load-3")
}

// TestIdentityEpochCacheFinalProbeServesAFilledCache pins the one final probe a
// waiter makes before it gives up (#7805). The waiter waits out three torn
// flights, so its flight budget is used up. In between, another caller's
// consistent flight filled the cache. The waiter must be served that set as a
// hit instead of failing, and must still start no load of its own. Without the
// final probe it fails retryably although a valid set is cached.
func TestIdentityEpochCacheFinalProbeServesAFilledCache(t *testing.T) {
	t.Parallel()

	q := newFlightQueryer(1)
	gates := tearGates(6)
	q.loadGates = gates
	close(q.gate) // load 7 and later do not block
	store := newFactStoreWithCache(q, 0)
	cache := store.identityCache

	var leaders []<-chan flightCaller
	var filled flightCaller
	cache.afterUnservedFlight = func(waited int) {
		if waited < maxIdentityWaiterFlights {
			leaders = append(leaders, startFlightCaller(context.Background(), store))
			wantLoads := int64(2*waited + 1)
			deadline := time.Now().Add(10 * time.Second)
			for q.loadCalls.Load() < wantLoads {
				if time.Now().After(deadline) {
					t.Error("the next flight never started")
					return
				}
				time.Sleep(time.Millisecond)
			}
			return
		}
		// After the third torn flight a consistent flight fills the cache. The
		// epoch is steady now, so this caller's load validates and is cached.
		filled = collectFlightCaller(t, "filler", startFlightCaller(context.Background(), store))
	}

	leaders = append(leaders, startFlightCaller(context.Background(), store))
	awaitLoadStarted(t, q)
	q.loadStarted <- struct{}{} // tearFlight consumes the first load's token
	waiter := startFlightCaller(context.Background(), store)
	awaitFlightWaiters(t, cache, 1)

	tearFlight(t, cache, q, gates, 1, 1)
	tearFlight(t, cache, q, gates, 3, 3)
	tearFlight(t, cache, q, gates, 5, 5)

	got := collectFlightCaller(t, "waiter", waiter)
	requireFactID(t, "filler", filled, "fact-load-7")
	requireFactID(t, "waiter", got, "fact-load-7")
	if loads := q.loadCalls.Load(); loads != 7 {
		t.Fatalf("loader executions = %d, want 7: six torn attempts plus the filler's; the waiter starts none", loads)
	}
	for i, ch := range leaders {
		requireUnstableError(t, "leader", collectFlightCaller(t, "leader", ch))
		_ = i
	}
}

// TestIdentityEpochCacheGiveUpHonorsTheCallersContext pins that the final probe
// runs on the caller's own context: a caller whose context has ended gets its
// own context error, not a give-up, and no gave_up outcome is counted.
func TestIdentityEpochCacheGiveUpHonorsTheCallersContext(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("giveup-ctx"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	cache, err := NewIdentityEpochCache(inst, 0)
	if err != nil {
		t.Fatalf("NewIdentityEpochCache: %v", err)
	}
	store := &FactStore{database: newFlightQueryer(1), identityCache: cache}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rows, err := cache.giveUp(ctx, store, identityWaiterGaveUpFlights, identityGaveUpFlights)
	if !errors.Is(err, context.Canceled) || len(rows) != 0 {
		t.Fatalf("giveUp = rows %v err %v, want context.Canceled", rows, err)
	}
	outcomes := sumByAttribute(t, reader, "eshu_dp_identity_cache_flight_waiter_total", "outcome")
	if outcomes[identityWaiterGaveUpFlights] != 0 {
		t.Fatalf("flight_waiter_total = %v, want no gave_up outcome for an ended context", outcomes)
	}
}

// cancelDuringProbeQueryer cancels the caller's context while the epoch probe
// runs and fails the probe with the context error, as a driver does.
type cancelDuringProbeQueryer struct {
	cancel context.CancelFunc
}

func (q *cancelDuringProbeQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected ExecContext call")
}

func (q *cancelDuringProbeQueryer) QueryContext(ctx context.Context, _ string, _ ...any) (db.Rows, error) {
	q.cancel()
	return nil, ctx.Err()
}

// TestIdentityEpochCacheGiveUpReturnsTheContextErrorWhenItEndsDuringTheProbe
// pins that a context that ends while the final probe runs yields the caller's
// own context error, not a give-up, and counts no gave_up outcome.
func TestIdentityEpochCacheGiveUpReturnsTheContextErrorWhenItEndsDuringTheProbe(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("giveup-probe-ctx"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	cache, err := NewIdentityEpochCache(inst, 0)
	if err != nil {
		t.Fatalf("NewIdentityEpochCache: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := &FactStore{database: &cancelDuringProbeQueryer{cancel: cancel}, identityCache: cache}

	rows, err := cache.giveUp(ctx, store, identityWaiterGaveUpWall, identityGaveUpWallClock)
	if !errors.Is(err, context.Canceled) || len(rows) != 0 {
		t.Fatalf("giveUp = rows %v err %v, want context.Canceled", rows, err)
	}
	outcomes := sumByAttribute(t, reader, "eshu_dp_identity_cache_flight_waiter_total", "outcome")
	if outcomes[identityWaiterGaveUpWall] != 0 || outcomes[identityWaiterGaveUpFlights] != 0 {
		t.Fatalf("flight_waiter_total = %v, want no gave_up outcome when the context ended", outcomes)
	}
}
