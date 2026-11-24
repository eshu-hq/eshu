// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The #10972 stamp-widening concurrency proofs. A row-set differential alone
// does not prove the merge under contention: these run the shipped reopen
// against itself and against a real claim on the same row.

// TestReopenWidensUnderEightConcurrentReopensToTheUnionLive: eight reopens of
// one pending holder, each with a distinct id, serialize on the holder's row
// lock; each merges into what the previous one committed, so the final stamp
// is the union of all eight plus the stored id, and none is lost.
func TestReopenWidensUnderEightConcurrentReopensToTheUnionLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	id := widenSeedPending(t, ctx, db, "h-widen-eight")
	want := []string{widenCVEA}
	var (
		wg       sync.WaitGroup
		start    = make(chan struct{})
		mu       sync.Mutex
		outcomes []supplychaincore.FanOutOutcome
		errs     []error
	)
	for i := 0; i < 8; i++ {
		cve := fmt.Sprintf("CVE-2026-8%03d", i)
		want = append(want, cve)
		origin := supplychaincore.FanOutOrigin{SourceScopeID: "scope:racer", SourceGenerationID: "g", SourceIntentID: fmt.Sprintf("i%d", i), CVEIDs: []string{cve}}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			reopen, err := store.ReopenHolderDetailed(ctx, "h-widen-eight", origin)
			mu.Lock()
			defer mu.Unlock()
			outcomes, errs = append(outcomes, reopen.Outcome), append(errs, err)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil || outcomes[i] != supplychaincore.FanOutStampWidened {
			t.Fatalf("racer %d: outcome %q err %v, want stamp_widened", i, outcomes[i], err)
		}
	}
	got, _ := fanOutStampCVEs(t, ctx, db, id)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stamp after 8 concurrent widens = %v, want the union %v", got, want)
	}
	if row := widenRead(t, ctx, db, id); row.status != "pending" || row.attempts != 0 {
		t.Fatalf("row = %+v, want still pending at attempt 0", row)
	}
}

// widenWaitForBlockedReopen waits until a reopen statement is waiting on a
// lock, so the test knows the interleaving it wants actually happened.
func widenWaitForBlockedReopen(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE wait_event_type = 'Lock' AND query LIKE '%WITH target AS MATERIALIZED%' AND datname = current_database()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the reopen never blocked on the claim's row lock: the interleaving did not happen")
}

// TestReopenWidensAgainstARealClaimInBothOrdersLive is the EvalPlanQual proof
// for the new WHERE and SET arms.
//
// Claim first: a real ReducerQueue.Claim runs inside an open transaction and
// holds the row; the reopen blocks on the lock. When the claim commits, the
// reopen rechecks the locked row, which is now claimed with the flag off, so it
// flags it and merges the stamp the claim saw with its change.
//
// Reopen first: the reopen widens the pending row inside an open transaction;
// a claim skips the locked row (SKIP LOCKED) and finds nothing, and once the
// widen commits, the claim takes the row with the merged stamp.
func TestReopenWidensAgainstARealClaimInBothOrdersLive(t *testing.T) {
	t.Run("claim commits first", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		id := widenSeedPending(t, ctx, db, "h-widen-epq-claim")
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		queue := fanOutLiveQueue(db)
		queue = postgres.NewReducerQueue(postgres.SQLTx{Tx: tx}, queue.LeaseOwner, queue.LeaseDuration)
		queue.ClaimDomains = fanOutLiveQueue(db).ClaimDomains
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok || intent.IntentID != id {
			t.Fatalf("claim in tx = %q ok=%v err=%v", intent.IntentID, ok, err)
		}
		done := make(chan supplychaincore.FanOutReopen, 1)
		failed := make(chan error, 1)
		go func() {
			reopen, err := fanOutLiveStore(db).ReopenHolderDetailed(ctx, "h-widen-epq-claim", widenOriginB)
			if err != nil {
				failed <- err
				return
			}
			done <- reopen
		}()
		widenWaitForBlockedReopen(t, ctx, db)
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-failed:
			t.Fatalf("reopen: %v", err)
		case reopen := <-done:
			if reopen.Outcome != supplychaincore.FanOutReplayFlagged {
				t.Fatalf("reopen after the claim committed = %+v, want replay_flagged", reopen)
			}
		}
		row := widenRead(t, ctx, db, id)
		if want := `["` + widenCVEA + `", "` + widenCVEB + `"]`; row.status != "claimed" || !row.flag || row.cves != want {
			t.Fatalf("row = %+v, want claimed, flagged, stamp %s", row, want)
		}
	})

	t.Run("reopen commits first", func(t *testing.T) {
		ctx, db := fanOutLiveDB(t)
		id := widenSeedPending(t, ctx, db, "h-widen-epq-reopen")
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		inTx := postgres.SupplyChainImpactFanOutStore{DB: postgres.SQLTx{Tx: tx}, LeaseOwner: fanOutLiveOwner}
		if reopen, err := inTx.ReopenHolderDetailed(ctx, "h-widen-epq-reopen", widenOriginB); err != nil || reopen.Outcome != supplychaincore.FanOutStampWidened {
			t.Fatalf("reopen in tx = %+v err=%v, want stamp_widened", reopen, err)
		}
		if _, ok, err := fanOutLiveQueue(db).Claim(ctx); err != nil || ok {
			t.Fatalf("claim while the widen holds the row = ok %v err %v, want no work (SKIP LOCKED)", ok, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		intent := lateClaim(t, ctx, db, id)
		if got := supplychaincore.ScopedPassCVEIDs(intent); !reflect.DeepEqual(got, []string{widenCVEA, widenCVEB}) {
			t.Fatalf("claimed pass scope = %v, want the merged [%s %s]", got, widenCVEA, widenCVEB)
		}
	})
}

// TestReopenWidensWithoutMovingTheClaimOrderLive is the starvation guard: the
// claim orders by updated_at, and a widen must not bump it, so a widened row
// is still claimed after an older pending row and before a newer one.
func TestReopenWidensWithoutMovingTheClaimOrderLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := fanOutLiveStore(db)
	ids := map[string]string{}
	for i, scope := range []string{"h-order-older", "h-order-widened", "h-order-newer"} {
		ids[scope] = widenSeedPending(t, ctx, db, scope)
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET updated_at = now() - make_interval(mins => $2) WHERE work_item_id = $1`,
			ids[scope], 10-i); err != nil {
			t.Fatal(err)
		}
	}
	if reopen, err := store.ReopenHolderDetailed(ctx, "h-order-widened", widenOriginB); err != nil || reopen.Outcome != supplychaincore.FanOutStampWidened {
		t.Fatalf("widen = %+v err=%v", reopen, err)
	}
	queue := fanOutLiveQueue(db)
	var order []string
	for range 3 {
		intent, ok, err := queue.Claim(ctx)
		if err != nil || !ok {
			t.Fatalf("claim: ok=%v err=%v", ok, err)
		}
		order = append(order, intent.ScopeID)
	}
	if want := []string{"h-order-older", "h-order-widened", "h-order-newer"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("claim order = %v, want %v: the widen moved the row in the queue", order, want)
	}
}
