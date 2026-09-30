// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package boundederr

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// Kind is the closed failure vocabulary of a bounded Postgres error. It is also
// the failure_class log value, so it stays low-cardinality.
type Kind string

const (
	// KindUnavailable covers a connection that could not be made or was lost:
	// dial failures, closed connections, and the connection-exception SQLSTATEs.
	KindUnavailable Kind = "unavailable"
	// KindTimeout covers a statement or transaction that ran out of time.
	KindTimeout Kind = "timeout"
	// KindCanceled covers a caller-canceled request, typically a client
	// disconnect.
	KindCanceled Kind = "canceled"
	// KindFailed covers every other driver or server failure.
	KindFailed Kind = "failed"
)

// publicText is the only message a client may see for each Kind. No value in it
// comes from the driver.
var publicText = map[Kind]string{
	KindUnavailable: "postgres store unavailable",
	KindTimeout:     "postgres store timed out",
	KindCanceled:    "postgres store request canceled",
	KindFailed:      "postgres store statement failed",
}

// Error is a Postgres driver error whose text is bounded. Error returns the
// fixed text for its Kind; Unwrap returns the driver's own error, so callers
// that classify with errors.Is or errors.As see exactly what they saw before.
type Error struct {
	kind  Kind
	cause error
}

// Error returns the fixed client-safe text for the failure class.
func (e *Error) Error() string { return publicText[e.kind] }

// Unwrap returns the driver error the text replaced.
func (e *Error) Unwrap() error { return e.cause }

// Kind reports the failure class.
func (e *Error) Kind() Kind { return e.kind }

// Wrap bounds err exactly as a pool opened through Open would, without logging.
// It returns nil for nil and returns err unchanged for the database/sql
// sentinels and for an error that is already bounded. Code that builds a pool
// error by hand, such as a test of a classifier that must keep working on the
// bounded form, uses it.
func Wrap(err error) error {
	if passthrough(err) {
		return err
	}
	return &Error{kind: classify(err), cause: err}
}

// passthrough reports whether err must reach database/sql unchanged. The four
// driver sentinels are matched by equality, not errors.Is: database/sql compares
// io.EOF with == to end a result set, and an errors.Is match would also let a
// driver error that merely wraps one of them (a dropped connection that unwraps
// to io.EOF, say) skip the bound and keep its connection target. The sentinels
// carry no driver text. An already-bounded error is left alone so a second
// wrapper layer does not re-log it.
func passthrough(err error) bool {
	if err == nil {
		return true
	}
	if err == io.EOF || err == driver.ErrBadConn || err == driver.ErrSkip || err == driver.ErrRemoveArgument { //nolint:errorlint // equality is the contract, see above
		return true
	}
	var bounded *Error
	return errors.As(err, &bounded)
}

// classify maps a driver error onto its Kind from the error's type and
// SQLSTATE, never from its text.
func classify(err error) Kind {
	var pgErr *pgconn.PgError
	var connectErr *pgconn.ConnectError
	var netErr net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return KindCanceled
	case errors.Is(err, context.DeadlineExceeded), pgconn.Timeout(err):
		return KindTimeout
	case errors.As(err, &pgErr):
		return classifyServerError(pgErr.Code)
	case errors.As(err, &connectErr), errors.As(err, &netErr), pgconn.SafeToRetry(err),
		errors.Is(err, io.ErrUnexpectedEOF):
		return KindUnavailable
	default:
		return KindFailed
	}
}

// classifyServerError maps a server SQLSTATE: query_canceled (57014) and
// lock_not_available (55P03) are timeouts, the connection-exception class (08)
// and the admin-shutdown states (57P01-57P03) are unavailability.
func classifyServerError(code string) Kind {
	switch {
	case code == "57014", code == "55P03":
		return KindTimeout
	case strings.HasPrefix(code, "08"), code == "57P01", code == "57P02", code == "57P03":
		return KindUnavailable
	default:
		return KindFailed
	}
}
