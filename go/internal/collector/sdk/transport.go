// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
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
//
// A bare io.EOF is transient only when it is wrapped in a *url.Error, which is
// how net/http reports a server that closed the connection before sending a
// response. The same bare io.EOF from a JSON decoder reading a 200 response with
// an empty body (a proxy, WAF, or misrouted base URL) is a persistent content
// problem, not a dropped connection, and returns false; a body cut short
// mid-response is io.ErrUnexpectedEOF and stays transient. Retries are not
// unbounded: callers stop after MaxConsecutiveTransportFailures.
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
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, io.EOF) {
		var urlErr *url.Error
		return errors.As(err, &urlErr)
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

// MaxConsecutiveTransportFailures bounds how long a collector retries the same
// source through transient transport errors before it treats the failure as
// persistent and returns it as fatal. A wrong host or port, a removed service,
// or a permanently blocked network path all surface as connection refused or
// unreachable forever; without a ceiling that misconfiguration would look like
// a healthy, idle collector instead of the crash-loop an operator can alert on.
const MaxConsecutiveTransportFailures = 20

// TransportFailureClass names the cause of a transport error as a bounded,
// low-cardinality string safe for logs and metric labels: "reset", "refused",
// "timeout", "unreachable", "eof", "dns", or "other". It never includes hosts,
// URLs, or error text.
func TransportFailureClass(err error) string {
	switch {
	case err == nil:
		return "other"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ECONNRESET),
		errors.Is(err, syscall.ECONNABORTED),
		errors.Is(err, syscall.EPIPE):
		return "reset"
	case errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETUNREACH):
		return "unreachable"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "timeout"
	case errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, io.EOF):
		return "eof"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return "other"
}
