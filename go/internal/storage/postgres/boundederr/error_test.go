// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// retryableErr is an opaque error that implements the SafeToRetry contract
// pgx's own "conn closed" lock error does, without its text.
type retryableErr struct{}

func (retryableErr) Error() string     { return "opaque" }
func (retryableErr) SafeToRetry() bool { return true }

func TestClassifyUsesTypeAndSQLStateNotText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want Kind
	}{
		{"canceled", context.Canceled, KindCanceled},
		{"deadline", context.DeadlineExceeded, KindTimeout},
		{"wrapped deadline", fmt.Errorf("read: %w", context.DeadlineExceeded), KindTimeout},
		{"query_canceled", &pgconn.PgError{Code: "57014", Message: "canceling statement"}, KindTimeout},
		{"lock_not_available", &pgconn.PgError{Code: "55P03"}, KindTimeout},
		{"connection_exception", &pgconn.PgError{Code: "08006"}, KindUnavailable},
		{"admin_shutdown", &pgconn.PgError{Code: "57P01"}, KindUnavailable},
		{"unique_violation", &pgconn.PgError{Code: "23505"}, KindFailed},
		{"safe to retry without the text", retryableErr{}, KindUnavailable},
		{"net error", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, KindUnavailable},
		{"unexpected eof", io.ErrUnexpectedEOF, KindUnavailable},
		{"plain", errors.New("boom"), KindFailed},
		{"text that looks like a timeout is not one", errors.New("context deadline exceeded"), KindFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := classify(tt.err); got != tt.want {
				t.Fatalf("classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorTextIsFixedPerKindAndKeepsTheCause(t *testing.T) {
	t.Parallel()

	cause := &pgconn.PgError{
		Severity: "ERROR", Code: "23505", ConstraintName: "uq_secret_table",
		Message: `duplicate key value violates unique constraint "uq_secret_table"`,
	}
	bounded := (&observer{}).newError(cause)

	for _, leaked := range []string{"uq_secret_table", "23505", "duplicate", "ERROR"} {
		if strings.Contains(bounded.Error(), leaked) {
			t.Errorf("Error() = %q carries %q", bounded.Error(), leaked)
		}
	}
	if bounded.Error() != publicText[KindFailed] || bounded.Kind() != KindFailed {
		t.Fatalf("Error() = %q kind=%q, want the fixed %q text", bounded.Error(), bounded.Kind(), publicText[KindFailed])
	}
	var pgErr *pgconn.PgError
	if !errors.As(bounded, &pgErr) || pgErr.ConstraintName != "uq_secret_table" {
		t.Fatalf("errors.As(PgError) lost the cause: %v", pgErr)
	}
	if !errors.Is(bounded, cause) {
		t.Fatal("errors.Is(bounded, cause) = false, want true through Unwrap")
	}
}

func TestPassthroughKeepsDatabaseSQLSentinelsByEquality(t *testing.T) {
	t.Parallel()

	for _, sentinel := range []error{io.EOF, driver.ErrBadConn, driver.ErrSkip, driver.ErrRemoveArgument} {
		if !passthrough(sentinel) {
			t.Errorf("passthrough(%v) = false, want true: database/sql branches on it", sentinel)
		}
	}
	if !passthrough(nil) {
		t.Error("passthrough(nil) = false, want true")
	}

	// A driver error that only wraps io.EOF still carries its connection
	// target, so it must be bounded, not passed through.
	wrapped := fmt.Errorf("failed to connect to `user=alice database=appdb`: %w", io.EOF)
	if passthrough(wrapped) {
		t.Fatal("passthrough(error wrapping io.EOF) = true, want false: the target would reach a client")
	}
	if passthrough(fmt.Errorf("x: %w", driver.ErrBadConn)) {
		t.Fatal("passthrough(error wrapping ErrBadConn) = true, want false")
	}
}
