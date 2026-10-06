// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// fakeDatabase is a hermetic db.Beginner that answers the writer pass's
// statements and records what the pass did, in order.
type fakeDatabase struct {
	mu sync.Mutex

	lockAcquired   bool
	tableInstalled bool
	clock          time.Time
	upsertAffected int64
	upsertErr      error
	beginErr       error
	commitErr      error
	execErr        error

	begun      int
	committed  int
	rolledBack int
	statements []string
	upsertArgs []any
	lockArgs   []any
}

func newFakeDatabase(clock time.Time) *fakeDatabase {
	return &fakeDatabase{lockAcquired: true, tableInstalled: true, clock: clock, upsertAffected: 1}
}

func (d *fakeDatabase) Begin(context.Context) (db.Transaction, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	d.begun++
	return &fakeTx{db: d}, nil
}

func (d *fakeDatabase) record(kind string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.statements = append(d.statements, kind)
}

func (d *fakeDatabase) snapshot() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.statements...)
}

// fakeTx is one pass transaction on fakeDatabase.
type fakeTx struct {
	db   *fakeDatabase
	done bool
}

func (t *fakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(query, "SET TRANSACTION ISOLATION LEVEL READ COMMITTED"):
		t.db.record("read_committed")
		return driverResult(0), nil
	case strings.Contains(query, "SET LOCAL jit = off"):
		t.db.record("set_jit_off")
		return driverResult(0), t.db.execErr
	case strings.Contains(query, "INSERT INTO status_summary_snapshots"):
		t.db.record("upsert")
		t.db.mu.Lock()
		t.db.upsertArgs = args
		t.db.mu.Unlock()
		if t.db.upsertErr != nil {
			return nil, t.db.upsertErr
		}
		return driverResult(t.db.upsertAffected), nil
	}
	return nil, fmt.Errorf("fake tx: unexpected exec %q", query)
}

func (t *fakeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case strings.Contains(query, "pg_try_advisory_xact_lock"):
		t.db.record("try_lock")
		t.db.mu.Lock()
		t.db.lockArgs = args
		t.db.mu.Unlock()
		return &fakeRows{rows: [][]any{{t.db.lockAcquired}}}, nil
	case strings.Contains(query, "clock_timestamp()"):
		t.db.record("clock")
		return &fakeRows{rows: [][]any{{t.db.clock, t.db.tableInstalled}}}, nil
	}
	return nil, fmt.Errorf("fake tx: unexpected query %q", query)
}

func (t *fakeTx) Commit() error {
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	t.db.record("commit")
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	if t.db.commitErr != nil {
		return t.db.commitErr
	}
	t.db.committed++
	return nil
}

func (t *fakeTx) Rollback() error {
	if t.done {
		return sql.ErrTxDone
	}
	t.done = true
	t.db.record("rollback")
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	t.db.rolledBack++
	return nil
}

// driverResult is a sql.Result that reports a fixed affected-row count.
type driverResult int64

func (r driverResult) LastInsertId() (int64, error) { return 0, errors.New("not supported") }
func (r driverResult) RowsAffected() (int64, error) { return int64(r), nil }

// fakeRows serves fixed rows to Scan targets of type bool and time.Time.
type fakeRows struct {
	rows  [][]any
	index int
}

func (r *fakeRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	if len(dest) != len(row) {
		return fmt.Errorf("fake rows: scan %d targets for %d columns", len(dest), len(row))
	}
	for i, target := range dest {
		switch out := target.(type) {
		case *bool:
			*out = row[i].(bool)
		case *time.Time:
			*out = row[i].(time.Time)
		default:
			return fmt.Errorf("fake rows: unsupported scan target %T", target)
		}
	}
	return nil
}

func (r *fakeRows) Err() error   { return nil }
func (r *fakeRows) Close() error { return nil }

// fakeStatement is a Statement whose Compute returns fixed entries, records
// each call, and can advance a fake clock to simulate a slow pass.
type fakeStatement struct {
	mu        sync.Mutex
	entries   []store.Entry
	err       error
	calls     int
	asOfs     []time.Time
	deadlines []time.Duration
	clock     *fakeClock
	cost      time.Duration
	inFlight  int
	maxFlight int
	block     chan struct{}
	started   chan struct{}
}

func (s *fakeStatement) statement() Statement {
	return Statement{
		ModelKey:     store.ModelActiveWorkSummary,
		SourceSHA256: strings.Repeat("cd", 32),
		Compute:      s.compute,
	}
}

func (s *fakeStatement) compute(ctx context.Context, _ db.Queryer, asOf time.Time) ([]store.Entry, error) {
	s.mu.Lock()
	s.calls++
	s.inFlight++
	if s.inFlight > s.maxFlight {
		s.maxFlight = s.inFlight
	}
	s.asOfs = append(s.asOfs, asOf)
	if deadline, ok := ctx.Deadline(); ok {
		s.deadlines = append(s.deadlines, time.Until(deadline))
	}
	block, started := s.block, s.started
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()
	if started != nil {
		started <- struct{}{}
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.clock != nil {
		s.clock.advance(s.cost)
	}
	if s.err != nil {
		return nil, s.err
	}
	return append([]store.Entry(nil), s.entries...), nil
}

func (s *fakeStatement) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// fakeClock is a manually advanced clock for pass durations and cadence.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// undefinedTable is the Postgres error a missing status_summary_snapshots
// table raises.
func undefinedTable() error {
	return &pgconn.PgError{Code: "42P01", Message: `relation "status_summary_snapshots" does not exist`}
}

// sampleEntries is a small statement result in live order.
func sampleEntries() []store.Entry {
	return []store.Entry{
		{Section: "queue", Ordinal: 1, JSON: `{"total_count":3,"oldest_outstanding_age_seconds":4.5}`},
		{Section: "stage", Ordinal: 1, JSON: `{"stage":"reducer","status":"pending","count":2}`},
		{Section: "stage", Ordinal: 2, JSON: `{"stage":"reducer","status":"succeeded","count":1}`},
	}
}
