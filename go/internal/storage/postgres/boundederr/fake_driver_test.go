// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"log/slog"
	"testing"
)

// script is the behavior a fake pgx connection plays back.
type script struct {
	execErr   error
	queryErr  error
	commitErr error
	// Per-seam failures the connection-level tests drive directly.
	pingErr, prepareErr, resetErr, checkErr, closeErr, beginErr, rollbackErr error
	rowsErr                                                                  error // returned by Next after rowsBefore rows
	rowsBefore                                                               int
	// badConnOnce makes the first connection's ExecContext answer
	// driver.ErrBadConn, so database/sql must retry on a fresh connection.
	badConnOnce bool
	// prepareStmt makes PrepareContext succeed with a fake statement whose
	// methods fail with the stmt*Err values below.
	prepareStmt                             bool
	stmtExecErr, stmtQueryErr, stmtCloseErr error
}

type fakeConnector struct {
	script   *script
	connects int
}

func (f *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	f.connects++
	return &fakeConn{script: f.script, ordinal: f.connects}, nil
}
func (f *fakeConnector) Driver() driver.Driver { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type fakeConn struct {
	script  *script
	ordinal int
}

var _ pgxConn = (*fakeConn)(nil)

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("prepare unused") }
func (c *fakeConn) PrepareContext(context.Context, string) (driver.Stmt, error) {
	if c.script.prepareErr != nil {
		return nil, c.script.prepareErr
	}
	if c.script.prepareStmt {
		return &fakeStmt{script: c.script}, nil
	}
	return nil, errors.New("prepare unused")
}
func (c *fakeConn) Close() error              { return c.script.closeErr }
func (c *fakeConn) Begin() (driver.Tx, error) { return &fakeTx{script: c.script}, c.script.beginErr }

func (c *fakeConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if c.script.beginErr != nil {
		return nil, c.script.beginErr
	}
	return &fakeTx{script: c.script}, nil
}
func (c *fakeConn) Ping(context.Context) error               { return c.script.pingErr }
func (c *fakeConn) CheckNamedValue(*driver.NamedValue) error { return c.script.checkErr }
func (c *fakeConn) ResetSession(context.Context) error       { return c.script.resetErr }

func (c *fakeConn) ExecContext(context.Context, string, []driver.NamedValue) (driver.Result, error) {
	if c.script.badConnOnce && c.ordinal == 1 {
		return nil, driver.ErrBadConn
	}
	if c.script.execErr != nil {
		return nil, c.script.execErr
	}
	return driver.RowsAffected(1), nil
}

func (c *fakeConn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	if c.script.queryErr != nil {
		return nil, c.script.queryErr
	}
	return &fakeRows{script: c.script}, nil
}

type fakeTx struct{ script *script }

func (t *fakeTx) Commit() error   { return t.script.commitErr }
func (t *fakeTx) Rollback() error { return t.script.rollbackErr }

type fakeRows struct {
	script *script
	served int
}

func (r *fakeRows) Columns() []string                     { return []string{"name"} }
func (r *fakeRows) ColumnTypeDatabaseTypeName(int) string { return "TEXT" }

// Close returns the error Next already reported, as pgx's Rows.Close does
// (`r.rows.Close(); return r.rows.Err()`), so database/sql's close-after-error
// reaches the wrapper with the same raw error a second time.
func (r *fakeRows) Close() error {
	if r.served >= r.script.rowsBefore {
		return r.script.rowsErr
	}
	return nil
}

func (r *fakeRows) Next(dest []driver.Value) error {
	if r.served >= r.script.rowsBefore {
		if r.script.rowsErr != nil {
			return r.script.rowsErr
		}
		return io.EOF
	}
	r.served++
	dest[0] = "row"
	return nil
}

// openFake opens a *sql.DB over the fake connector and captures the operator log.
func openFake(t *testing.T, s *script) (*sql.DB, *fakeConnector, *bytes.Buffer) {
	t.Helper()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	fake := &fakeConnector{script: s}
	db := sql.OpenDB(NewConnector(fake, WithLogger(logger)))
	t.Cleanup(func() { _ = db.Close() })
	return db, fake, &logs
}

// fakeStmt is a prepared statement whose every execution method fails with the
// error the script names, so each statement seam can be driven to failure.
type fakeStmt struct{ script *script }

var (
	_ driver.Stmt             = (*fakeStmt)(nil)
	_ driver.StmtExecContext  = (*fakeStmt)(nil)
	_ driver.StmtQueryContext = (*fakeStmt)(nil)
)

func (s *fakeStmt) Close() error  { return s.script.stmtCloseErr }
func (s *fakeStmt) NumInput() int { return -1 }
func (s *fakeStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, s.script.stmtExecErr
}

func (s *fakeStmt) Query([]driver.Value) (driver.Rows, error) {
	return nil, s.script.stmtQueryErr
}

func (s *fakeStmt) ExecContext(context.Context, []driver.NamedValue) (driver.Result, error) {
	return nil, s.script.stmtExecErr
}

func (s *fakeStmt) QueryContext(context.Context, []driver.NamedValue) (driver.Rows, error) {
	return nil, s.script.stmtQueryErr
}
