// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// scanSingleString runs one query against the fake and returns the single
// string column of its single row, so a test can assert which queued result
// answered without repeating the scan plumbing.
func scanSingleString(t *testing.T, db *sql.DB, query string) string {
	t.Helper()

	rows, err := db.QueryContext(context.Background(), query)
	if err != nil {
		t.Fatalf("QueryContext(%q) error = %v, want nil", query, err)
	}
	defer func() {
		_ = rows.Close()
	}()

	if !rows.Next() {
		t.Fatalf("QueryContext(%q) returned no rows, want one", query)
	}
	var value string
	if err := rows.Scan(&value); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v, want nil", err)
	}
	return value
}

// TestOpenContentReaderTestDBSequencesQueuedResults pins the queue semantics
// the harness is built around: results answer in the order they were queued and
// each one is consumed exactly once. A handler that issues two reads against a
// single fake depends on this to give the two reads different rows, and a fake
// that replayed the first result forever would let a test asserting on the
// second read pass while reading the first.
func TestOpenContentReaderTestDBSequencesQueuedResults(t *testing.T) {
	t.Parallel()

	db := querytestutil.OpenContentReaderTestDB(t, []querytestutil.ContentReaderQueryResult{
		{Columns: []string{"label"}, Rows: [][]driver.Value{{"first"}}},
		{Columns: []string{"label"}, Rows: [][]driver.Value{{"second"}}},
	})

	for i, want := range []string{"first", "second"} {
		got := scanSingleString(t, db, "SELECT label FROM widgets")
		if got != want {
			t.Fatalf("query %d label = %q, want %q", i, got, want)
		}
	}
}

// TestOpenContentReaderTestDBRejectsMissingQueryFragment proves QueryContains
// is an assertion rather than decoration: a handler that stops emitting a
// required SQL fragment must fail the read, not quietly receive the rows.
func TestOpenContentReaderTestDBRejectsMissingQueryFragment(t *testing.T) {
	t.Parallel()

	db := querytestutil.OpenContentReaderTestDB(t, []querytestutil.ContentReaderQueryResult{
		{Columns: []string{"label"}, QueryContains: []string{"FROM widgets", "WHERE repo_id = $1"}},
	})

	_, err := db.QueryContext(context.Background(), "SELECT label FROM widgets")
	if err == nil {
		t.Fatalf("QueryContext() error = nil, want a missing-fragment error")
	}
	if !strings.Contains(err.Error(), `missing fragment "WHERE repo_id = $1"`) {
		t.Fatalf("QueryContext() error = %q, want it to name the absent fragment", err.Error())
	}
}

// TestOpenContentReaderTestDBEnforcesFragmentOrder covers the distinction
// QueryContainsInOrder exists for. Both fragments are present, so a plain
// contains check passes; only the ordered check catches a clause emitted in the
// wrong position, which is how a filter ends up applied after a LIMIT.
func TestOpenContentReaderTestDBEnforcesFragmentOrder(t *testing.T) {
	t.Parallel()

	db := querytestutil.OpenContentReaderTestDB(t, []querytestutil.ContentReaderQueryResult{
		{
			Columns:              []string{"label"},
			QueryContains:        []string{"LIMIT $1", "WHERE repo_id = $2"},
			QueryContainsInOrder: []string{"WHERE repo_id = $2", "LIMIT $1"},
		},
	})

	_, err := db.QueryContext(context.Background(), "SELECT label FROM widgets LIMIT $1 WHERE repo_id = $2")
	if err == nil {
		t.Fatalf("QueryContext() error = nil, want an ordered-fragment error")
	}
	if !strings.Contains(err.Error(), `missing ordered fragment "LIMIT $1"`) {
		t.Fatalf("QueryContext() error = %q, want it to name the out-of-order fragment", err.Error())
	}
}

// TestContentReaderQueryContainsInOrderAcceptsFragmentsInOrder pins the
// positive half of the ordered check, including the rule that matching advances
// past the fragment it consumed. Without that advance a repeated fragment would
// match its own earlier occurrence and the check would pass on a query that
// emits the clauses only once.
func TestContentReaderQueryContainsInOrderAcceptsFragmentsInOrder(t *testing.T) {
	t.Parallel()

	query := "SELECT label FROM widgets WHERE repo_id = $1 ORDER BY label LIMIT $2"

	if err := querytestutil.ContentReaderQueryContainsInOrder(query, []string{
		"FROM widgets", "WHERE repo_id = $1", "ORDER BY label", "LIMIT $2",
	}); err != nil {
		t.Fatalf("ContentReaderQueryContainsInOrder() error = %v, want nil", err)
	}

	err := querytestutil.ContentReaderQueryContainsInOrder(query, []string{"LIMIT $2", "FROM widgets"})
	if err == nil {
		t.Fatalf("ContentReaderQueryContainsInOrder() error = nil, want an out-of-order failure")
	}
	if !strings.Contains(err.Error(), `missing ordered fragment "FROM widgets"`) {
		t.Fatalf("ContentReaderQueryContainsInOrder() error = %q, want it to name the fragment", err.Error())
	}
}

