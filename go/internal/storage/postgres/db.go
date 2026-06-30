// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

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

// SQLDB adapts a *sql.DB into the combined storage interface surface.
type SQLDB struct {
	DB *sql.DB
}

// QueryContext implements Queryer against a sql.DB.
func (db SQLDB) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return db.DB.QueryContext(ctx, query, args...)
}

// ExecContext implements Executor against a sql.DB.
func (db SQLDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return db.DB.ExecContext(ctx, query, args...)
}

func (db SQLDB) execContextWithLockTimeout(
	ctx context.Context,
	query string,
	lockTimeout time.Duration,
) (sql.Result, error) {
	if db.DB == nil {
		return nil, fmt.Errorf("postgres SQLDB requires a database handle")
	}
	if lockTimeout <= 0 {
		return db.ExecContext(ctx, query)
	}
	conn, err := db.DB.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open schema connection: %w", err)
	}
	closeConn := true
	defer func() {
		if closeConn {
			_ = conn.Close()
		}
	}()

	if _, err := conn.ExecContext(ctx, "SELECT set_config('lock_timeout', $1, false)", lockTimeout.String()); err != nil {
		return nil, fmt.Errorf("set schema lock timeout: %w", err)
	}
	result, execErr := conn.ExecContext(ctx, query)
	_, resetErr := conn.ExecContext(ctx, "SELECT set_config('lock_timeout', '0', false)")
	if resetErr != nil {
		resetErr = fmt.Errorf("reset schema lock timeout: %w", resetErr)
	}
	closeErr := conn.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close schema connection: %w", closeErr)
	}
	closeConn = false
	return result, errors.Join(execErr, resetErr, closeErr)
}

// Begin opens a transaction against the wrapped database.
func (db SQLDB) Begin(ctx context.Context) (Transaction, error) {
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}

	return SQLTx{Tx: tx}, nil
}

// SQLTx adapts a *sql.Tx into the storage transaction surface.
type SQLTx struct {
	Tx *sql.Tx
}

// QueryContext implements Queryer against a sql.Tx.
func (tx SQLTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.Tx.QueryContext(ctx, query, args...)
}

// ExecContext implements Executor against a sql.Tx.
func (tx SQLTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.Tx.ExecContext(ctx, query, args...)
}

// Commit commits the wrapped transaction.
func (tx SQLTx) Commit() error {
	return tx.Tx.Commit()
}

// Rollback rolls back the wrapped transaction.
func (tx SQLTx) Rollback() error {
	return tx.Tx.Rollback()
}
