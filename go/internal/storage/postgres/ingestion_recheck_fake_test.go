// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The destructive-successor guard re-reads its sizing inputs on the commit
// transaction after the scope row lock (#8750 window). The positional fakes
// below model a scope with no concurrent writer: the transaction sees the
// same committed state the pre-tx read saw, so the recheck finds no change.
// A test that needs an interleave uses guardRoutingDB, which stages in-tx
// answers by query identity.

// guardRecheckQueries are the reads the recheck issues on the transaction.
var guardRecheckQueries = map[string]bool{
	unchangedGenerationCandidateQuery:   true,
	successorGuardActiveGenerationQuery: true,
}

// rememberRecheckRead keeps the outer connection's answer to a recheck read
// so the next transaction can mirror it.
func (f *fakeTransactionalDB) rememberRecheckRead(query string, rows queueFakeRows) {
	if !guardRecheckQueries[query] {
		return
	}
	if f.recheckRows == nil {
		f.recheckRows = map[string]queueFakeRows{}
	}
	f.recheckRows[query] = queueFakeRows{rows: rows.rows, err: rows.err}
}

// withRecheckRows hands the remembered pre-tx answers to tx.
func (f *fakeTransactionalDB) withRecheckRows(tx *fakeTx) *fakeTx {
	if tx != nil && f.recheckRows != nil {
		tx.recheckRows = f.recheckRows
	}
	return tx
}

// recheckRead answers a recheck read by identity, ahead of the positional
// responses so it never consumes one. An unstaged recheck read fails like
// any other unexpected query.
func (f *fakeTx) recheckRead(query string) (db.Rows, bool, error) {
	if !guardRecheckQueries[query] {
		return nil, false, nil
	}
	rows, ok := f.recheckRows[query]
	if !ok {
		return nil, true, errors.New("unexpected recheck query in transaction: no pre-tx read to mirror")
	}
	if rows.err != nil {
		return nil, true, rows.err
	}
	return &queueFakeRows{rows: rows.rows}, true, nil
}
