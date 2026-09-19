// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

import (
	"context"
	"database/sql"
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

func (t *recordingTx) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	panic("recordingTx: unexpected query")
}

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
