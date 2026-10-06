// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

var passClock = time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)

// newPassRunner returns a runner over a fake database and statement with a
// fake clock and a JSON log buffer.
func newPassRunner(t *testing.T) (*Runner, *fakeDatabase, *fakeStatement, *bytes.Buffer) {
	t.Helper()
	database := newFakeDatabase(passClock)
	statement := &fakeStatement{entries: sampleEntries()}
	logs := &bytes.Buffer{}
	clock := &fakeClock{now: time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)}
	statement.clock, statement.cost = clock, 300*time.Millisecond
	runner := &Runner{
		DB:        database,
		Statement: statement.statement(),
		Interval:  MinInterval,
		Now:       clock.Now,
		Logger:    slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	return runner, database, statement, logs
}

// TestRunOnceWritesTheRowInOneTransaction proves one pass is one transaction
// in the ruling's order: jit off, the advisory try-lock, the database clock,
// the statement at that clock, one guarded upsert, commit.
func TestRunOnceWritesTheRowInOneTransaction(t *testing.T) {
	t.Parallel()
	runner, database, statement, _ := newPassRunner(t)

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeOK || pass.Err != nil {
		t.Fatalf("RunOnce() = %+v, want outcome ok", pass)
	}
	want := []string{"set_jit_off", "try_lock", "clock", "upsert", "commit"}
	if got := database.snapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("pass statements = %v, want %v", got, want)
	}
	if database.begun != 1 || database.committed != 1 || database.rolledBack != 0 {
		t.Fatalf("transactions begun/committed/rolled back = %d/%d/%d, want 1/1/0",
			database.begun, database.committed, database.rolledBack)
	}
	if len(database.lockArgs) != 1 || database.lockArgs[0] != store.WriterLockKey {
		t.Fatalf("try-lock args = %v, want [WriterLockKey]", database.lockArgs)
	}
	if len(statement.asOfs) != 1 || !statement.asOfs[0].Equal(passClock) {
		t.Fatalf("statement asOf = %v, want the database clock %v", statement.asOfs, passClock)
	}
	if !pass.AsOf.Equal(passClock) || pass.RowCount != len(sampleEntries()) {
		t.Fatalf("pass as_of/row_count = %v/%d, want %v/%d", pass.AsOf, pass.RowCount, passClock, len(sampleEntries()))
	}
	args := database.upsertArgs
	if len(args) != 7 {
		t.Fatalf("upsert args = %d, want 7", len(args))
	}
	if args[0] != store.ModelActiveWorkSummary || args[1] != store.SchemaVersion ||
		args[2] != strings.Repeat("cd", 32) || !args[3].(time.Time).Equal(passClock) || args[5] != 3 {
		t.Fatalf("upsert key/version/sha/as_of/row_count = %v", args[:6])
	}
	if args[4] != 300.0 {
		t.Fatalf("stored pass_duration_ms = %v, want the 300 ms compute time", args[4])
	}
	decoded, err := store.DecodeEntries([]byte(args[6].(string)))
	if err != nil || !reflect.DeepEqual(decoded, sampleEntries()) {
		t.Fatalf("stored payload = %v (%v), want the statement rows in order", decoded, err)
	}
}

// TestRunOnceBoundsThePassByTwoIntervals proves the pass carries the
// ruling's Go-side deadline of two intervals.
func TestRunOnceBoundsThePassByTwoIntervals(t *testing.T) {
	t.Parallel()
	runner, _, statement, _ := newPassRunner(t)

	runner.RunOnce(context.Background())

	if len(statement.deadlines) != 1 {
		t.Fatalf("statement saw %d deadlines, want 1", len(statement.deadlines))
	}
	if got := statement.deadlines[0]; got <= 0 || got > 2*MinInterval {
		t.Fatalf("pass deadline = %v from now, want within (0, %v]", got, 2*MinInterval)
	}
}

// TestRunOnceSkipsWhenAnotherWriterHoldsTheLock proves a second replica
// skips the tick: no statement, no upsert, the transaction rolled back.
func TestRunOnceSkipsWhenAnotherWriterHoldsTheLock(t *testing.T) {
	t.Parallel()
	runner, database, statement, _ := newPassRunner(t)
	database.lockAcquired = false

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeSkippedLock || pass.Err != nil {
		t.Fatalf("RunOnce() = %+v, want skipped_lock", pass)
	}
	if statement.callCount() != 0 {
		t.Fatalf("statement ran %d times while the lock was held elsewhere", statement.callCount())
	}
	if got, want := database.snapshot(), []string{"set_jit_off", "try_lock", "rollback"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pass statements = %v, want %v", got, want)
	}
}

