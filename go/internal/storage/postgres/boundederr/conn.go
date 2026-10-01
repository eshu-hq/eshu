// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"database/sql/driver"
)

// conn forwards every driver.Conn call to the pgx connection and bounds the
// error it returns.
type conn struct {
	inner pgxConn
	obs   *observer
}

var (
	_ driver.Conn               = (*conn)(nil)
	_ driver.ConnBeginTx        = (*conn)(nil)
	_ driver.ConnPrepareContext = (*conn)(nil)
	_ driver.ExecerContext      = (*conn)(nil)
	_ driver.QueryerContext     = (*conn)(nil)
	_ driver.Pinger             = (*conn)(nil)
	_ driver.NamedValueChecker  = (*conn)(nil)
	_ driver.SessionResetter    = (*conn)(nil)
)

// Prepare is the context-free prepare database/sql falls back to.
func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// PrepareContext prepares query and wraps the statement.
func (c *conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	stmt, err := c.inner.PrepareContext(ctx, query)
	if err != nil {
		return nil, c.obs.bound(ctx, opPrepare, query, err)
	}
	return &statement{inner: stmt, query: query, obs: c.obs}, nil
}

// Close closes the pgx connection. A close error carries no request data and is
// bounded like any other.
func (c *conn) Close() error {
	return c.obs.bound(context.Background(), opClose, "", c.inner.Close())
}

// Begin starts a transaction with default options.
func (c *conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx starts a transaction and wraps it.
func (c *conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.inner.BeginTx(ctx, opts)
	if err != nil {
		return nil, c.obs.bound(ctx, opBegin, "", err)
	}
	return &transaction{inner: tx, obs: c.obs}, nil
}

// ExecContext runs a statement that returns no rows.
func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	result, err := c.inner.ExecContext(ctx, query, args)
	if err != nil {
		return nil, c.obs.bound(ctx, opExec, query, err)
	}
	return result, nil
}

// QueryContext runs a statement that returns rows and wraps the result set.
func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rows, err := c.inner.QueryContext(ctx, query, args)
	if err != nil {
		return nil, c.obs.bound(ctx, opQuery, query, err)
	}
	return newRows(rows, query, c.obs), nil
}

// Ping checks the connection.
func (c *conn) Ping(ctx context.Context) error {
	return c.obs.bound(ctx, opPing, "", c.inner.Ping(ctx))
}

// CheckNamedValue forwards pgx's argument conversion. database/sql branches on
// driver.ErrSkip and driver.ErrRemoveArgument here, which bound leaves alone.
func (c *conn) CheckNamedValue(value *driver.NamedValue) error {
	return c.obs.bound(context.Background(), opQuery, "", c.inner.CheckNamedValue(value))
}

// ResetSession runs before a pooled connection is reused. driver.ErrBadConn
// from it discards the connection and is left alone.
func (c *conn) ResetSession(ctx context.Context) error {
	return c.obs.bound(ctx, opReset, "", c.inner.ResetSession(ctx))
}

// statement wraps a prepared pgx statement.
type statement struct {
	inner driver.Stmt
	query string
	obs   *observer
}

var (
	_ driver.Stmt             = (*statement)(nil)
	_ driver.StmtExecContext  = (*statement)(nil)
	_ driver.StmtQueryContext = (*statement)(nil)
)

// Close releases the prepared statement.
func (s *statement) Close() error {
	return s.obs.bound(context.Background(), opClose, s.query, s.inner.Close())
}

// NumInput reports the placeholder count.
func (s *statement) NumInput() int { return s.inner.NumInput() }

// Exec is the context-free exec database/sql falls back to.
func (s *statement) Exec(args []driver.Value) (driver.Result, error) {
	result, err := s.inner.Exec(args) //nolint:staticcheck // forwarding the legacy driver.Stmt method
	if err != nil {
		return nil, s.obs.bound(context.Background(), opExec, s.query, err)
	}
	return result, nil
}

// Query is the context-free query database/sql falls back to.
func (s *statement) Query(args []driver.Value) (driver.Rows, error) {
	rows, err := s.inner.Query(args) //nolint:staticcheck // forwarding the legacy driver.Stmt method
	if err != nil {
		return nil, s.obs.bound(context.Background(), opQuery, s.query, err)
	}
	return newRows(rows, s.query, s.obs), nil
}

// ExecContext runs the prepared statement without returning rows.
func (s *statement) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	execer, ok := s.inner.(driver.StmtExecContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	result, err := execer.ExecContext(ctx, args)
	if err != nil {
		return nil, s.obs.bound(ctx, opExec, s.query, err)
	}
	return result, nil
}

// QueryContext runs the prepared statement and wraps the result set.
func (s *statement) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	queryer, ok := s.inner.(driver.StmtQueryContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := queryer.QueryContext(ctx, args)
	if err != nil {
		return nil, s.obs.bound(ctx, opQuery, s.query, err)
	}
	return newRows(rows, s.query, s.obs), nil
}

// transaction wraps a pgx transaction.
type transaction struct {
	inner driver.Tx
	obs   *observer
}

var _ driver.Tx = (*transaction)(nil)

// Commit commits the transaction.
func (t *transaction) Commit() error {
	return t.obs.bound(context.Background(), opCommit, "", t.inner.Commit())
}

// Rollback rolls the transaction back.
func (t *transaction) Rollback() error {
	return t.obs.bound(context.Background(), opRollback, "", t.inner.Rollback())
}
