// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

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

// This file provides a minimal generic fake database/sql driver for #5167
// store-level access-scoping tests whose store interfaces (e.g.
// kubernetesCorrelationQueryer, observabilityCoverageCorrelationQueryer)
// demand a concrete *sql.Rows return, which cannot be constructed without a
// real database/sql round trip. It mirrors the narrower
// contentReaderDriver/recordingContentReaderConn pattern
// (content_reader_driver_test.go, content_reader_cross_repo_test.go) but is
// not tied to ContentReader, so it is reusable across any QueryContext-based
// store in this package.

// scopeQueryerRecorder captures every query issued through
// openScopeQueryerTestDB, so a test can assert both the dispatched SQL text
// (the access-scoping predicate) and the bound argument values (the granted
// repository/scope id arrays), matching the #5137 ReadLiveActivity test
// precedent (internal/storage/postgres/status_operations_test.go).
type scopeQueryerRecorder struct {
	mu      sync.Mutex
	queries []string
	args    [][]driver.Value
}

func (r *scopeQueryerRecorder) record(query string, args []driver.NamedValue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, query)
	recorded := make([]driver.Value, 0, len(args))
	for _, arg := range args {
		recorded = append(recorded, arg.Value)
	}
	r.args = append(r.args, recorded)
}

func (r *scopeQueryerRecorder) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.queries)
}

var scopeQueryerDriverSeq uint64

// openScopeQueryerTestDB opens a *sql.DB backed by a fake driver that records
// every dispatched query/args pair and always returns the given columns/rows
// (canned, not real SQL evaluation -- matching every other fake driver in
// this package's test suite; live WHERE-clause execution correctness is a
// live/integration-test concern, not a unit-test one).
func openScopeQueryerTestDB(t *testing.T, columns []string, rows [][]driver.Value) (*sql.DB, *scopeQueryerRecorder) {
	t.Helper()

	name := fmt.Sprintf("scope-queryer-test-%d", atomic.AddUint64(&scopeQueryerDriverSeq, 1))
	recorder := &scopeQueryerRecorder{}
	sql.Register(name, &scopeQueryerDriver{recorder: recorder, columns: columns, rows: rows})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, recorder
}

type scopeQueryerDriver struct {
	recorder *scopeQueryerRecorder
	columns  []string
	rows     [][]driver.Value
}

func (d *scopeQueryerDriver) Open(string) (driver.Conn, error) {
	return &scopeQueryerConn{recorder: d.recorder, columns: d.columns, rows: d.rows}, nil
}

type scopeQueryerConn struct {
	recorder *scopeQueryerRecorder
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
