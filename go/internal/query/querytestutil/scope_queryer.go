// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

// This file is a minimal generic fake database/sql driver for #5167
// store-level access-scoping tests whose store interfaces demand a concrete
// *sql.Rows, which only a database/sql round trip can build. It is not tied
// to any one read model, so root query tests and handler-family tests share
// it. It moved here from root's scope_query_test_helpers_test.go when the
// observability-coverage family left root (#6642): a helper in a _test.go
// file cannot be imported by another package's tests.

// ScopeQueryerRecorder captures every query issued through
// OpenScopeQueryerTestDB, so a test can assert both the dispatched SQL text
// (the access-scoping predicate) and the bound argument values (the granted
// repository/scope id arrays), matching the #5137 ReadLiveActivity test
// precedent (internal/storage/postgres/status_operations_test.go). It is safe
// for concurrent use.
type ScopeQueryerRecorder struct {
	mu      sync.Mutex
	queries []string
	args    [][]driver.Value
}

func (r *ScopeQueryerRecorder) record(query string, args []driver.NamedValue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, query)
	recorded := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		recorded = append(recorded, arg.Value)
	}
	r.args = append(r.args, recorded)
}

// Calls returns how many queries have been dispatched so far.
func (r *ScopeQueryerRecorder) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries)
}

// Queries returns a copy of the dispatched SQL text, in dispatch order.
func (r *ScopeQueryerRecorder) Queries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.queries...)
}

// Args returns a copy of each dispatched query's bound argument values, in
// dispatch order.
func (r *ScopeQueryerRecorder) Args() [][]driver.Value {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]driver.Value, len(r.args))
	for i, args := range r.args {
		out[i] = append([]driver.Value(nil), args...)
	}
	return out
}

var scopeQueryerDriverSeq uint64

// OpenScopeQueryerTestDB opens a *sql.DB backed by a fake driver that records
// every dispatched query/args pair and always returns the given columns/rows
// (canned, not real SQL evaluation; live WHERE-clause correctness is a
// live/integration-test concern, not a unit-test one). The DB closes when the
// test ends.
func OpenScopeQueryerTestDB(t *testing.T, columns []string, rows [][]driver.Value) (*sql.DB, *ScopeQueryerRecorder) {
	t.Helper()

	name := fmt.Sprintf("scope-queryer-test-%d", atomic.AddUint64(&scopeQueryerDriverSeq, 1))
	recorder := &ScopeQueryerRecorder{}
	sql.Register(name, &scopeQueryerDriver{recorder: recorder, columns: columns, rows: rows})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, recorder
}

type scopeQueryerDriver struct {
	recorder *ScopeQueryerRecorder
	columns  []string
	rows     [][]driver.Value
}

func (d *scopeQueryerDriver) Open(string) (driver.Conn, error) {
	return &scopeQueryerConn{recorder: d.recorder, columns: d.columns, rows: d.rows}, nil
}

type scopeQueryerConn struct {
	recorder *ScopeQueryerRecorder
	columns  []string
	rows     [][]driver.Value
}

func (c *scopeQueryerConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *scopeQueryerConn) Close() error { return nil }

func (c *scopeQueryerConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *scopeQueryerConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	c.recorder.record(query, args)
	return &scopeQueryerRows{columns: c.columns, rows: append([][]driver.Value(nil), c.rows...)}, nil
}

type scopeQueryerRows struct {
	columns []string
	rows    [][]driver.Value
	pos     int
}

func (r *scopeQueryerRows) Columns() []string { return r.columns }
func (r *scopeQueryerRows) Close() error      { return nil }

func (r *scopeQueryerRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.pos])
	r.pos++
	return nil
}
