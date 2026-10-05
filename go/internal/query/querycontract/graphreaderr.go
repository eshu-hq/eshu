// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Sentinel errors for the bounded graph-read policy. Handlers compare against
// these with errors.Is to decide the HTTP contract below, which makes them part
// of the read contract rather than an implementation detail of the driver.
var (
	// ErrGraphReadDeadline reports that the bounded graph-read budget expired.
	ErrGraphReadDeadline = errors.New("graph query exceeded its deadline")
	// ErrGraphUnavailable reports that the graph backend could not serve a read.
	ErrGraphUnavailable = errors.New("graph temporarily unavailable; retry after graph health is restored")
	// ErrGraphQueryFailed reports that the graph backend rejected or failed a
	// read for a reason that is neither a deadline nor an availability
	// problem. Its text is the only thing a caller may show a client: the
	// driver's own message quotes the statement, inline literals included
	// (#7253). The driver cause stays reachable through errors.As and
	// Unwrap for classification and operator logs.
	ErrGraphQueryFailed = errors.New("graph query failed")
	// ErrReaderRetryable is the public, fixed-text face of a PostgreSQL
	// reader that could not serve a fenced read in time: the replica had not
	// replayed to the writer checkpoint (db.ErrReaderStale) or the reader pool
	// wait timed out (db.ErrReaderUnavailable joined with
	// context.DeadlineExceeded). A reader failure that is not a timeout
	// (authentication, connection refused, permission denied, client cancel)
	// is not retryable and never carries this text. Its text is the only thing a
	// caller may show a client; the sentinel the reader returned stays
	// reachable through errors.Is for classification and operator logs (#7523).
	ErrReaderRetryable = errors.New("database read temporarily unavailable; retry shortly")
)

// BackendUnavailableRetryAfterSeconds is the Retry-After value, in seconds,
// sent with the transient 503 backend_unavailable graph-read verdicts (graph
// unavailable, stale or timed-out PostgreSQL reader), the WithCheckpoint 503, and
// the identity-store 503 (#7586); a permanent 503 such as an unconfigured graph
// backend carries no hint. It is
// a fixed hint sized to the reader replay window (2 s by default), so a retry
// lands after the replica has had one full catch-up window; the server reads
// no clock to derive it.
const BackendUnavailableRetryAfterSeconds = db.ReaderRetryAfterSeconds

// ClassifyBoundedGraphReadError maps a graph-read error onto
// ErrGraphReadDeadline when the read ran out of time, so the caller answers the
// stable 504 backend_timeout contract through WriteGraphReadError instead of a
// generic 500.
//
// ctx is the bounded context the reads ran under (typically the one
// WithBoundedGraphReadDeadline returned). The read counts as timed out when err
// wraps context.DeadlineExceeded or when ctx's deadline has expired, whatever
// error the reader returned. The second test is the #7353 hardening: when the
// bounded context expires mid-read, the Neo4j driver surfaces a
// ConnectivityError ("Timeout while reading from connection"), not ctx.Err(),
// so an errors.Is check on err alone misses it. Neo4jReader already classifies
// that case itself; this covers every other GraphQuery implementation.
//
// A canceled ctx (client disconnect) is not a deadline and err is returned
// unchanged, as is any error while ctx is still live -- a genuine connectivity
// failure keeps its own mapping. An error that already carries a graph-read
// sentinel (ErrGraphReadDeadline or ErrGraphUnavailable) is the reader's own
// verdict and is also returned unchanged, so a handler's failure_class log and
// its 503/504 response always agree. The same holds for a PostgreSQL reader
// verdict (db.ErrReaderStale, or db.ErrReaderUnavailable with a connection
// acquisition or identity-check deadline): those wrap context.DeadlineExceeded but are retryable 503s, never
// the 504 budget. A non-deadline db.ErrReaderUnavailable is not a fence verdict
// and is classified like any other error.
//
// When it does classify, the returned error wraps both ErrGraphReadDeadline
// and the reader's original err, so logs keep the cause while the response
// carries only the public sentinel message. A nil err stays nil.
func ClassifyBoundedGraphReadError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, ErrGraphReadDeadline) || errors.Is(err, ErrGraphUnavailable) || isReaderFenceError(err) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrGraphReadDeadline, err)
	}
	return err
}

// isReaderFenceError reports whether err carries a transient PostgreSQL reader
// verdict that is safe to tell a client to retry: a replica that missed the
// writer checkpoint (db.ErrReaderStale, always a replay-window deadline), or a
// reader connection acquisition (pool wait or dial) or identity check that
// timed out inside the replay window (db.ErrReaderUnavailable joined with
// context.DeadlineExceeded).
//
// db.ErrReaderUnavailable alone is NOT enough. runtime/postgres joins that
// sentinel onto every reader connection failure, identity-query error, and
// replay-query error, including permanent ones (authentication or TLS failure,
// connection refused, a role denied pg_control_system) and a client
// context.Canceled. Those fall through to the caller's own 500 so a permanent
// misconfiguration is never told "retry shortly". The deadline-wrapping
// verdicts also must be left alone by ClassifyBoundedGraphReadError, or a
// transient reader condition would read as a 504 graph deadline.
func isReaderFenceError(err error) bool {
	if errors.Is(err, db.ErrReaderStale) {
		return true
	}
	return errors.Is(err, db.ErrReaderUnavailable) && errors.Is(err, context.DeadlineExceeded)
}

