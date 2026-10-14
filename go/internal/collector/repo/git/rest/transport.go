// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rest

import (
	"context"
	"errors"
	"reflect"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// streamErrorTypes are the reflect type names of an HTTP/2 stream reset: the
// one net/http's bundled HTTP/2 client returns (an unexported type, so it is
// matched by name; the package test pins the name against a real server) and
// golang.org/x/net/http2's.
var streamErrorTypes = map[string]struct{}{
	"http.http2StreamError": {},
	"http2.StreamError":     {},
}

// isStreamError reports whether err is, or wraps, an HTTP/2 RST_STREAM
// received while reading a response. It wraps no io.ErrUnexpectedEOF and is
// not a net.Error, so sdk.IsTransientTransportError does not accept it.
func isStreamError(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if _, ok := streamErrorTypes[reflect.TypeOf(err).String()]; ok {
			return true
		}
	}
	return false
}

// IsTransient reports whether err is a connection-level fault a GitHub call
// retries: everything sdk.IsTransientTransportError accepts, plus an HTTP/2
// stream reset. A cancelled context is never transient, so shutdown is never
// retried.
func IsTransient(ctx context.Context, err error) bool {
	if sdk.IsTransientTransportError(ctx, err) {
		return true
	}
	if err == nil || (ctx != nil && ctx.Err() != nil) || errors.Is(err, context.Canceled) {
		return false
	}
	return isStreamError(err)
}

// TransportClass names the failure class of a transport error as a bounded
// metric label: reset, refused, timeout, unreachable, eof, dns, or other. An
// HTTP/2 stream reset is a "reset".
func TransportClass(err error) string {
	if isStreamError(err) {
		return "reset"
	}
	return sdk.TransportFailureClass(err)
}
