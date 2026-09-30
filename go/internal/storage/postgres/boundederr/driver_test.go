// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestRowsEndOfResultSetAndColumnTypesSurviveTheWrapper(t *testing.T) {
	t.Parallel()

	db, _, logs := openFake(t, &script{rowsBefore: 2})
	rows, err := db.QueryContext(context.Background(), "SELECT name FROM t")
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	defer func() { _ = rows.Close() }()
	types, err := rows.ColumnTypes()
	if err != nil || len(types) != 1 || types[0].DatabaseTypeName() != "TEXT" {
		t.Fatalf("ColumnTypes() = %v, %v; want the forwarded TEXT type", types, err)
	}
	count := 0
	for rows.Next() {
		count++
	}
	if count != 2 {
		t.Fatalf("rows = %d, want 2", count)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err() = %v, want nil: io.EOF must end the set, not become an error", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("a clean read logged: %s", logs.String())
	}
}

func TestServerErrorMidResultSetIsBoundedAndLogged(t *testing.T) {
	t.Parallel()

	cause := &pgconn.PgError{Severity: "ERROR", Code: "XX000", Message: `relation "tenant_secrets" is corrupt`}
	db, _, logs := openFake(t, &script{rowsBefore: 1, rowsErr: cause})
	rows, err := db.QueryContext(context.Background(), "SELECT name FROM tenant_secrets WHERE id = $1", 7)
	if err != nil {
		t.Fatalf("QueryContext() error = %v, want nil", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
	}
	err = rows.Err()
	requireBounded(t, err, KindFailed, "tenant_secrets")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "XX000" {
		t.Fatalf("errors.As(PgError) = %v, want the server error reachable", pgErr)
	}
	if got := strings.Count(logs.String(), "event_name=postgres.store.error"); got != 1 {
		t.Fatalf("a mid-result-set failure logged %d records, want exactly 1 (Close re-reports the same error):\n%s", got, logs.String())
	}
	for _, want := range []string{
		"level=ERROR", "event_name=postgres.store.error", "failure_class=failed",
		"postgres_store.operation=rows", "tenant_secrets", "SELECT name FROM tenant_secrets WHERE id = $1",
	} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("operator log lacks %q:\n%s", want, logs.String())
		}
	}
}

func TestExecUniqueViolationIsBoundedAndLogsAtWarn(t *testing.T) {
	t.Parallel()

	cause := &pgconn.PgError{
		Severity: "ERROR", Code: "23505", ConstraintName: "uq_accounts_email",
		Message: `duplicate key value violates unique constraint "uq_accounts_email"`,
	}
	db, _, logs := openFake(t, &script{execErr: cause})
	_, err := db.ExecContext(context.Background(), "INSERT INTO accounts(email) VALUES ($1)", "a@b.c")
	requireBounded(t, err, KindFailed, "uq_accounts_email")
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "postgres_store.sqlstate=23505") {
		t.Fatalf("a client-triggerable integrity error must log at WARN with its SQLSTATE:\n%s", logs.String())
	}
}

func TestErrBadConnStillRetriesOnAFreshConnection(t *testing.T) {
	t.Parallel()

	db, fake, logs := openFake(t, &script{badConnOnce: true})
	if _, err := db.ExecContext(context.Background(), "UPDATE t SET x = 1"); err != nil {
		t.Fatalf("ExecContext() error = %v, want nil after database/sql retries the bad connection", err)
	}
	if fake.connects < 2 {
		t.Fatalf("connects = %d, want at least 2: database/sql must see driver.ErrBadConn and redial", fake.connects)
	}
	if logs.Len() != 0 {
		t.Fatalf("a retried bad connection logged: %s", logs.String())
	}
}

func TestCommitErrorIsBounded(t *testing.T) {
	t.Parallel()

	db, _, _ := openFake(t, &script{commitErr: &pgconn.PgError{Code: "40001", Message: "could not serialize access due to concurrent update of row 42"}})
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeginTx() error = %v, want nil", err)
	}
	requireBounded(t, tx.Commit(), KindFailed, "row 42")
}

func TestCanceledRequestIsBoundedButNotLogged(t *testing.T) {
	t.Parallel()

	db, _, logs := openFake(t, &script{queryErr: context.Canceled})
	_, err := db.QueryContext(context.Background(), "SELECT 1")
	requireBounded(t, err, KindCanceled, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatal("errors.Is(err, context.Canceled) = false, want true through Unwrap")
	}
	if logs.Len() != 0 {
		t.Fatalf("a canceled request is a client disconnect, not a fault; it logged: %s", logs.String())
	}
}

func TestTimeoutLogsAtWarn(t *testing.T) {
	t.Parallel()

	db, _, logs := openFake(t, &script{execErr: &pgconn.PgError{Code: "57014", Message: "canceling statement due to statement timeout"}})
	_, err := db.ExecContext(context.Background(), "SELECT pg_sleep(60)")
	requireBounded(t, err, KindTimeout, "statement timeout")
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "failure_class=timeout") {
		t.Fatalf("a timeout must log at WARN:\n%s", logs.String())
	}
}

func TestPoolBoundsARealDialFailureAndKeepsItsCause(t *testing.T) {
	t.Parallel()

	cfg, err := pgx.ParseConfig("postgres://alice:s3cret@127.0.0.1:1/appdb?sslmode=disable&connect_timeout=2")
	if err != nil {
		t.Fatalf("ParseConfig() error = %v", err)
	}
	db := sql.OpenDB(NewConnector(stdlib.GetConnector(*cfg)))
	t.Cleanup(func() { _ = db.Close() })
	err = db.PingContext(context.Background())
	requireBounded(t, err, KindUnavailable, "alice")
	for _, leaked := range []string{"appdb", "127.0.0.1", "user=", "s3cret"} {
		if strings.Contains(err.Error(), leaked) {
			t.Errorf("Error() = %q carries %q", err.Error(), leaked)
		}
	}
	var connectErr *pgconn.ConnectError
	if !errors.As(err, &connectErr) {
		t.Fatal("errors.As(ConnectError) = false, want the driver error reachable for classification")
	}
}

// requireBounded asserts err is an *Error of kind whose text is the fixed
// public string and omits leaked (when non-empty).
func requireBounded(t *testing.T, err error, kind Kind, leaked string) {
	t.Helper()

	var bounded *Error
	if !errors.As(err, &bounded) {
		t.Fatalf("err = %v (%T), want an *Error", err, err)
	}
	if bounded.Kind() != kind || err.Error() != publicText[kind] {
		t.Fatalf("err = %q kind=%q, want %q kind=%q", err.Error(), bounded.Kind(), publicText[kind], kind)
	}
	if leaked != "" && strings.Contains(err.Error(), leaked) {
		t.Fatalf("Error() = %q carries %q", err.Error(), leaked)
	}
}
