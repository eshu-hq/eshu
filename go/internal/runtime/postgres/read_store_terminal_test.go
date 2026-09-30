// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
)

func TestReadTransactionFinishedCauseRetainsDriverAndCloseErrors(t *testing.T) {
	driverErr := errors.New("seeded rollback driver error")
	closeErr := errors.New("seeded connection close error")
	tx := &readTransaction{err: errors.Join(driverErr, closeErr)}
	tx.once.Do(func() {}) // The cancellation callback has already finished.
	got := tx.finish(false)
	if !errors.Is(got, sql.ErrTxDone) || !errors.Is(got, driverErr) || !errors.Is(got, closeErr) {
		t.Fatalf("finished error = %v, want ErrTxDone and both terminal causes", got)
	}
	clean := &readTransaction{}
	clean.once.Do(func() {})
	if got := clean.finish(false); got != sql.ErrTxDone {
		t.Fatalf("clean finished error = %v, want ErrTxDone", got)
	}
}

type terminalErrorConnector struct{ rollbackErr error }

func (c terminalErrorConnector) Connect(context.Context) (driver.Conn, error) {
	return terminalErrorConn(c), nil
}
func (terminalErrorConnector) Driver() driver.Driver { return terminalErrorDriver{} }

type terminalErrorDriver struct{}

func (terminalErrorDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type terminalErrorConn struct{ rollbackErr error }

func (terminalErrorConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (terminalErrorConn) Close() error                        { return nil }
func (c terminalErrorConn) Begin() (driver.Tx, error) {
	return terminalErrorTx(c), nil
}

type terminalErrorTx struct{ rollbackErr error }

func (terminalErrorTx) Commit() error     { return nil }
func (t terminalErrorTx) Rollback() error { return t.rollbackErr }

func TestReadTransactionCanceledTerminalRetainsActualDriverRollbackError(t *testing.T) {
	rollbackErr := errors.New("driver rollback failed")
	pool := sql.OpenDB(terminalErrorConnector{rollbackErr: rollbackErr})
	defer pool.Close()
	conn, err := pool.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	sqlTx, err := conn.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	owned := &readTransaction{tx: sqlTx, conn: conn, stop: func() bool { return true }}
	if err := owned.Rollback(); !errors.Is(err, rollbackErr) {
		t.Fatalf("first rollback=%v, want driver failure", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := owned.Commit()
			if !errors.Is(got, sql.ErrTxDone) || !errors.Is(got, rollbackErr) {
				t.Errorf("finished commit=%v, want ErrTxDone and rollback cause", got)
			}
		}()
	}
	wg.Wait()
}