// TestRunOnceSkipsAMissingTableAndWarnsOnce proves a reducer that starts
// before migration 161 skips without running the statement, warns once per
// process, and keeps passing.
func TestRunOnceSkipsAMissingTableAndWarnsOnce(t *testing.T) {
	t.Parallel()
	runner, database, statement, logs := newPassRunner(t)
	database.tableInstalled = false

	for i := 0; i < 3; i++ {
		if pass := runner.RunOnce(context.Background()); pass.Outcome != OutcomeSkippedMissingTable || pass.Err != nil {
			t.Fatalf("pass %d = %+v, want skipped_missing_table", i, pass)
		}
	}
	if statement.callCount() != 0 {
		t.Fatalf("statement ran %d times without the table", statement.callCount())
	}
	if got := strings.Count(logs.String(), `"msg":"status summary table is not installed; writer skips until the migration applies"`); got != 1 {
		t.Fatalf("missing-table warnings = %d, want 1\n%s", got, logs.String())
	}

	database.tableInstalled = true
	if pass := runner.RunOnce(context.Background()); pass.Outcome != OutcomeOK {
		t.Fatalf("pass after the migration = %+v, want ok", pass)
	}
}

// TestRunOnceTreatsAnUpsertOnAMissingTableAsSkipped proves the 42P01 that
// Upsert classifies as ErrNotInstalled (table dropped between the check and
// the write) is a skip, not an error.
func TestRunOnceTreatsAnUpsertOnAMissingTableAsSkipped(t *testing.T) {
	t.Parallel()
	runner, database, _, _ := newPassRunner(t)
	database.upsertErr = undefinedTable()

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeSkippedMissingTable || pass.Err != nil {
		t.Fatalf("RunOnce() = %+v, want skipped_missing_table", pass)
	}
	if database.committed != 0 || database.rolledBack != 1 {
		t.Fatalf("committed/rolled back = %d/%d, want 0/1", database.committed, database.rolledBack)
	}
}

// TestRunOnceCountsAGuardRejection proves an upsert the as_of guard rejects
// commits, changes nothing, and is reported as rejected_guard.
func TestRunOnceCountsAGuardRejection(t *testing.T) {
	t.Parallel()
	runner, database, _, _ := newPassRunner(t)
	database.upsertAffected = 0

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeRejectedGuard || pass.Err != nil {
		t.Fatalf("RunOnce() = %+v, want rejected_guard", pass)
	}
	if database.committed != 1 {
		t.Fatalf("committed = %d, want 1", database.committed)
	}
}

// TestRunOnceReportsEveryFailureAsAnError proves each failing step ends the
// pass as an error, rolls the transaction back, never writes a partial row,
// and logs the SQLSTATE when Postgres supplied one.
func TestRunOnceReportsEveryFailureAsAnError(t *testing.T) {
	t.Parallel()
	statementFailure := fmt.Errorf("read active work summary entries: %w", &pgconn.PgError{Code: "57014"})
	for name, tc := range map[string]struct {
		arrange   func(*fakeDatabase, *fakeStatement)
		committed int
		upserts   int
		sqlstate  string
	}{
		"begin":     {arrange: func(d *fakeDatabase, _ *fakeStatement) { d.beginErr = errors.New("pool closed") }},
		"jit off":   {arrange: func(d *fakeDatabase, _ *fakeStatement) { d.execErr = errors.New("connection reset") }},
		"statement": {arrange: func(_ *fakeDatabase, s *fakeStatement) { s.err = statementFailure }, sqlstate: "57014"},
		"upsert":    {arrange: func(d *fakeDatabase, _ *fakeStatement) { d.upsertErr = errors.New("disk full") }, upserts: 1},
		"commit":    {arrange: func(d *fakeDatabase, _ *fakeStatement) { d.commitErr = errors.New("serialization") }, upserts: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runner, database, statement, logs := newPassRunner(t)
			tc.arrange(database, statement)

			pass := runner.RunOnce(context.Background())

			if pass.Outcome != OutcomeError || pass.Err == nil {
				t.Fatalf("RunOnce() = %+v, want an error outcome", pass)
			}
			if database.committed != 0 {
				t.Fatalf("a failed pass committed %d transactions", database.committed)
			}
			if got := strings.Count(strings.Join(database.snapshot(), ","), "upsert"); got != tc.upserts {
				t.Fatalf("upserts = %d, want %d", got, tc.upserts)
			}
			if !strings.Contains(logs.String(), `"msg":"status summary writer pass failed"`) {
				t.Fatalf("no error log for the failed pass:\n%s", logs.String())
			}
			if tc.sqlstate != "" && !strings.Contains(logs.String(), `"sqlstate":"`+tc.sqlstate+`"`) {
				t.Fatalf("error log lacks sqlstate %s:\n%s", tc.sqlstate, logs.String())
			}
		})
	}
}

// TestRunOnceRejectsAnIncompleteStatement proves a statement without the
// source digest the reader fences on never writes a row.
func TestRunOnceRejectsAnIncompleteStatement(t *testing.T) {
	t.Parallel()
	runner, database, _, _ := newPassRunner(t)
	runner.Statement.SourceSHA256 = ""

	pass := runner.RunOnce(context.Background())

	if pass.Outcome != OutcomeError || database.committed != 0 {
		t.Fatalf("RunOnce() = %+v committed=%d, want an error and no commit", pass, database.committed)
	}
}
