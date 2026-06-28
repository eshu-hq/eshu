// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// RunReadOnlyTool invokes a read-only MCP tool in-process without an HTTP round-trip.
//
// It is a thin exported seam over the unexported dispatchTool function, intended
// for the Ask Eshu engine and similar callers that need to invoke read capabilities
// programmatically without going through the network stack.
//
// Constraints:
//   - Only routes that resolveRoute recognises are supported; mutation tools are
//     not reachable through this function but callers should not rely on that as a
//     safety boundary — use an appropriately scoped handler.
//   - The handler must be safe for concurrent use; RunReadOnlyTool makes a single
//     synchronous ServeHTTP call per invocation.
//   - Scope and authentication are threaded via ctx and authHeader, exactly as they
//     are for the normal MCP transport path.
//
// Returns (envelope, value, isError, nil) on a successful dispatch. When the tool
// response is a canonical ResponseEnvelope, envelope is non-nil and value is nil.
// When the tool response is plain JSON (e.g. list_collectors), envelope is nil and
// value carries the decoded payload. Returns (nil, nil, false, err) when the tool
// name is unknown or the dispatch fails before a response is produced.
func RunReadOnlyTool(
	ctx context.Context,
	handler http.Handler,
	toolName string,
	args map[string]any,
	authHeader string,
	logger *slog.Logger,
) (*query.ResponseEnvelope, any, bool, error) {
	result, err := dispatchTool(ctx, handler, toolName, args, authHeader, logger)
	if err != nil {
		return nil, nil, false, err
	}
	if result.Envelope != nil {
		return result.Envelope, nil, result.IsError, nil
	}
	return nil, result.Value, result.IsError, nil
}

// InProcessMessageHandler returns an [http.Handler] that processes MCP JSON-RPC
// messages backed by the given query handler. It is the exported replay seam for
// R-9 (#4111): the handler can be driven via httptest without a network server,
// so mcpreplay records and asserts MCP tool responses offline.
//
// The returned handler uses the standalone-POST path of handleHTTPMessage (no
// SSE session, no sessionId query parameter). Every call is synchronous: the
// JSON-RPC response is written directly to the HTTP response body and returned
// to the caller. This matches the standalone-POST behavior a client gets when
// posting to /mcp/message without a sessionId; the SSE-linked path (202
// Accepted + channel delivery) is not exercised.
//
// logger may be nil; a discard logger is substituted to avoid noise in test
// output. The discard logger satisfies the nil-guard inside NewServer, so the
// server is always initialised with a valid logger.
func InProcessMessageHandler(queryHandler http.Handler, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	s := NewServer(queryHandler, logger)
	return http.HandlerFunc(s.handleHTTPMessage)
}
