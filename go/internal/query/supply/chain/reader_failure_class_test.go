// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"regexp"
	"syscall"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// sqlStateError mimics a driver error exposing SQLState() (pgconn.PgError)
// without importing a driver into the handler package.
type sqlStateError struct{ code, msg string }

func (e sqlStateError) Error() string    { return e.msg }
func (e sqlStateError) SQLState() string { return e.code }

// fixedTextError mirrors runtime/postgres privateError: a fixed public text
// whose driver cause stays reachable through Unwrap.
type fixedTextError struct {
	text  string
	cause error
}

func (e fixedTextError) Error() string { return e.text }
func (e fixedTextError) Unwrap() error { return e.cause }

type timeoutNetError struct{}

func (timeoutNetError) Error() string   { return "i/o timeout" }
func (timeoutNetError) Timeout() bool   { return true }
func (timeoutNetError) Temporary() bool { return false }

// TestClassifyReaderFailure pins the closed sets behind the error_site and
// error_cause attributes of supply_chain_query.stage_failed (#7546). Both are
// computed from the error chain only and never carry error text.
func TestClassifyReaderFailure(t *testing.T) {
	t.Parallel()

	resetOp := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	refusedOp := &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
	tests := []struct {
		name      string
		err       error
		wantSite  string
		wantCause string
	}{
		{"arbitrary error", errors.New("boom"), "other", "unknown"},
		{"stale reader", fmt.Errorf("x: %w", errors.Join(db.ErrReaderStale, context.DeadlineExceeded)), "reader_stale", "deadline_exceeded"},
		{"unavailable reader with no cause", db.ErrReaderUnavailable, "reader_unavailable", "unknown"},
		{"borrow failure wrapping a connection reset", fixedTextError{"PostgreSQL reader connection unavailable", errors.Join(db.ErrReaderUnavailable, resetOp)}, "reader_unavailable", "conn_reset"},
		{"connection refused", fixedTextError{"PostgreSQL reader connection unavailable", errors.Join(db.ErrReaderUnavailable, refusedOp)}, "reader_unavailable", "conn_refused"},
		{"deadline exceeded", errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded), "reader_unavailable", "deadline_exceeded"},
		{"canceled", errors.Join(db.ErrReaderUnavailable, context.Canceled), "reader_unavailable", "canceled"},
		{"conn done", errors.Join(db.ErrReaderUnavailable, sql.ErrConnDone), "reader_unavailable", "conn_done"},
		{"eof", errors.Join(db.ErrReaderUnavailable, io.EOF), "reader_unavailable", "eof"},
		{"unexpected eof", fmt.Errorf("wrap: %w", io.ErrUnexpectedEOF), "other", "eof"},
		{"net timeout", errors.Join(db.ErrReaderUnavailable, timeoutNetError{}), "reader_unavailable", "net_timeout"},
		{"sqlstate class", errors.Join(db.ErrReaderUnavailable, sqlStateError{"08006", "connection failure host=secret"}), "reader_unavailable", "sqlstate_08"},
		{"sqlstate auth class", sqlStateError{"28P01", "password authentication failed for user admin"}, "other", "sqlstate_28"},
		{"malformed sqlstate is unknown", sqlStateError{"zz'\n", "bad"}, "other", "unknown"},
		{"short sqlstate is unknown", sqlStateError{"0", "bad"}, "other", "unknown"},
		{"nil error", nil, "other", "unknown"},
	}
	closedSite := map[string]struct{}{"reader_stale": {}, "reader_unavailable": {}, "other": {}}
	closedCause := regexp.MustCompile(`^(deadline_exceeded|canceled|conn_done|eof|conn_refused|conn_reset|net_timeout|sqlstate_[0-9A-Z]{2}|unknown)$`)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			site, cause := classifyReaderFailure(tt.err)
			if site != tt.wantSite || cause != tt.wantCause {
				t.Fatalf("classifyReaderFailure() = (%q, %q), want (%q, %q)", site, cause, tt.wantSite, tt.wantCause)
			}
			if _, ok := closedSite[site]; !ok {
				t.Fatalf("site %q is outside the closed set", site)
			}
			if !closedCause.MatchString(cause) {
				t.Fatalf("cause %q is outside the closed set", cause)
			}
		})
	}
}
