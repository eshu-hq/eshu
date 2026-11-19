// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10876 waiting-race proofs: competing claimers drain parked waits while the provider commits mid-flight with exactly-once execution; disposable database per test; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestWaitingConcurrentClaimAndWakeLive: four competing claimers on
// independent connections drain parked waits while the provider commits
// mid-flight, and one waiter registers after the provider already committed.
// Exactly-once execution holds: every row succeeds once, no attempt budget
// is burned, and no wait row lingers. The wake's UPDATE re-evaluates its
// status predicate under EvalPlanQual, so a row a concurrent Fail parks
// after the wake's snapshot is still reopened by the same statement.
func TestWaitingConcurrentClaimAndWakeLive(t *testing.T) {
	ctx := context.Background()
	base, targetDSN := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	scopeID := "waiting-scope-race"
	seedWaitingScope(t, ctx, base, scopeID, "race-g0", "race-g1", clock)

	const parked = 7
	ids := make([]string, 0, parked+1)
	for i := 0; i < parked; i++ {
		id := "wait-race-parked-" + string(rune('a'+i))
		ids = append(ids, id)
		addOwnGateItem(t, ctx, base, id, scopeID, "race-g1", reducer.DomainDeployableUnitCorrelation, clock)
	}

	// Park seven rows up front: claim + Fail each through one queue.
	seedQueue := newOwnGateQueue(base, &clock, reducer.DomainDeployableUnitCorrelation)
	for range parked {
		intent, ok, err := seedQueue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("seed claim: ok=%v err=%v", ok, err)
		}
		if err := seedQueue.Fail(ctx, intent, notYetActive(scopeID, "race-g1")); err != nil {
			t.Fatalf("seed fail %s: %v", intent.IntentID, err)
		}
	}

	// The late waiter arrives after the parking: it is the only pending row,
	// so its claim is deterministic. It holds its lease past the provider
	// commit and fails after: the set-aside re-check must reopen it at once.
	ids = append(ids, "wait-race-late")
	addOwnGateItem(t, ctx, base, "wait-race-late", scopeID, "race-g1", reducer.DomainDeployableUnitCorrelation, clock)
	lateDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
	lateQueue := newOwnGateQueue(lateDB, &clock, reducer.DomainDeployableUnitCorrelation)
	lateIntent, ok, err := lateQueue.Claim(ctx)
	if err != nil || !ok {
		t.Fatalf("late claim: ok=%v err=%v", ok, err)
	}
	if lateIntent.IntentID != "wait-race-late" {
		t.Fatalf("late claimed %q, want the one unparked row", lateIntent.IntentID)
	}

	// The provider commits mid-flight on its own connection. The goroutine
	// reports through the channel: t.Fatalf is only legal on the test
	// goroutine.
	providerDone := make(chan error, 1)
	go func() {
		time.Sleep(200 * time.Millisecond)
		providerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
		providerDone <- activatePendingGenErr(ctx, providerDB, scopeID, "race-g1", &clock)
	}()

	// Four competing claimers drain on independent connections: claim, run
	// once, Ack. A row runs exactly once because the claim lease fences it;
	// the wake only makes parked rows visible, never duplicates them.
	var acked atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerDB := openReducerFairnessClaimerDB(t, ctx, targetDSN)
			workerQueue := newOwnGateQueue(workerDB, &clock, reducer.DomainDeployableUnitCorrelation)
			for {
				select {
				case <-stop:
					return
				default:
				}
				intent, ok, err := workerQueue.Claim(ctx)
				if err != nil {
					t.Errorf("worker claim: %v", err)
					return
				}
				if !ok {
					select {
					case <-stop:
						return
					case <-time.After(100 * time.Millisecond):
						continue
					}
				}
				if err := workerQueue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
					t.Errorf("worker ack %s: %v", intent.IntentID, err)
					return
				}
				acked.Add(1)
			}
		}()
	}

	// The late waiter fails after the provider committed: immediate reopen,
	// no lingering wait row.
	time.Sleep(500 * time.Millisecond)
	if err := lateQueue.Fail(ctx, lateIntent, notYetActive(scopeID, "race-g1")); err != nil {
		t.Fatalf("late fail: %v", err)
	}
	if got := readWaitingRow(t, ctx, base, "wait-race-late"); got.waitRows != 0 {
		t.Fatalf("late wait rows = %d right after Fail, want 0 (re-check reopened at once)", got.waitRows)
	}

	// Drain until every row succeeded or the bound hits.
	var stopOnce sync.Once
	stopWorkers := func() { stopOnce.Do(func() { close(stop) }) }
	t.Cleanup(stopWorkers)
	deadline := time.After(60 * time.Second)
	for {
		var succeeded int
		if err := base.QueryRowContext(ctx,
			`SELECT count(*) FROM fact_work_items WHERE scope_id = $1 AND stage = 'reducer' AND status = 'succeeded'`,
			scopeID).Scan(&succeeded); err != nil {
			t.Fatalf("count succeeded: %v", err)
		}
		if succeeded == len(ids) {
			break
		}
		select {
		case <-deadline:
			stopWorkers()
			wg.Wait()
			t.Fatalf("drained %d/%d rows in 60s", succeeded, len(ids))
		case <-time.After(200 * time.Millisecond):
		}
	}
	stopWorkers()
	wg.Wait()
	select {
	case err := <-providerDone:
		if err != nil {
			t.Fatalf("provider: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("provider never committed")
	}

	if got := acked.Load(); got != int64(len(ids)) {
		t.Fatalf("acked = %d, want %d (exactly-once execution)", got, len(ids))
	}
	for _, id := range ids {
		state := readWaitingRow(t, ctx, base, id)
		if state.status != "succeeded" {
			t.Errorf("%s status = %q, want succeeded", id, state.status)
		}
		if state.attempts != 1 {
			t.Errorf("%s attempt_count = %d, want 1 (no budget burned)", id, state.attempts)
		}
		if state.waitRows != 0 {
			t.Errorf("%s wait rows = %d, want 0", id, state.waitRows)
		}
	}
}

// TestWaitingDeferShareImprovementLive measures put-back runs before and
// after on the same waiting seed (#10876 acceptance criterion 6, #10862
// baseline method: put-back runs over total runs). BEFORE emulates the timer
// path: providers stay down while the clock advances past the backstop three
// times, so every waiter re-runs on each horizon and every re-run is wasted.
// AFTER commits the providers once: every waiter succeeds on its next run
// with zero re-parks. It also pins the per-reason WaitingCounts aggregates
// (criterion 7) on a real waiting set.
func TestWaitingDeferShareImprovementLive(t *testing.T) {
	ctx := context.Background()
	base, _ := openWaitingDB(t, ctx)
	clock := time.Now().UTC().Truncate(time.Second)
	scopeID := "waiting-scope-defer-share"
	seedWaitingScope(t, ctx, base, scopeID, "share-g0", "share-g1", clock)

	const waiters = 12
	for i := 0; i < waiters; i++ {
		addOwnGateItem(t, ctx, base, "wait-share-"+string(rune('a'+i)), scopeID, "share-g1", reducer.DomainDeployableUnitCorrelation, clock)
	}
	queue := newOwnGateQueue(base, &clock, reducer.DomainDeployableUnitCorrelation)

	// Park every waiter: 12 executions, 12 put back, 0 successes.
	parkAll := func() int {
		n := 0
		for {
			intent, ok, err := queue.Claim(ctx)
			if err != nil || !ok {
				if err != nil {
					t.Fatalf("drain claim: %v", err)
				}
				return n
			}
			n++
			if err := queue.Fail(ctx, intent, notYetActive(scopeID, "share-g1")); err != nil {
				t.Fatalf("drain fail %s: %v", intent.IntentID, err)
			}
		}
	}
	if parked := parkAll(); parked != waiters {
		t.Fatalf("park executions = %d, want %d", parked, waiters)
	}

	// The per-reason aggregates name the waiting set while it is parked.
	counts, err := WaitingCounts(ctx, SQLDB{DB: base})
	if err != nil {
		t.Fatalf("waiting counts: %v", err)
	}
	if len(counts) != 1 || counts[0].FailureClass != reducercontract.GenerationActivationNotReadyFailureClass ||
		counts[0].Count != waiters {
		t.Fatalf("waiting counts = %+v, want one row of %d %s waits", counts,
			waiters, reducercontract.GenerationActivationNotReadyFailureClass)
	}

	// BEFORE: three timer horizons with the providers still down. Every
	// re-run ends parked again: all wasted.
	wasted := 0
	for cycle := 0; cycle < 3; cycle++ {
		clock = clock.Add(WaitingBackstopDelay + time.Minute)
		wasted += parkAll()
	}
	if wasted != 3*waiters {
		t.Fatalf("timer-cycle re-runs = %d, want %d (every waiter re-ran every horizon)", wasted, 3*waiters)
	}

	// AFTER: the provider commits once. Every waiter succeeds on its next
	// run; none re-parks.
	activatePendingGen(t, ctx, base, scopeID, "share-g1", &clock)
	after := 0
	reparks := 0
	for {
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			if err != nil {
				t.Fatalf("after claim: %v", err)
			}
			break
		}
		after++
		if got := readWaitingRow(t, ctx, base, intent.IntentID); got.waitRows != 0 {
			t.Fatalf("%s still parked after the provider committed", intent.IntentID)
		}
		if err := queue.Ack(ctx, intent, reducercontract.Result{}); err != nil {
			// A still-missing provider re-parks; the wake must have fired
			// spuriously. Count it instead of failing open.
			if failErr := queue.Fail(ctx, intent, notYetActive(scopeID, "share-g1")); failErr != nil {
				t.Fatalf("re-park %s: %v", intent.IntentID, failErr)
			}
			reparks++
			continue
		}
	}
	if after != waiters || reparks != 0 {
		t.Fatalf("after provider commit: %d runs %d re-parks, want %d runs 0 re-parks", after, reparks, waiters)
	}
	t.Logf("#10876 defer share on %d %s waiters: before=%d wasted re-runs over 3 timer horizons (every re-run parked again); after=%d runs, %d successes, %d re-parks, attempt_count frozen at 1",
		waiters, reducercontract.GenerationActivationNotReadyFailureClass, wasted, after, after, reparks)

	counts, err = WaitingCounts(ctx, SQLDB{DB: base})
	if err != nil {
		t.Fatalf("waiting counts after drain: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("waiting counts after drain = %+v, want empty", counts)
	}
}
