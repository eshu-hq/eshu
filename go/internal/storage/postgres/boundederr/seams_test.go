// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestEveryConnectionSeamBoundsAndLogsOnce drives each driver call that is not
// a query or an exec to failure and requires a bounded error plus exactly one
// postgres.store.error record naming the operation. A seam that dropped its
// bound would hand the raw driver text to database/sql, and a seam with the
// wrong operation label would mislead the operator reading the record.
func TestEveryConnectionSeamBoundsAndLogsOnce(t *testing.T) {
	t.Parallel()

	cause := &pgconn.PgError{Severity: "ERROR", Code: "XX000", Message: `relation "tenant_secrets" is corrupt`}
	tests := []struct {
		name      string
		script    script
		operation string
		call      func(c *conn) error
	}{
		{"ping", script{pingErr: cause}, opPing, func(c *conn) error { return c.Ping(context.Background()) }},
		{"prepare", script{prepareErr: cause}, opPrepare, func(c *conn) error {
			_, err := c.PrepareContext(context.Background(), "SELECT 1")
			return err
		}},
		{"reset session", script{resetErr: cause}, opReset, func(c *conn) error { return c.ResetSession(context.Background()) }},
		{"check named value", script{checkErr: cause}, opQuery, func(c *conn) error { return c.CheckNamedValue(&driver.NamedValue{}) }},
		{"close", script{closeErr: cause}, opClose, func(c *conn) error { return c.Close() }},
		{"begin", script{beginErr: cause}, opBegin, func(c *conn) error {
			_, err := c.BeginTx(context.Background(), driver.TxOptions{})
			return err
		}},
		{"rollback", script{rollbackErr: cause}, opRollback, func(c *conn) error {
			tx, err := c.BeginTx(context.Background(), driver.TxOptions{})
			if err != nil {
				return err
			}
			return tx.Rollback()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			obs := &observer{logger: slog.New(slog.NewTextHandler(&logs, nil))}
			s := tt.script
			err := tt.call(&conn{inner: &fakeConn{script: &s, ordinal: 1}, obs: obs})
			requireBounded(t, err, KindFailed, "tenant_secrets")
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatal("errors.As(PgError) = false, want the cause reachable")
			}
			if got := strings.Count(logs.String(), "event_name=postgres.store.error"); got != 1 {
				t.Fatalf("logged %d records, want exactly 1:\n%s", got, logs.String())
			}
			if want := "postgres_store.operation=" + tt.operation; !strings.Contains(logs.String(), want) {
				t.Fatalf("record lacks %q:\n%s", want, logs.String())
			}
		})
	}
}

// TestEveryStatementSeamBoundsAndLogsOnce drives each method of a prepared
// statement to failure and requires a bounded error plus exactly one
// postgres.store.error record carrying the statement text and the right
// operation. A statement method that dropped its bound would hand raw driver
// text to database/sql.
func TestEveryStatementSeamBoundsAndLogsOnce(t *testing.T) {
	t.Parallel()

	cause := &pgconn.PgError{Severity: "ERROR", Code: "XX000", Message: `relation "tenant_secrets" is corrupt`}
	const query = "SELECT name FROM tenant_secrets WHERE id = $1"
	tests := []struct {
		name      string
		script    script
		operation string
		call      func(s driver.Stmt) error
	}{
		{"close", script{stmtCloseErr: cause}, opClose, func(s driver.Stmt) error { return s.Close() }},
		{"exec", script{stmtExecErr: cause}, opExec, func(s driver.Stmt) error {
			_, err := s.Exec(nil) //nolint:staticcheck // the legacy driver.Stmt method
			return err
		}},
		{"query", script{stmtQueryErr: cause}, opQuery, func(s driver.Stmt) error {
			_, err := s.Query(nil) //nolint:staticcheck // the legacy driver.Stmt method
			return err
		}},
		{"exec context", script{stmtExecErr: cause}, opExec, func(s driver.Stmt) error {
			_, err := s.(driver.StmtExecContext).ExecContext(context.Background(), nil)
			return err
		}},
		{"query context", script{stmtQueryErr: cause}, opQuery, func(s driver.Stmt) error {
			_, err := s.(driver.StmtQueryContext).QueryContext(context.Background(), nil)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			obs := &observer{logger: slog.New(slog.NewTextHandler(&logs, nil))}
			s := tt.script
			s.prepareStmt = true
			stmt, err := (&conn{inner: &fakeConn{script: &s, ordinal: 1}, obs: obs}).PrepareContext(context.Background(), query)
			if err != nil {
				t.Fatalf("PrepareContext() error = %v, want a statement", err)
			}
			err = tt.call(stmt)
			requireBounded(t, err, KindFailed, "tenant_secrets")
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatal("errors.As(PgError) = false, want the cause reachable")
			}
			if got := strings.Count(logs.String(), "event_name=postgres.store.error"); got != 1 {
				t.Fatalf("logged %d records, want exactly 1:\n%s", got, logs.String())
			}
			for _, want := range []string{"postgres_store.operation=" + tt.operation, "SELECT name FROM tenant_secrets WHERE id = $1"} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("record lacks %q:\n%s", want, logs.String())
				}
			}
		})
	}
}
