// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake

import (
	"context"
	"database/sql"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// BeginReadOnlyRepeatableRead implements db.ReadOnlyRepeatableReadBeginner.
// It records the call and, absent an injected BeginReadOnlyRepeatableReadErr,
// returns a Transaction that delegates every call back to this ExecQueryer
// so a single fixture (Routes, QueryResponses, ExecResults) answers both
// direct and transactional calls the same way.
func (f *ExecQueryer) BeginReadOnlyRepeatableRead(
	_ context.Context,
) (db.Transaction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.BeginReadOnlyRepeatableReadCalls++
	if f.BeginReadOnlyRepeatableReadErr != nil {
		return nil, f.BeginReadOnlyRepeatableReadErr
	}
	return &Transaction{parent: f}, nil
}

// Transaction is the db.Transaction ExecQueryer.BeginReadOnlyRepeatableRead
// returns. It counts its own calls separately from the parent's Execs and
// Queries logs, then delegates to the parent so the parent's staged
// responses and error injections apply uniformly whether or not the caller
// went through a transaction.
type Transaction struct {
	parent *ExecQueryer
}

// QueryContext counts the call on the parent ExecQueryer and delegates to
// its QueryContext.
func (tx *Transaction) QueryContext(
	ctx context.Context,
	query string,
	args ...any,
) (db.Rows, error) {
	tx.parent.mu.Lock()
	tx.parent.TransactionQueryCalls++
	tx.parent.mu.Unlock()
	return tx.parent.QueryContext(ctx, query, args...)
}

// ExecContext counts the call on the parent ExecQueryer and delegates to its
// ExecContext.
func (tx *Transaction) ExecContext(
	ctx context.Context,
	query string,
	args ...any,
) (sql.Result, error) {
	tx.parent.mu.Lock()
	tx.parent.TransactionExecCalls++
	tx.parent.mu.Unlock()
	return tx.parent.ExecContext(ctx, query, args...)
}

// Commit counts the call and returns the parent's injected
// TransactionCommitErr, if any.
func (tx *Transaction) Commit() error {
	tx.parent.mu.Lock()
	defer tx.parent.mu.Unlock()
	tx.parent.TransactionCommitCalls++
	return tx.parent.TransactionCommitErr
}

// Rollback counts the call and returns the parent's injected
// TransactionRollbackErr, if any.
func (tx *Transaction) Rollback() error {
	tx.parent.mu.Lock()
	defer tx.parent.mu.Unlock()
	tx.parent.TransactionRollbackCalls++
	return tx.parent.TransactionRollbackErr
}
