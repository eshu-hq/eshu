// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"errors"
	"fmt"
	"net/http"
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
)

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
// its 503/504 response always agree. The returned error wraps both
// ErrGraphReadDeadline and the reader's original err, so logs keep the cause
// while the response carries only the public sentinel message. A nil err
// stays nil.
func ClassifyBoundedGraphReadError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, ErrGraphReadDeadline) || errors.Is(err, ErrGraphUnavailable) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrGraphReadDeadline, err)
	}
	return err
}

type graphReadHTTPError struct {
	status  int
	code    ErrorCode
	message string
}

// WriteGraphReadError writes the stable HTTP contract for a bounded graph-read
// availability error. It returns false without touching the response when err
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
	mapped, ok := mapGraphReadHTTPError(err)
	if !ok {
		return 0, nil, false
	}
	return mapped.status, &ErrorEnvelope{
		Code:       mapped.code,
		Message:    mapped.message,
		Capability: capability,
	}, true
}

func mapGraphReadHTTPError(err error) (graphReadHTTPError, bool) {
	switch {
	case errors.Is(err, ErrGraphUnavailable):
		return graphReadHTTPError{
			status:  http.StatusServiceUnavailable,
			code:    ErrorCodeBackendUnavailable,
			message: ErrGraphUnavailable.Error(),
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
