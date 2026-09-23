// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

var (
	_ db.ExecQueryer                    = (*ExecQueryer)(nil)
	_ db.ReadOnlyRepeatableReadBeginner = (*ExecQueryer)(nil)
)

// ExecCall records one ExecContext invocation: the exact query text and
// positional arguments the caller passed.
type ExecCall struct {
	Query string
	Args  []any
}

// QueryCall records one QueryContext invocation: the exact query text and
// positional arguments the caller passed.
type QueryCall struct {
	Query string
	Args  []any
}

// Route inspects a QueryContext call and optionally answers it. It returns
// handled == false to decline, leaving the call to the next Route or, once
// every Route has declined, to the FIFO QueryResponses queue. A Route that
// wants to fail the call sets Rows.FailWith on the response it returns.
//
// Routes let a caller's own tests express query-shape-specific behavior
// (for example, "this query always returns zero rows" or "route this
// argument to a per-key fixture") without ExecQueryer knowing anything about
// that query. See doc.go for why this replaced constant-matching in the
// package this fake was extracted from.
//
// A Route runs while QueryContext holds the ExecQueryer's lock, so it must
// not call back into the same ExecQueryer (ExecContext, QueryContext, or
// BeginReadOnlyRepeatableRead) or it deadlocks; it should be a pure function
// of (query, args).
type Route func(query string, args []any) (rows *Rows, handled bool)

// ExecQueryer is a shared, in-memory stand-in for db.ExecQueryer (and, via
// BeginReadOnlyRepeatableRead in transaction.go, for
// db.ReadOnlyRepeatableReadBeginner). Callers construct it as a struct
// literal, stage the fields their test needs, and pass it wherever
// production code expects a db.ExecQueryer.
//
// All fields are safe to read after the exercised call returns; ExecQueryer
// guards every field with an internal mutex so it can also be driven by
// concurrent callers (see doc.go).
type ExecQueryer struct {
	mu sync.Mutex

	// Execs accumulates every ExecContext call, in call order.
	Execs []ExecCall
	// Queries accumulates every QueryContext call, in call order.
	Queries []QueryCall

	// ExecErrors is consumed FIFO: while non-empty, each ExecContext call
	// pops and returns the first error instead of a result.
	ExecErrors []error
	// ExecResults is consumed FIFO, after ExecErrors is empty: each
	// ExecContext call pops and returns the first result. Once both queues
	// are empty, ExecContext returns Result{} (one row affected).
	ExecResults []sql.Result

	// Routes are tried, in order, before QueryResponses for every
	// QueryContext call. The first Route that reports handled == true wins.
	Routes []Route
	// QueryResponses is consumed FIFO by QueryContext calls no Route
	// handled. A call with an empty queue and no matching Route fails with
	// an error naming the unexpected query.
	QueryResponses []Rows
	// Adapt, when non-nil, is copied onto every Rows this ExecQueryer hands
	// out (from a Route or from QueryResponses) whose own Adapt is nil, so a
	// caller with legacy fixtures can opt every response into a RowAdapter
	// (see LegacyQueueRowAdapter) once instead of setting it on each Rows
	// individually. A Rows with its own Adapt already set keeps it.
	Adapt RowAdapter

	// BeginReadOnlyRepeatableReadCalls counts calls to
	// BeginReadOnlyRepeatableRead.
	BeginReadOnlyRepeatableReadCalls int
	// BeginReadOnlyRepeatableReadErr, when non-nil, is returned by every
	// BeginReadOnlyRepeatableRead call instead of a transaction.
	BeginReadOnlyRepeatableReadErr error
	// TransactionQueryCalls counts QueryContext calls made through a
	// transaction returned by BeginReadOnlyRepeatableRead.
	TransactionQueryCalls int
	// TransactionExecCalls counts ExecContext calls made through a
	// transaction returned by BeginReadOnlyRepeatableRead.
	TransactionExecCalls int
	// TransactionCommitCalls counts Commit calls on transactions this
	// ExecQueryer issued.
	TransactionCommitCalls int
	// TransactionCommitErr, when non-nil, is returned by every Commit call.
	TransactionCommitErr error
	// TransactionRollbackCalls counts Rollback calls on transactions this
	// ExecQueryer issued.
	TransactionRollbackCalls int
	// TransactionRollbackErr, when non-nil, is returned by every Rollback
	// call.
	TransactionRollbackErr error
}

// ExecContext records the call and returns the next staged error or result,
// in that priority order, defaulting to Result{} when neither queue has
// entries left.
func (f *ExecQueryer) ExecContext(
	_ context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Execs = append(f.Execs, ExecCall{Query: query, Args: args})

	if len(f.ExecErrors) > 0 {
		err := f.ExecErrors[0]
		f.ExecErrors = f.ExecErrors[1:]
		return nil, err
	}
	if len(f.ExecResults) > 0 {
		result := f.ExecResults[0]
		f.ExecResults = f.ExecResults[1:]
		return result, nil
	}
	return Result{}, nil
}

// QueryContext records the call, then answers it from the first matching
// Route or, failing that, the next entry in QueryResponses. A call with no
// matching Route and an empty QueryResponses queue fails with an error
// naming the unexpected query, so a caller notices a fixture gap
// immediately instead of silently reading zero rows.
func (f *ExecQueryer) QueryContext(
	_ context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Queries = append(f.Queries, QueryCall{Query: query, Args: args})

	for _, route := range f.Routes {
		response, handled := route(query, args)
		if !handled {
			continue
		}
		if response == nil {
			return nil, fmt.Errorf("fake: route handled query but returned nil rows: %s", query)
		}
		// Copy the route's Rows before handing it out: routes are ordinary
		// functions and may return the same *Rows for repeated matching
		// calls (a package-level fixture, say), so returning it as-is would
		// let one call's Next/Scan advance shared state that the next call
		// then sees, matching neither the caller's fixture nor the FIFO
		// path below (which already hands out a fresh copy).
		copied := *response
		if copied.FailWith != nil {
			return nil, copied.FailWith
		}
		if copied.Adapt == nil {
			copied.Adapt = f.Adapt
		}
		return &copied, nil
	}

	if len(f.QueryResponses) == 0 {
		return nil, fmt.Errorf("fake: unexpected query: %s", query)
	}
	response := f.QueryResponses[0]
	f.QueryResponses = f.QueryResponses[1:]
	if response.FailWith != nil {
		return nil, response.FailWith
	}
	if response.Adapt == nil {
		response.Adapt = f.Adapt
	}
	return &response, nil
}
