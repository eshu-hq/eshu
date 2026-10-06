// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	statussummary "github.com/eshu-hq/eshu/go/internal/reducer/status/summary"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// TestWriterRowEqualsLiveStatementLive is D3.2 item 1 on the writer side:
// after a pass, the stored row equals the live statement at the row's as_of,
// read in one snapshot, at 0.2 %, 50 %, and 100 % live work, and the
// comparison is shown able to differ. Item 9 (empty state) is the last case:
// with every input deleted the row holds only the queue section.
func TestWriterRowEqualsLiveStatementLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	writer := newLiveWriter(database)
	const total = 600
	for _, state := range []struct {
		name string
		live int
	}{
		{"0.2 percent live", 1},
		{"50 percent live", total / 2},
		{"100 percent live", total},
	} {
		t.Run(state.name, func(t *testing.T) {
			seedWork(ctx, t, database, total, state.live)
			if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
				t.Fatalf("RunOnce() = %+v, want ok", pass)
			}
			row := assertRowEqualsLive(ctx, t, database)
			sections := map[string]bool{}
			for _, entry := range row.Entries {
				sections[entry.Section] = true
			}
			want := []string{"stage", "backlog", "queue"}
			if state.live >= 6 {
				want = append(want, "blockage", "failure")
			}
			for _, section := range want {
				if !sections[section] {
					t.Fatalf("row has sections %v; the fixture must exercise %q", sections, section)
				}
			}
			// Non-vacuity: change one outstanding row and the same
			// comparison must report a difference.
			mustExec(ctx, t, database, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id = 'w-0'`)
			if stored, live := readRowAndLive(ctx, t, database); reflect.DeepEqual(stored.Entries, live) {
				t.Fatal("the equality check could not see a changed work item")
			}
		})
	}
	t.Run("empty", func(t *testing.T) {
		mustExec(ctx, t, database, `DELETE FROM fact_work_items`)
		mustExec(ctx, t, database, `DELETE FROM shared_projection_intents`)
		mustExec(ctx, t, database, `DELETE FROM shared_projection_partition_leases`)
		if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
			t.Fatalf("RunOnce() = %+v, want ok", pass)
		}
		row := assertRowEqualsLive(ctx, t, database)
		if row.RowCount != 1 || row.Entries[0].Section != "queue" {
			t.Fatalf("empty-state row = %+v, want one queue entry", row.Entries)
		}
	})
}

// TestWriterKilledMidPassKeepsTheOldRowLive is D3.2 item 7: a pass whose
// backend dies between the statement and the upsert leaves the previous row
// whole and still equal to live at its as_of; the next pass replaces it.
func TestWriterKilledMidPassKeepsTheOldRowLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 300, 120)
	writer := newLiveWriter(database)
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("first pass = %+v, want ok", pass)
	}
	before := storedAsOf(ctx, t, database)

	killed := newLiveWriter(database)
	killed.Statement.Compute = func(ctx context.Context, q db.Queryer, asOf time.Time) ([]store.Entry, error) {
		entries, err := postgres.ReadActiveWorkSummaryEntries(ctx, q, asOf)
		if err != nil {
			return nil, err
		}
		rows, err := q.QueryContext(ctx, `SELECT pg_backend_pid()`)
		if err != nil {
			return nil, err
		}
		var pid int
		for rows.Next() {
			if err := rows.Scan(&pid); err != nil {
				return nil, err
			}
		}
		_ = rows.Close()
		mustExec(context.Background(), t, database, `SELECT pg_terminate_backend($1)`, pid)
		return entries, nil
	}
	if pass := killed.RunOnce(ctx); pass.Outcome != statussummary.OutcomeError {
		t.Fatalf("killed pass = %+v, want an error", pass)
	}
	if got := storedAsOf(ctx, t, database); !got.Equal(before) {
		t.Fatalf("stored as_of after the killed pass = %s, want the old %s", got, before)
	}
	assertRowEqualsLive(ctx, t, database)

	// The next pass replaces the row, including a change made after the kill.
	mustExec(ctx, t, database, `UPDATE fact_work_items SET status = 'succeeded' WHERE work_item_id IN ('w-0', 'w-1')`)
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("pass after the kill = %+v, want ok", pass)
	}
	if got := storedAsOf(ctx, t, database); !got.After(before) {
		t.Fatalf("stored as_of after the next pass = %s, want after %s", got, before)
	}
	assertRowEqualsLive(ctx, t, database)
}

// TestWriterCountsAGuardRejectionLive is D3.2 item 8 through the runner: a
// stored row newer than the pass's database clock is left untouched and the
// pass reports rejected_guard.
func TestWriterCountsAGuardRejectionLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 60, 30)
	writer := newLiveWriter(database)
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("first pass = %+v, want ok", pass)
	}
	mustExec(ctx, t, database, `UPDATE status_summary_snapshots SET as_of = as_of + interval '1 hour'`)
	future := storedAsOf(ctx, t, database)

	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeRejectedGuard || pass.Err != nil {
		t.Fatalf("pass under a newer row = %+v, want rejected_guard", pass)
	}
	if got := storedAsOf(ctx, t, database); !got.Equal(future) {
		t.Fatalf("stored as_of = %s, want the newer %s kept", got, future)
	}
}

// TestWriterSkipsAMissingTableLive proves a reducer that runs before
// migration 161 skips without error, and writes once the migration applies.
func TestWriterSkipsAMissingTableLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 60, 30)
	mustExec(ctx, t, database, `DROP TABLE status_summary_snapshots`)
	writer := newLiveWriter(database)

	for i := 0; i < 2; i++ {
		if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeSkippedMissingTable || pass.Err != nil {
			t.Fatalf("pass %d without the table = %+v, want skipped_missing_table", i, pass)
		}
	}
	mustExec(ctx, t, database, summaryMigrationSQL(t))
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("pass after the migration = %+v, want ok", pass)
	}
	assertRowEqualsLive(ctx, t, database)
}

// TestWriterReplacesARowFromAnotherStatementLive is the writer half of the
// rolling-upgrade fence: a row written by a binary with another statement
// digest is replaced, digest and all, by the next production pass.
func TestWriterReplacesARowFromAnotherStatementLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 60, 30)
	older := newLiveWriter(database)
	older.Statement.SourceSHA256 = strings.Repeat("0", 64)
	if pass := older.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("older-binary pass = %+v, want ok", pass)
	}
	if row, _ := readRowAndLive(ctx, t, database); row.SourceSHA256 != strings.Repeat("0", 64) {
		t.Fatalf("stored digest = %q, want the older binary's", row.SourceSHA256)
	}

	if pass := newLiveWriter(database).RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("current-binary pass = %+v, want ok", pass)
	}
	assertRowEqualsLive(ctx, t, database) // also asserts the current digest
}

// TestWriterPassRunsReadCommittedLive proves the pass pins READ COMMITTED
// even when the database default is REPEATABLE READ, where the guarded upsert
// would raise 40001 after another writer's commit.
func TestWriterPassRunsReadCommittedLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 60, 30)
	mustExec(ctx, t, database, `DO $$ BEGIN EXECUTE format(
		'ALTER DATABASE %I SET default_transaction_isolation = ''repeatable read''', current_database()); END $$`)
	database.SetMaxIdleConns(0) // new sessions pick up the database default
	var seen string
	writer := newLiveWriter(database)
	writer.Statement.Compute = func(ctx context.Context, q db.Queryer, asOf time.Time) ([]store.Entry, error) {
		rows, err := q.QueryContext(ctx, `SHOW transaction_isolation`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err := rows.Scan(&seen); err != nil {
				return nil, err
			}
		}
		_ = rows.Close()
		return postgres.ReadActiveWorkSummaryEntries(ctx, q, asOf)
	}
	var defaultLevel string
	if err := database.QueryRowContext(ctx, `SHOW default_transaction_isolation`).Scan(&defaultLevel); err != nil {
		t.Fatalf("read the session default: %v", err)
	}
	if defaultLevel != "repeatable read" {
		t.Fatalf("session default isolation = %q; the proof needs repeatable read", defaultLevel)
	}
	if pass := writer.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("RunOnce() = %+v, want ok", pass)
	}
	if seen != "read committed" {
		t.Fatalf("pass transaction isolation = %q, want read committed", seen)
	}
}

// TestSecondWriterSkipsWhileTheFirstHoldsTheLockLive proves the advisory lock
// makes exactly one writer compute: a second writer that ticks while the
// first is mid-pass skips without running the statement.
func TestSecondWriterSkipsWhileTheFirstHoldsTheLockLive(t *testing.T) {
	ctx, database := openWriterDatabase(t)
	seedWork(ctx, t, database, 60, 30)
	entered, release := make(chan struct{}), make(chan struct{})
	first := newLiveWriter(database)
	first.Statement.Compute = func(ctx context.Context, q db.Queryer, asOf time.Time) ([]store.Entry, error) {
		close(entered)
		<-release
		return postgres.ReadActiveWorkSummaryEntries(ctx, q, asOf)
	}
	done := make(chan statussummary.Pass, 1)
	go func() { done <- first.RunOnce(ctx) }()
	<-entered

	secondRan := false
	second := newLiveWriter(database)
	second.Statement.Compute = func(ctx context.Context, q db.Queryer, asOf time.Time) ([]store.Entry, error) {
		secondRan = true
		return postgres.ReadActiveWorkSummaryEntries(ctx, q, asOf)
	}
	if pass := second.RunOnce(ctx); pass.Outcome != statussummary.OutcomeSkippedLock || secondRan {
		t.Fatalf("second writer = %+v (ran statement: %v), want skipped_lock without the statement", pass, secondRan)
	}
	close(release)
	if pass := <-done; pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("first writer = %+v, want ok", pass)
	}
	if pass := second.RunOnce(ctx); pass.Outcome != statussummary.OutcomeOK {
		t.Fatalf("second writer after the lock freed = %+v, want ok", pass)
	}
	assertRowEqualsLive(ctx, t, database)
}
