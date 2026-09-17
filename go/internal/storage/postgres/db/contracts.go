// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package db

import (
	"context"
	"database/sql"
)

// Rows is the small read-only row cursor surface shared by storage adapters.
type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

// Queryer is the small read-only SQL adapter shared by storage adapters.
type Queryer interface {
	QueryContext(context.Context, string, ...any) (Rows, error)
}

// Executor is the narrow adapter surface required to apply SQL statements
// against a SQL connection or transaction.
type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// ExecQueryer combines read and write access for storage adapters.
type ExecQueryer interface {
	Queryer
	Executor
}

// Transaction is the narrow transactional surface required by durable commit
// boundaries in storage adapters.
type Transaction interface {
	ExecQueryer
	Commit() error
	Rollback() error
}

// Beginner constructs transactions for storage adapters that need atomic writes.
type Beginner interface {
	Begin(context.Context) (Transaction, error)
}

// ReadOnlyRepeatableReadBeginner constructs a read-only transaction whose
// statements share one repeatable-read snapshot.
type ReadOnlyRepeatableReadBeginner interface {
	BeginReadOnlyRepeatableRead(context.Context) (Transaction, error)
}
