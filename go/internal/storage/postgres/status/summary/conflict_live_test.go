// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// TestStatusSummaryConflictWaitLive proves the as_of guard under real
// contention, not just under two writers that happen to run one after the
// other. INSERT ... ON CONFLICT DO UPDATE ... WHERE under READ COMMITTED waits
// for an in-flight transaction that holds the conflicting row, then locks the
// newest committed version and evaluates the WHERE against that version rather
// than the statement snapshot. Each case holds one writer's transaction open,
// proves from pg_stat_activity that the other writer is blocked on a lock (so
// the two genuinely overlapped), releases the holder, and checks both the
// waiter's advanced result and the stored row.
//
// The writer pass must therefore stay READ COMMITTED: under REPEATABLE READ a
// conflict with a row committed after the snapshot raises SQLSTATE 40001
// instead of waiting and rechecking.
func TestStatusSummaryConflictWaitLive(t *testing.T) {
	ctx, database := openProofDatabase(t)
	applySummaryMigration(ctx, t, database)
	store := summary.NewStore(poolStore{database})

	base := proofRow(proofAsOf, "base", 3)
	older := proofRow(proofAsOf.Add(10*time.Second), "older", 3)
	newer := proofRow(proofAsOf.Add(20*time.Second), "newer", 3)

	for _, tc := range []struct {
		name         string
		seeded       *summary.Row // committed before the holder starts; nil = empty table
		held         summary.Row  // written by the holder, left uncommitted
		waiting      summary.Row  // written by the waiter while the holder is open
		holderCommit bool
		waiterWins   bool
		stored       summary.Row // row expected after both finish
	}{
		{"newer held on an existing row, older waits and is rejected", &base, newer, older, true, false, newer},
		{"older held on an existing row, newer waits and advances", &base, older, newer, true, true, newer},
		{"newer held as the first insert, older waits and is rejected", nil, newer, older, true, false, newer},
		{"older held as the first insert, newer waits and advances", nil, older, newer, true, true, newer},
		{"holder rolls back an update, waiter advances over the base", &base, newer, older, false, true, older},
		{"holder rolls back the first insert, waiter inserts", nil, newer, older, false, true, older},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := database.ExecContext(ctx, `DELETE FROM status_summary_snapshots`); err != nil {
				t.Fatalf("reset table: %v", err)
			}
			if tc.seeded != nil {
				mustUpsert(ctx, t, store, *tc.seeded, true)
			}

			holderConn, holderTx := beginConnTx(ctx, t, database)
			defer func() { _ = holderConn.Close() }()
			if advanced, err := summary.Upsert(ctx, txStore{holderTx}, tc.held); err != nil || !advanced {
				t.Fatalf("holder Upsert() = %v, %v, want true, nil", advanced, err)
			}

			waiterConn, err := database.Conn(ctx)
			if err != nil {
				t.Fatalf("waiter Conn(): %v", err)
			}
			defer func() { _ = waiterConn.Close() }()
			var waiterPID int
			if err := waiterConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&waiterPID); err != nil {
				t.Fatalf("waiter pg_backend_pid(): %v", err)
			}
			type outcome struct {
				advanced bool
				err      error
			}
			done := make(chan outcome, 1)
			go func() {
				advanced, err := summary.Upsert(ctx, connStore{waiterConn}, tc.waiting)
				done <- outcome{advanced, err}
			}()

			waitUntilBlockedOnLock(ctx, t, database, waiterPID)
			select {
			case got := <-done:
				t.Fatalf("waiter finished while the holder was uncommitted: %+v (it did not wait)", got)
			default:
			}

			if tc.holderCommit {
				if err := holderTx.Commit(); err != nil {
					t.Fatalf("holder Commit(): %v", err)
				}
			} else if err := holderTx.Rollback(); err != nil {
				t.Fatalf("holder Rollback(): %v", err)
			}
			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("waiter Upsert() error = %v", got.err)
				}
				if got.advanced != tc.waiterWins {
					t.Fatalf("waiter advanced = %v, want %v", got.advanced, tc.waiterWins)
				}
			case <-time.After(15 * time.Second):
				t.Fatal("the waiter stayed blocked 15s after the holder ended")
			}
			assertStored(ctx, t, store, tc.stored)
		})
	}
}

// beginConnTx opens a transaction on a dedicated connection so the test can
// hold it open while another connection writes.
func beginConnTx(ctx context.Context, t *testing.T, database *sql.DB) (*sql.Conn, *sql.Tx) {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn(): %v", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("BeginTx(): %v", err)
	}
	return conn, tx
}

// waitUntilBlockedOnLock polls pg_stat_activity until the backend is waiting on
// a heavyweight lock, which is how a writer waiting on another transaction's
// in-flight conflicting row shows up.
func waitUntilBlockedOnLock(ctx context.Context, t *testing.T, database *sql.DB, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waitType sql.NullString
		if err := database.QueryRowContext(ctx,
			`SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&waitType); err != nil {
			t.Fatalf("read pg_stat_activity for pid %d: %v", pid, err)
		}
		if waitType.Valid && waitType.String == "Lock" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("backend %d never blocked on a lock (wait_event_type=%q): the writers did not overlap", pid, waitType.String)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
