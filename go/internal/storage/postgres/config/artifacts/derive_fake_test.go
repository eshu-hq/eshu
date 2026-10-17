// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// deriveCall is one statement the fake database received.
type deriveCall struct {
	inTx   bool
	sql    string
	args   []any
	ctxErr error
}

// deriveFakeDB is a scripted Database for the hermetic derive tests. It
// answers the version read and the state read, records every statement in
// order, and fails a statement whose text contains a key of failOn.
type deriveFakeDB struct {
	mu        sync.Mutex
	version   *int64
	state     *writeState
	calls     []deriveCall
	failOn    map[string]error
	begins    int
	commits   int
	rollbacks int
}

func (f *deriveFakeDB) record(ctx context.Context, inTx bool, query string, args []any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, deriveCall{inTx: inTx, sql: query, args: args, ctxErr: ctx.Err()})
	if err := ctx.Err(); err != nil {
		return err
	}
	for key, err := range f.failOn {
		if strings.Contains(query, key) {
			return err
		}
	}
	return nil
}

func (f *deriveFakeDB) query(ctx context.Context, inTx bool, query string, args []any) (db.Rows, error) {
	if err := f.record(ctx, inTx, query, args); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch query {
	case contentVersionSQL:
		if f.version == nil {
			return &fakeRows{values: [][]any{{nil}}}, nil
		}
		return &fakeRows{values: [][]any{{*f.version}}}, nil
	case readStateSQL:
		if f.state == nil {
			return &fakeRows{}, nil
		}
		row := []any{nil, int64(f.state.extractorVersion), nil}
		if f.state.contentVersion != nil {
			row[0] = *f.state.contentVersion
		}
		if f.state.rowCount != nil {
			row[2] = int64(*f.state.rowCount)
		}
		return &fakeRows{values: [][]any{row}}, nil
	default:
		return &fakeRows{}, nil
	}
}

func (f *deriveFakeDB) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return f.query(ctx, false, query, args)
}

func (f *deriveFakeDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := f.record(ctx, false, query, args); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

func (f *deriveFakeDB) Begin(ctx context.Context) (db.Transaction, error) {
	f.mu.Lock()
	f.begins++
	f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &deriveFakeTx{db: f, ctx: ctx}, nil
}

// statements returns the SQL of every recorded call, in order.
func (f *deriveFakeDB) statements() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.calls))
	for _, call := range f.calls {
		out = append(out, call.sql)
	}
	return out
}

// called reports whether a statement was sent.
func (f *deriveFakeDB) called(query string) bool {
	for _, statement := range f.statements() {
		if statement == query {
			return true
		}
	}
	return false
}

// callOf returns the first call of a statement.
func (f *deriveFakeDB) callOf(query string) (deriveCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, call := range f.calls {
		if call.sql == query {
			return call, true
		}
	}
	return deriveCall{}, false
}

type deriveFakeTx struct {
	db  *deriveFakeDB
	ctx context.Context
}

func (t *deriveFakeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	return t.db.query(ctx, true, query, args)
}

func (t *deriveFakeTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if err := t.db.record(ctx, true, query, args); err != nil {
		return nil, err
	}
	return driver.RowsAffected(0), nil
}

func (t *deriveFakeTx) Commit() error {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	t.db.commits++
	return t.ctx.Err()
}

func (t *deriveFakeTx) Rollback() error {
	t.db.mu.Lock()
	defer t.db.mu.Unlock()
	t.db.rollbacks++
	return nil
}

// fakeRows serves fixed rows. Scan supports the destinations the derive uses.
type fakeRows struct {
	values [][]any
	next   int
	row    []any
}

func (r *fakeRows) Next() bool {
	if r.next >= len(r.values) {
		return false
	}
	r.row = r.values[r.next]
	r.next++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if len(dest) != len(r.row) {
		return fmt.Errorf("scan: %d destinations for %d columns", len(dest), len(r.row))
	}
	for i, target := range dest {
		switch typed := target.(type) {
		case sql.Scanner:
			if err := typed.Scan(r.row[i]); err != nil {
				return err
			}
		case *int64:
			value, ok := r.row[i].(int64)
			if !ok {
				return fmt.Errorf("scan column %d: %T into *int64", i, r.row[i])
			}
			*typed = value
		case *int:
			value, ok := r.row[i].(int64)
			if !ok {
				return fmt.Errorf("scan column %d: %T into *int", i, r.row[i])
			}
			*typed = int(value)
		default:
			return fmt.Errorf("scan column %d: unsupported destination %T", i, target)
		}
	}
	return nil
}

func (r *fakeRows) Err() error   { return nil }
func (r *fakeRows) Close() error { return nil }

// sqlStateError is a driver error that carries a SQLSTATE, as pgconn.PgError
// does.
type sqlStateError struct{ code string }

func (e sqlStateError) Error() string    { return "postgres error " + e.code }
func (e sqlStateError) SQLState() string { return e.code }

var errFakeLoad = errors.New("fake load failure")

func int64Ptr(v int64) *int64 { return &v }
func intPtr(v int) *int       { return &v }