// TestContentReaderCheckArgsComparesByteSliceBindArgsWithoutPanicking proves
// ContentReaderCheckArgs's []byte branch: before the fix, a WantArgs entry
// holding a []byte (the shape package query binds a JSONB parameter with)
// reached the `got == want` fallthrough with both sides typed []byte -- the
// same uncomparable dynamic type on both interface values -- and panicked with
// "comparing uncomparable type []uint8" instead of returning a mismatch error
// (#5764 round-9 P3-3 review follow-up).
func TestContentReaderCheckArgsComparesByteSliceBindArgsWithoutPanicking(t *testing.T) {
	t.Parallel()

	args := []driver.NamedValue{{Ordinal: 1, Value: []byte(`{"a":1}`)}}

	if err := querytestutil.ContentReaderCheckArgs(args, []driver.Value{[]byte(`{"a":1}`)}); err != nil {
		t.Fatalf("ContentReaderCheckArgs() error = %v, want nil for equal []byte bind args", err)
	}

	err := querytestutil.ContentReaderCheckArgs(args, []driver.Value{[]byte(`{"a":2}`)})
	if err == nil {
		t.Fatalf("ContentReaderCheckArgs() error = nil, want a mismatch error for differing []byte bind args")
	}
	if !strings.Contains(err.Error(), "bind arg $1") {
		t.Fatalf("ContentReaderCheckArgs() error = %q, want it to name the mismatched arg", err.Error())
	}
}

// TestContentReaderCheckArgsToleratesIntWrittenForInt64 covers the numeric
// tolerance the helper promises. A test writes a limit as a plain Go int, and
// driver.DefaultParameterConverter delivers an int64, so a strict comparison
// would fail every numeric expectation in the suite.
func TestContentReaderCheckArgsToleratesIntWrittenForInt64(t *testing.T) {
	t.Parallel()

	args := []driver.NamedValue{{Ordinal: 1, Value: int64(25)}}

	if err := querytestutil.ContentReaderCheckArgs(args, []driver.Value{25}); err != nil {
		t.Fatalf("ContentReaderCheckArgs() error = %v, want nil for int want vs int64 got", err)
	}
	if err := querytestutil.ContentReaderCheckArgs(args, []driver.Value{26}); err == nil {
		t.Fatalf("ContentReaderCheckArgs() error = nil, want a mismatch for a different number")
	}
}

// TestContentReaderCheckArgsReportsCountMismatch pins the arity half of the
// check. An extra or dropped bind parameter shifts every later placeholder, so
// the count has to fail loudly rather than compare the args that happen to line
// up.
func TestContentReaderCheckArgsReportsCountMismatch(t *testing.T) {
	t.Parallel()

	args := []driver.NamedValue{{Ordinal: 1, Value: "repo-1"}, {Ordinal: 2, Value: int64(10)}}

	err := querytestutil.ContentReaderCheckArgs(args, []driver.Value{"repo-1"})
	if err == nil {
		t.Fatalf("ContentReaderCheckArgs() error = nil, want an arity mismatch error")
	}
	if !strings.Contains(err.Error(), "got 2 bind args, want 1") {
		t.Fatalf("ContentReaderCheckArgs() error = %q, want it to report both counts", err.Error())
	}

	if err := querytestutil.ContentReaderCheckArgs(args, nil); err != nil {
		t.Fatalf("ContentReaderCheckArgs() error = %v, want nil when want is nil", err)
	}
}

// TestOpenContentReaderTestDBCheckesBindArgsThroughTheDriver proves WantArgs is
// wired into the query path rather than only reachable by calling the helper
// directly. A swapped $1/$2 binding leaves the SQL text byte-identical, so this
// is the only assertion in the harness that catches it.
func TestOpenContentReaderTestDBCheckesBindArgsThroughTheDriver(t *testing.T) {
	t.Parallel()

	db := querytestutil.OpenContentReaderTestDB(t, []querytestutil.ContentReaderQueryResult{
		{Columns: []string{"label"}, WantArgs: []driver.Value{"repo-1", 10}},
	})

	_, err := db.QueryContext(context.Background(), "SELECT label FROM widgets WHERE repo_id = $1 LIMIT $2", 10, "repo-1")
	if err == nil {
		t.Fatalf("QueryContext() error = nil, want a bind-arg mismatch for swapped positions")
	}
	if !strings.Contains(err.Error(), "bind arg $1") {
		t.Fatalf("QueryContext() error = %q, want it to name the mismatched position", err.Error())
	}
}

// TestOpenContentReaderTestDBReturnsQueuedError pins the failure-injection path.
// Handler tests covering a storage error depend on the queued Err surfacing
// from QueryContext instead of an empty row set, which would exercise the
// empty-result branch instead.
func TestOpenContentReaderTestDBReturnsQueuedError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("storage unavailable")
	db := querytestutil.OpenContentReaderTestDB(t, []querytestutil.ContentReaderQueryResult{
		{Columns: []string{"label"}, Err: sentinel},
	})

	_, err := db.QueryContext(context.Background(), "SELECT label FROM widgets")
	if !errors.Is(err, sentinel) {
		t.Fatalf("QueryContext() error = %v, want the queued sentinel", err)
	}
}

// TestOpenContentReaderTestDBFailsAnUnqueuedQuery keeps the harness honest about
// reads nobody declared. Answering an unqueued query with no rows would let a
// handler issue an extra read and still pass.
func TestOpenContentReaderTestDBFailsAnUnqueuedQuery(t *testing.T) {
	t.Parallel()

	db := querytestutil.OpenContentReaderTestDB(t, nil)

	_, err := db.QueryContext(context.Background(), "SELECT label FROM widgets")
	if err == nil {
		t.Fatalf("QueryContext() error = nil, want an unexpected-query error")
	}
	if !strings.Contains(err.Error(), "unexpected query") {
		t.Fatalf("QueryContext() error = %q, want an unexpected-query error", err.Error())
	}
}
