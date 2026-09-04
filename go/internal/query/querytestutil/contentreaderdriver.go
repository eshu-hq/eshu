// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
)

// ContentReaderQueryResult is one queued answer for the fake SQL driver behind
// OpenContentReaderTestDB, plus the assertions to run against the query that
// consumes it.
//
// The fields are exported because a struct literal built in another package can
// only set exported fields, and the whole point of this package is that handler
// families keep their tests when they move out of package query (#6060). Package
// query keeps an unexported adapter with the original lowercase field names and
// converts, so its existing test files are untouched.
type ContentReaderQueryResult struct {
	// Columns are the column names the answer reports. They also decide whether
	// this queued result answers one of the incidental reads that otherwise get
	// a default empty answer -- see contentReaderDefaultRows.
	Columns []string
	// Rows are the driver values the answer yields, one slice per row.
	Rows [][]driver.Value
	// Err, when non-nil, fails the query instead of returning rows. Handler
	// tests covering a storage-failure path depend on seeing it.
	Err error
	// QueryContains asserts each fragment appears somewhere in the SQL text.
	QueryContains []string
	// QueryContainsInOrder asserts each fragment appears after the previous
	// one, which is what catches a clause emitted in the wrong position.
	QueryContainsInOrder []string
	// WantArgs, when non-nil, asserts the exact positional bind values
	// QueryContext receives, in order, including type (e.g. int64 vs string
	// after driver.DefaultParameterConverter runs). Catches an argument-order
	// swap that leaves the query TEXT byte-identical -- QueryContains and
	// QueryContainsInOrder only inspect the SQL string, so a swapped
	// $1/$2/$3 binding is invisible to both (#5764 round-8 P2-2 review
	// follow-up).
	WantArgs []driver.Value
}

// OpenContentReaderTestDB returns a *sql.DB backed by a fake driver that answers
// from results, in order, and closes itself when the test ends.
//
// Each call registers its own driver under a unique name, so tests running in
// parallel do not share a queue. The pool is capped at one connection because
// the queue lives on the connection: a second connection would start from the
// full result list and answer a later query with the first result.
func OpenContentReaderTestDB(t *testing.T, results []ContentReaderQueryResult) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("content-reader-test-%d", atomic.AddUint64(&contentReaderDriverSeq, 1))
	sql.Register(name, &contentReaderDriver{results: results})

	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

// contentReaderDriverSeq names each registered fake driver uniquely.
// database/sql panics on a duplicate driver name, and every test in a package
// opens its own.
var contentReaderDriverSeq uint64

// contentReaderDriver hands each connection its own copy of the queued results.
type contentReaderDriver struct {
	results []ContentReaderQueryResult
}

// Open returns a connection carrying a private copy of the queued results, so
// consuming one on this connection cannot disturb another.
func (d *contentReaderDriver) Open(string) (driver.Conn, error) {
	return &contentReaderConn{results: append([]ContentReaderQueryResult(nil), d.results...)}, nil
}

// contentReaderConn answers queries from a queue it consumes as it goes.
type contentReaderConn struct {
	results []ContentReaderQueryResult
}

// Prepare is unimplemented: the harness answers through QueryContext only, and
// a handler that prepares a statement is outside what these tests cover.
func (c *contentReaderConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

// Close releases nothing; the queue is plain memory.
func (c *contentReaderConn) Close() error {
	return nil
}

// Begin is unimplemented for the same reason as Prepare.
func (c *contentReaderConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

// QueryContext answers one read.
//
// An incidental read the test did not queue a result for -- a readiness probe,
// a rollup a handler issues on the way to the query under test -- is answered
// with an empty row set of the right shape and leaves the queue untouched.
// Consuming the queue for those would misalign every later expectation.
//
// Everything else takes the head of the queue, runs that result's assertions
// against the SQL text and the bind values, and answers with its rows. An empty
// queue is an error rather than an empty answer, so a handler issuing a read
// nobody declared fails instead of passing.
func (c *contentReaderConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if rows := contentReaderDefaultRows(query, c.results); rows != nil {
		return rows, nil
	}
	if len(c.results) == 0 {
		return nil, fmt.Errorf("unexpected query")
	}
	result := c.results[0]
	c.results = c.results[1:]
	for _, fragment := range result.QueryContains {
		if !strings.Contains(query, fragment) {
			return nil, fmt.Errorf("query missing fragment %q", fragment)
		}
	}
	if err := ContentReaderQueryContainsInOrder(query, result.QueryContainsInOrder); err != nil {
		return nil, err
	}
	if err := ContentReaderCheckArgs(args, result.WantArgs); err != nil {
		return nil, err
	}
	if result.Err != nil {
		return nil, result.Err
	}
	return &contentReaderRows{columns: result.Columns, rows: result.Rows}, nil
}

// contentReaderRows walks a canned row set once.
type contentReaderRows struct {
	columns []string
	rows    [][]driver.Value
	index   int
}

// Columns reports the answer's column names.
func (r *contentReaderRows) Columns() []string {
	return r.columns
}

// Close releases nothing; the rows are plain memory.
func (r *contentReaderRows) Close() error {
	return nil
}

// Next copies the row at the cursor into dest and advances, reporting io.EOF
// once the canned rows are exhausted.
func (r *contentReaderRows) Next(dest []driver.Value) error {
	if r.index >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.index])
	r.index++
	return nil
}
