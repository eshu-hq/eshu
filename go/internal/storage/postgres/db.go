package postgres

import (
	"context"
	"database/sql"
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