type graphReadHTTPError struct {
	status  int
	code    ErrorCode
	message string
	details map[string]any
	// retryable marks a verdict that is transient and safe to tell a client to
	// retry (#7536). It is set per verdict, never derived from the status code,
	// so a future permanent 503 verdict cannot inherit Retry-After.
	retryable bool
}

// WriteGraphReadError writes the stable HTTP contract for a bounded graph-read
// availability error, and for a PostgreSQL reader that was stale or whose
// connection acquisition (pool wait or dial) or identity check timed out (a
// retryable 503 backend_unavailable with Retry-After, #7523).
// Each verdict carries its own retryable marker (graph unavailable and the
// reader fence are marked); WriteErrorEnvelope sets Retry-After only for a
// marked 503, never for an arbitrary 503 and not merely because the status is 503.
// A reader failure that is not a timeout is not claimed and stays the caller's 500.
// It returns false without touching the response when err
// is not one of the shared graph-read errors, leaving the caller's own mapping
// in place.
func WriteGraphReadError(w http.ResponseWriter, r *http.Request, err error, capability string) bool {
	status, errEnv, ok := GraphReadErrorEnvelope(err, capability)
	if !ok {
		return false
	}
	WriteErrorEnvelope(w, r, status, errEnv)
	return true
}

// GraphReadErrorEnvelope returns the same stable status and error envelope that
// WriteGraphReadError would write, for seams that return an envelope to their
// caller instead of writing the response themselves. It reports false when err
// is not one of the shared graph-read errors.
func GraphReadErrorEnvelope(err error, capability string) (int, *ErrorEnvelope, bool) {
	mapped, ok := mapGraphReadError(err)
	if !ok {
		return 0, nil, false
	}
	return mapped.status, mapped.envelope(capability), true
}

// envelope builds the wire envelope for one mapped verdict.
func (e graphReadHTTPError) envelope(capability string) *ErrorEnvelope {
	return &ErrorEnvelope{
		Code:       e.code,
		Message:    e.message,
		Capability: capability,
		Details:    e.details,
		retryable:  e.retryable,
	}
}

// mapGraphReadError is the verdict mapper GraphReadErrorEnvelope runs. It is a
// variable only so a test can drive a verdict the production mapping does not
// produce today (a 503 that is not retryable) through the real envelope and
// writer path (#7536); nothing outside tests reassigns it.
var mapGraphReadError = mapGraphReadHTTPError

func mapGraphReadHTTPError(err error) (graphReadHTTPError, bool) {
	switch {
	case errors.Is(err, ErrGraphUnavailable):
		return graphReadHTTPError{
			status:    http.StatusServiceUnavailable,
			code:      ErrorCodeBackendUnavailable,
			message:   ErrGraphUnavailable.Error(),
			retryable: true,
		}, true
	case isReaderFenceError(err):
		// Checked before the deadline sentinel: the reader's stale verdict
		// wraps context.DeadlineExceeded, but it is a retryable 503, not the
		// 504 bounded-read budget. The body carries only the fixed message;
		// the reader's error text never reaches the client.
		return graphReadHTTPError{
			status:    http.StatusServiceUnavailable,
			code:      ErrorCodeBackendUnavailable,
			message:   ErrReaderRetryable.Error(),
			details:   map[string]any{"retry_after_seconds": BackendUnavailableRetryAfterSeconds},
			retryable: true,
		}, true
	case errors.Is(err, ErrGraphReadDeadline):
		return graphReadHTTPError{
			status:  http.StatusGatewayTimeout,
			code:    ErrorCodeBackendTimeout,
			message: ErrGraphReadDeadline.Error(),
		}, true
	default:
		return graphReadHTTPError{}, false
	}
}

// statementRejecter is implemented by a graph-read error that wraps the backend
// rejecting the statement itself (a Cypher syntax or semantic error).
type statementRejecter interface {
	StatementRejection() (string, bool)
}

// GraphStatementRejection reports whether err is the graph backend rejecting
// the submitted statement as malformed, and returns the first line of the
// backend's message with every numeric and string literal redacted (the lines
// that quote the statement are dropped). It is for the routes that run a
// caller-authored statement (read-only Cypher, graph-query visualization),
// which answer 400 with this message so the author can fix the query instead of
// a bare 500 (#7253). A route that runs a statement Eshu built must not use it:
// a rejected server-built statement is a server fault. Any other error,
// including nil, reports false.
func GraphStatementRejection(err error) (string, bool) {
	var rejecter statementRejecter
	if err == nil || !errors.As(err, &rejecter) {
		return "", false
	}
	return rejecter.StatementRejection()
}
