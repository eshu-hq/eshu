// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9320 PostgreSQL 18.6 concurrency proof of the reopen stamp on one fact_work_items row: two bounded replayers split 40 dead-letter rows with no row replayed twice, a replay blocked on a poison sweep row lock keeps the sweep stamp, a re-projection blocked on a claimer leaves the live claim alone and one blocked on an Ack stamps from succeeded, and a rolled-back replay leaves no stamp before the retry stamps once; interleavings fixed by pg_blocking_pids; about 1.3 s locally in an isolated schema
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/poison"
)

// TestReopenStampConcurrentWritersLive is the #9320 concurrency proof. The
// conflict domain is one fact_work_items row; the writers are the dead-letter
// replay, the poison sweep, the reconcile re-projection upsert, and a claimer
// or Ack. It proves the stamp lands once per real terminal-to-pending
// transition, from the writer that made it, and never on a row a concurrent
// claim holds:
//
//   - two replayers over the same dead-letter set split it: every row is
//     replayed once and carries the stamp of the replayer that moved it;
//   - a replay that waits on a poison sweep's row lock rechecks the row after
//     the sweep commits, finds it pending, and leaves the sweep's stamp;
//   - a re-projection that waits on a claimer's row lock skips the now
//     live-claimed row and leaves its reopen evidence alone, and one that
//     waits on an Ack stamps from the row's committed succeeded status;
//   - a replay rolled back after its UPDATE leaves no stamp, the retry stamps
//     once, and a repeat replay of the now-pending row is a no-op.
//
// Skipped unless ESHU_POSTGRES_DSN names a disposable Postgres.
func TestReopenStampConcurrentWritersLive(t *testing.T) {
	database, ctx := openIsolatedLiveDB(t, "reopen_race", "set ESHU_POSTGRES_DSN to run the #9320 reopen-stamp concurrency proof")
	now := time.Now().UTC().Truncate(time.Microsecond)
	f := newReopenStampFixture(ctx, database, now)
	store := NewRecoveryStore(SQLDB{DB: database})

	t.Run("two replayers split one dead-letter set", func(t *testing.T) {
		f.scope(t, "s-two", "g-two", f.old)
		const rows = 40
		for i := range rows {
			f.row(t, reopenStampRow{
				id: fmt.Sprintf("r-two-%02d", i), scopeID: "s-two", generationID: "g-two",
				stage: "reducer", domain: "d-two", status: "dead_letter",
			})
		}
		stamps := []time.Time{now.Add(time.Second), now.Add(2 * time.Second)}
		results := make([]recovery.ReplayResult, len(stamps))
		errs := make([]error, len(stamps))
		var ready, done sync.WaitGroup
		gate := make(chan struct{})
		for i, stamp := range stamps {
			ready.Add(1)
			done.Add(1)
			go func() {
				defer done.Done()
				ready.Done()
				<-gate
				results[i], errs[i] = store.ReplayFailedWorkItems(ctx, recovery.ReplayFilter{
					Stage: recovery.StageReducer, ScopeIDs: []string{"s-two"}, Limit: rows,
				}, stamp)
			}()
		}
		ready.Wait()
		close(gate)
		done.Wait()
		owner := map[string]time.Time{}
		for i, result := range results {
			if errs[i] != nil {
				t.Fatalf("replayer %d error = %v", i, errs[i])
			}
			for _, id := range result.WorkItemIDs {
				if _, dup := owner[id]; dup {
					t.Fatalf("%s replayed by both replayers", id)
				}
				owner[id] = stamps[i]
			}
		}
		if len(owner) != rows {
			t.Fatalf("replayed %d distinct rows, want %d", len(owner), rows)
		}
		for id, stamp := range owner {
			f.assertReopenedAt(t, id, stamp)
		}
	})

	t.Run("replay waiting on a poison sweep keeps the sweep stamp", func(t *testing.T) {
		f.scope(t, "s-poison-race", "g-poison-race", f.old)
		f.row(t, reopenStampRow{
			id: "r-poison-race", scopeID: "s-poison-race", generationID: "g-poison-race",
			stage: "reducer", domain: "d-poison-race", status: "dead_letter",
		})
		sweepAt, replayAt := now.Add(time.Minute), now.Add(2*time.Minute)
		holder := beginLockHolder(ctx, t, database)
		if _, err := poison.NewStore(holder.tx).RecoverDeadLetters(ctx, poison.Policy{MaxRecoverAttempts: 3, BatchLimit: 100}, sweepAt); err != nil {
			t.Fatalf("RecoverDeadLetters() in holder error = %v", err)
		}
		waiter := runBlocked(ctx, t, database, holder, func() error {
			result, err := store.ReplayFailedWorkItems(ctx, recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"s-poison-race"}}, replayAt)
			if err == nil && result.Replayed != 0 {
				return fmt.Errorf("replay moved %d rows the sweep already reopened", result.Replayed)
			}
			return err
		})
		holder.commit(t)
		if err := <-waiter; err != nil {
			t.Fatal(err)
		}
		f.assertReopenedAt(t, "r-poison-race", sweepAt)
	})

	t.Run("re-projection waiting on a claimer or an Ack", func(t *testing.T) {
		for _, c := range []struct {
			name, from, to string
			stamped        bool
		}{
			{name: "claim", from: "pending", to: "claimed", stamped: false},
			{name: "ack", from: "claimed", to: "succeeded", stamped: true},
		} {
			scopeID := "s-reproject-race-" + c.name
			generationID := "g-" + scopeID
			id := "projector_" + scopeID + "_" + generationID
			f.scope(t, scopeID, generationID, f.old)
			earlier := now.Add(-3 * time.Hour)
			live := now.Add(time.Hour)
			var claimUntil *time.Time
			if c.from == "claimed" {
				claimUntil = &live
			}
			f.row(t, reopenStampRow{
				id: id, scopeID: scopeID, generationID: generationID, stage: "projector",
				domain: "source_local", status: c.from, claimUntil: claimUntil, reopenedAt: &earlier,
			})
			holder := beginLockHolder(ctx, t, database)
			if _, err := holder.tx.ExecContext(ctx, `UPDATE fact_work_items
SET status = $2, lease_owner = 'claimer', claim_until = $3 WHERE work_item_id = $1`, id, c.to, live); err != nil {
				t.Fatalf("%s holder update: %v", c.name, err)
			}
			reprojectAt := now.Add(5 * time.Minute)
			waiter := runBlocked(ctx, t, database, holder, func() error {
				var matched, reopened bool
				err := database.QueryRowContext(ctx, reprojectActiveGenerationQuery,
					reprojectAt, scopeID, generationID, "hint", "commit", "race", "").Scan(&matched, &reopened)
				if err == nil && reopened != c.stamped {
					return fmt.Errorf("%s: re-projection reopened=%v, want %v", c.name, reopened, c.stamped)
				}
				return err
			})
			holder.commit(t)
			if err := <-waiter; err != nil {
				t.Fatal(err)
			}
			if c.stamped {
				f.assertReopenedAt(t, id, reprojectAt)
				continue
			}
			if got := f.state(t, id, now); got.status != "claimed" {
				t.Fatalf("%s status = %q, want the claimer's claimed row left alone", id, got.status)
			}
			f.assertReopenKept(t, id, &earlier)
		}
	})

	t.Run("retry after a rolled-back replay stamps once", func(t *testing.T) {
		f.scope(t, "s-retry", "g-retry", f.old)
		f.row(t, reopenStampRow{id: "r-retry", scopeID: "s-retry", generationID: "g-retry", stage: "reducer", domain: "d-retry", status: "dead_letter"})
		filter := recovery.ReplayFilter{Stage: recovery.StageReducer, ScopeIDs: []string{"s-retry"}}
		tx, err := SQLDB{DB: database}.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		if result, err := NewRecoveryStore(tx).ReplayFailedWorkItems(ctx, filter, now.Add(time.Minute)); err != nil || result.Replayed != 1 {
			t.Fatalf("replay in failed transaction = (%d, %v), want 1 row", result.Replayed, err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if got := f.state(t, "r-retry", now); got.status != "dead_letter" || got.reopenedAt.Valid {
			t.Fatalf("after rollback = %+v, want dead_letter with no stamp", got)
		}
		retryAt := now.Add(2 * time.Minute)
		if result, err := store.ReplayFailedWorkItems(ctx, filter, retryAt); err != nil || result.Replayed != 1 {
			t.Fatalf("retry replay = (%d, %v), want 1 row", result.Replayed, err)
		}
		if result, err := store.ReplayFailedWorkItems(ctx, filter, now.Add(3*time.Minute)); err != nil || result.Replayed != 0 {
			t.Fatalf("repeat replay = (%d, %v), want a no-op", result.Replayed, err)
		}
		f.assertReopenedAt(t, "r-retry", retryAt)
	})
}

// lockHolder is an open transaction that holds row locks a concurrent writer
// must wait on.
type lockHolder struct {
	tx  db.Transaction
	pid int
}

// beginLockHolder opens the holding transaction and records its backend pid.
func beginLockHolder(ctx context.Context, t *testing.T, database *sql.DB) lockHolder {
	t.Helper()
	tx, err := SQLDB{DB: database}.Begin(ctx)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	rows, err := tx.QueryContext(ctx, `SELECT pg_backend_pid()`)
	if err != nil {
		t.Fatalf("holder pid: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var pid int
	if !rows.Next() {
		t.Fatalf("holder pid: no row (%v)", rows.Err())
	}
	if err := rows.Scan(&pid); err != nil {
		t.Fatalf("holder pid: %v", err)
	}
	return lockHolder{tx: tx, pid: pid}
}

// commit commits the holding transaction, releasing its row locks.
func (h lockHolder) commit(t *testing.T) {
	t.Helper()
	if err := h.tx.Commit(); err != nil {
		t.Fatalf("commit holder: %v", err)
	}
}

// runBlocked starts write in a goroutine and returns once Postgres reports a
// backend blocked by the holder, so the interleaving is deterministic: the
// write has taken its snapshot and is waiting on the holder's row lock.
func runBlocked(ctx context.Context, t *testing.T, database *sql.DB, holder lockHolder, write func() error) <-chan error {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- write() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var blocked int
		if err := database.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity
WHERE $1 = ANY(pg_blocking_pids(pid))`, holder.pid).Scan(&blocked); err != nil {
			t.Fatalf("poll blocked backends: %v", err)
		}
		if blocked > 0 {
			return result
		}
		select {
		case err := <-result:
			t.Fatalf("writer finished without waiting on the holder's lock: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("writer never blocked on the holder's row lock")
	return nil
}
