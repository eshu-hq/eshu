// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"io"
	"net"
	"syscall"
)

// transientErrnos are the socket-level failures that a healthy provider
// produces during load balancer rotations, idle-connection reaping, and pod
// restarts. Each one is a property of the connection, not of the request, so
// the same request is expected to succeed on the next attempt.
var transientErrnos = []error{
	syscall.ECONNRESET,
	syscall.ECONNREFUSED,
	syscall.ECONNABORTED,
	syscall.EPIPE,
	syscall.ETIMEDOUT,
	syscall.EHOSTUNREACH,
	syscall.ENETUNREACH,
}

// IsTransientTransportError reports whether err is a transient connection-level
// failure that a collector should retry after backoff instead of treating as a
// fatal source error: connection reset/refused/aborted, broken pipe, an
// unexpected or premature EOF, or a network timeout (including a per-request
// http.Client timeout).
//
// It never reports true when the parent ctx is already done or when err wraps
// context.Canceled, so operator shutdown and caller cancellation are never
// retried. TLS or certificate failures, DNS "not found", malformed responses,
// and HTTP status errors are not transport errors and return false; status
// handling stays with StatusPolicy.
func IsTransientTransportError(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx != nil && ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	for _, errno := range transientErrnos {
		if errors.Is(err, errno) {
			return true
		}
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return dnsErr.IsTimeout || dnsErr.IsTemporary
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}
