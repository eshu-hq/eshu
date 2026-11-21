// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	adminstore "github.com/eshu-hq/eshu/go/internal/query/admin/store"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// deadLetterHoldRow locks one work item row in an open transaction, so the
// callers that race for it queue behind one lock; release commits it.
func deadLetterHoldRow(t *testing.T, ctx context.Context, db *sql.DB, id string) (release func()) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM fact_work_items WHERE work_item_id = $1 FOR UPDATE`, id); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	return func() {
		if err := tx.Commit(); err != nil {
			t.Errorf("release row lock: %v", err)
		}
	}
}

// deadLetterAwaitLockWaiters waits until want backends of this database are
// blocked on a lock, so the release starts every racer from the same point.
func deadLetterAwaitLockWaiters(t *testing.T, ctx context.Context, db *sql.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND pid <> pg_backend_pid()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Logf("only %d of %d racers blocked on the row lock after 10s; checking the outcome anyway", waiting, want)
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestReopenHolderDeadLetterTwoConcurrentReopensMoveItOnceLive proves two fan-outs
// that reach one dead holder at once move it once: the first reopens it and
// the second, re-reading the locked row, sees a queued item and is a no-op.
func TestReopenHolderDeadLetterTwoConcurrentReopensMoveItOnceLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := deadLetterStore(db)
	fanOutSeedScope(t, ctx, db, "holder-race", "g1")
	id := fanOutSeedItem(t, ctx, db, "holder-race", "g1")
	deadLetterSeed(t, ctx, db, id, supplychaincore.SupplyChainImpactFanOutFailureClass)

	release := deadLetterHoldRow(t, ctx, db, id)
	results := make([]supplychaincore.FanOutReopen, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			origin := supplychaincore.FanOutOrigin{SourceScopeID: fmt.Sprintf("scope:racer-%d", i), SourceGenerationID: "g", SourceIntentID: "i"}
			results[i], errs[i] = store.ReopenHolderDetailed(ctx, "holder-race", origin)
		}(i)
	}
	deadLetterAwaitLockWaiters(t, ctx, db, 2)
	release()
	wg.Wait()

	counts := map[supplychaincore.FanOutOutcome]int{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("racer %d error = %v", i, err)
		}
		counts[results[i].Outcome]++
	}
	if counts[supplychaincore.FanOutDeadLetterReopened] != 1 || counts[supplychaincore.FanOutPendingNoop] != 1 {
		t.Fatalf("outcomes = %v, want one dead_letter_reopened and one pending_noop", results)
	}
	assertGenuineReopen(t, deadLetterRead(t, ctx, db, id))
}

// TestReopenHolderDeadLetterRacesTheAdminReplayLive proves the fan-out reopen and
// the operator replay endpoint's store statement never both move one dead
// letter: exactly one of them moves it, the replay event exists only when the
// replay moved it, and the row is pending either way. Eight holders race so
// both orders occur.
func TestReopenHolderDeadLetterRacesTheAdminReplayLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	store := deadLetterStore(db)
	replay := adminstore.NewStore(db)
	movers := map[string]int{}
	for i := 0; i < 8; i++ {
		scope := fmt.Sprintf("holder-replay-%d", i)
		fanOutSeedScope(t, ctx, db, scope, "g1")
		id := fanOutSeedItem(t, ctx, db, scope, "g1")
		deadLetterSeed(t, ctx, db, id, supplychaincore.SupplyChainImpactFanOutFailureClass)

		var (
			wg        sync.WaitGroup
			reopen    supplychaincore.FanOutReopen
			reopenErr error
			replayed  []admin.WorkItem
			replayErr error
		)
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			reopen, reopenErr = store.ReopenHolderDetailed(ctx, scope, fanOutLiveOrigin)
		}()
		go func() {
			defer wg.Done()
			<-start
			replayed, replayErr = replay.ReplayFailedWorkItems(ctx, admin.ReplayWorkItemFilter{
				WorkItemIDs: []string{id}, Limit: 1, ExcludeFailureClasses: recovery.UnsafeReplayFailureClasses(),
			})
		}()
		close(start)
		wg.Wait()
		if reopenErr != nil || replayErr != nil {
			t.Fatalf("holder %d: reopen err=%v replay err=%v", i, reopenErr, replayErr)
		}
		reopenMoved := reopen.Outcome == supplychaincore.FanOutDeadLetterReopened
		replayMoved := len(replayed) == 1
		if reopenMoved == replayMoved {
			t.Fatalf("holder %d: reopen=%+v replayed=%d, want exactly one mover", i, reopen, len(replayed))
		}
		var events int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_replay_events WHERE work_item_id = $1`, id).Scan(&events); err != nil {
			t.Fatal(err)
		}
		got := deadLetterRead(t, ctx, db, id)
		switch {
		case reopenMoved:
			movers["fan-out"]++
			if events != 0 {
				t.Fatalf("holder %d: %d replay events after the fan-out moved the row", i, events)
			}
			assertGenuineReopen(t, got)
		default:
			movers["replay"]++
			if events != 1 || got.status != "pending" || got.attempts != 4 || reopen.Outcome != supplychaincore.FanOutPendingNoop {
				t.Fatalf("holder %d: replay moved it but events=%d row=%+v reopen=%+v", i, events, got, reopen)
			}
		}
	}
	t.Logf("movers across 8 races: %v", movers)
}

// TestReopenHolderDeadLetterInsideTheCallersTransactionLive proves the #10055
// commit-skew fence's in-transaction reopen covers a dead letter too: the
// reopen is invisible until the caller commits and vanishes on rollback.
func TestReopenHolderDeadLetterInsideTheCallersTransactionLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	for _, commit := range []bool{true, false} {
		scope := fmt.Sprintf("holder-tx-%v", commit)
		fanOutSeedScope(t, ctx, db, scope, "g1")
		id := fanOutSeedItem(t, ctx, db, scope, "g1")
		deadLetterSeed(t, ctx, db, id, "reducer_handler_timeout")
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := postgres.ReopenSupplyChainImpactHolderInTransaction(ctx, postgres.SQLTx{Tx: tx}, scope, "scope:retracted", "g-retracted", deadLetterNow); err != nil {
			_ = tx.Rollback()
			t.Fatalf("in-transaction reopen: %v", err)
		}
		if outside := deadLetterRead(t, ctx, db, id); outside.status != "dead_letter" {
			_ = tx.Rollback()
			t.Fatalf("before commit another session sees %q, want dead_letter", outside.status)
		}
		if commit {
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			assertGenuineReopen(t, deadLetterRead(t, ctx, db, id))
			continue
		}
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
		if got := deadLetterRead(t, ctx, db, id); got.status != "dead_letter" || got.attempts != 4 || !got.hasStamp {
			t.Fatalf("after rollback row = %+v, want the dead letter unchanged", got)
		}
	}
}
