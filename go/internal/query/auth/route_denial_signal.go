// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package auth

import "context"

// scopedRouteDenialSignal records that the auth middleware refused THIS
// request on route admission -- ScopedBearerRouteDenialReason or
// BrowserSessionRouteDenialReason returned a reason code and
// scopedRouteDeniedResponse wrote the 403 -- rather than on credential
// resolution. It is a pointer carried on the request context so a mutation
// inside the middleware is visible to the caller that installed it after
// ServeHTTP returns, even though context values themselves are immutable. It
// is the same shape go/internal/mcp's denyClassification uses, and for the
// same reason: the observer sits outside the handler that knows the answer.
type scopedRouteDenialSignal struct{ denied bool }

type scopedRouteDenialSignalCtxKey struct{}

// WithScopedRouteDenialSignal derives a context carrying a fresh route-denial
// signal and returns it with the predicate that reads it back. The predicate
// reports whether the auth middleware refused the request on route admission;
// it is false for an admitted request and for a 401 that never got as far as
// route admission.
//
// It exists for one caller: go/internal/mcp's authenticatedTransportHandler,
// which labels eshu_dp_mcp_transport_auth_denied_total and, without this,
// classifies every unmarked 401/403 as reason="unauthenticated". An all-scope
// bearer refused on GET /sse or POST /mcp/message under hosted_multi_tenant is
// an authorization refusal, not a failed authentication, and an operator
// paging on an authentication-failure spike must not be woken by a governance
// mode working exactly as configured.
//
// The dependency has to run this way round. go/internal/mcp imports
// go/internal/query, never the reverse, so query cannot mark a sentinel that
// mcp defines; mcp installs the sentinel query defines, and query marks it.
// A response header would be the other option and is worse: it would put an
// internal admission fact on the wire for every refused MCP client to read.
//
// Installing this is optional. cmd/api does not, and the mark below is a
// no-op with no signal on the context, so the refusal path costs one type
// assertion on a request that is already being refused.
//
// The predicate must be called from the same goroutine that served the
// request, after ServeHTTP returns. Both the write and the read happen on the
// request goroutine, so there is no cross-goroutine visibility question to
// answer.
func WithScopedRouteDenialSignal(ctx context.Context) (context.Context, func() bool) {
	signal := &scopedRouteDenialSignal{}
	return context.WithValue(ctx, scopedRouteDenialSignalCtxKey{}, signal),
		func() bool { return signal.denied }
}

// MarkScopedRouteDenied sets the signal WithScopedRouteDenialSignal installed,
// if any. scopedRouteDeniedResponse calls it, which is the single choke point
// every route-admission refusal goes through -- the scoped-bearer branch and
// the browser-session branch of authMiddlewareWithRoutePolicy both write their
// 403 there -- so the signal cannot drift out of step with the response the
// caller actually received.
func MarkScopedRouteDenied(ctx context.Context) {
	if signal, ok := ctx.Value(scopedRouteDenialSignalCtxKey{}).(*scopedRouteDenialSignal); ok {
		signal.denied = true
	}
}
