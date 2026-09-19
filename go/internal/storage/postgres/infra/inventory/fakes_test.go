// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"database/sql"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type recordedExec struct {
	query string
	args  []any
}

type recordingTx struct {
	parent     *recordingDB
	execs      []recordedExec
	committed  bool
	rolledBack bool
}

// recordingDB is a transactional fake: every Begin opens a recordingTx that
// records its statements; failOnExec (1-based, counted across all
// transactions) injects one statement failure.
type recordingDB struct {
	mu         sync.Mutex
	txs        []*recordingTx
	execCount  int
	failOnExec int
	// unfenced makes the derive lock report a session without the writer
	// setting.
	unfenced bool
}

func (d *recordingDB) Begin(context.Context) (db.Transaction, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx := &recordingTx{parent: d}
	d.txs = append(d.txs, tx)
	return tx, nil
}

func (d *recordingDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	panic("recordingDB: statements must run inside a transaction")
}

func (d *recordingDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	panic("recordingDB: unexpected query")
}

func (t *recordingTx) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	t.parent.mu.Lock()
	defer t.parent.mu.Unlock()
	t.parent.execCount++
	t.execs = append(t.execs, recordedExec{query: query, args: args})
	if t.parent.failOnExec > 0 && t.parent.execCount == t.parent.failOnExec {
		return nil, errInjected
	}
	return driverResult(1), nil
}

// QueryContext answers the derive lock, which returns the connection's
// writer session setting: "derive" unless the parent fakes an unfenced
// session. It is recorded with the execs so statement-order assertions see it.
func (t *recordingTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	t.parent.mu.Lock()
	defer t.parent.mu.Unlock()
	if !strings.Contains(query, "pg_advisory_xact_lock") {
		panic("recordingTx: unexpected query")
	}
	t.execs = append(t.execs, recordedExec{query: query, args: args})
	setting := "derive"
	if t.parent.unfenced {
		setting = ""
	}
	return &settingRows{value: setting}, nil
}

// settingRows is a one-row, one-column result.
type settingRows struct {
	value string
	read  bool
}

func (r *settingRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (r *settingRows) Scan(dest ...any) error {
	*dest[0].(*string) = r.value
	return nil
}

func (r *settingRows) Err() error   { return nil }
func (r *settingRows) Close() error { return nil }

func (t *recordingTx) Commit() error {
	t.committed = true
	return nil
}

func (t *recordingTx) Rollback() error {
	if !t.committed {
		t.rolledBack = true
	}
	return nil
}

type driverResult int64

func (r driverResult) LastInsertId() (int64, error) { return 0, nil }
func (r driverResult) RowsAffected() (int64, error) { return int64(r), nil }

// execOnlyDB implements db.ExecQueryer but not db.Beginner.
type execOnlyDB struct{}

func (execOnlyDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return driverResult(0), nil
}

func (execOnlyDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, nil
}
