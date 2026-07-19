// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// This file holds the exported AuthMiddleware* constructor wrappers. They all
// delegate to the unexported authMiddleware / authMiddlewareWithRoutePolicy in
// auth.go, differing only in which optional resolvers, audit sink, route
// policy, and enforcement predicate they thread through.
//
// The legacy constructors derive dev-mode-open from the shared key alone
// (authEnforcementConfigured = token != ""), which is bit-for-bit the
// pre-existing behavior and keeps the large existing test-call surface
// unchanged. Production wiring that also configures a scoped-token file or an
// OIDC bearer audience (with no shared key) MUST instead use one of the
// *AndEnforcement variants and pass the explicit wiring-computed predicate, or
// a headerless request would be served open despite a real auth source being
// configured (the headerless bypass this fix closes).

// AuthMiddleware wraps an HTTP handler with bearer token authentication.
//
// If token is empty, authentication is disabled (dev mode).
// If the request path is in publicHTTPPaths, authentication is skipped.
// Otherwise, the Authorization header must contain "Bearer <token>" with
// a token that matches the configured value using constant-time comparison.
//
// Returns 401 Unauthorized with a JSON error body if authentication fails.
func AuthMiddleware(token string, next http.Handler) http.Handler {
	return authMiddleware(token, nil, nil, next, nil, token != "")
}

// AuthMiddlewareWithGovernanceAudit wraps an HTTP handler with bearer token
// authentication and records denied read-authorization events when a private
// audit sink is available.
func AuthMiddlewareWithGovernanceAudit(
	token string,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return authMiddleware(token, nil, nil, next, audit, token != "")
}

// AuthMiddlewareWithScopedTokens wraps an HTTP handler with shared-token
// compatibility plus optional scoped-token resolution.
func AuthMiddlewareWithScopedTokens(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
) http.Handler {
	return authMiddleware(token, resolver, nil, next, nil, token != "")
}

// AuthMiddlewareWithBrowserSessionsAndScopedTokens wraps an HTTP handler with
// shared-token, scoped-token, and server-managed browser-session authentication.
func AuthMiddlewareWithBrowserSessionsAndScopedTokens(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
) http.Handler {
	return authMiddleware(token, resolver, sessionResolver, next, nil, token != "")
}

// AuthMiddlewareWithBrowserSessionsScopedTokensAndGovernanceAudit wraps an HTTP
// handler with shared-token compatibility, scoped-token resolution, browser
// session-cookie resolution, and denied read-authorization audit events.
func AuthMiddlewareWithBrowserSessionsScopedTokensAndGovernanceAudit(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return authMiddleware(token, resolver, sessionResolver, next, audit, token != "")
}

// AuthMiddlewareWithScopedTokensAndGovernanceAudit wraps an HTTP handler with
// shared-token compatibility, optional scoped-token resolution, and denied
// read-authorization audit events. A nil resolver disables scoped-token
// resolution, leaving shared-token (or dev-mode when token is empty) behavior
// unchanged.
//
// This constructor derives dev-mode-open from the shared key only. Production
// wiring that may configure a scoped-token file or OIDC bearer audience
// without a shared key MUST use
// AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement instead.
func AuthMiddlewareWithScopedTokensAndGovernanceAudit(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return authMiddleware(token, resolver, nil, next, audit, token != "")
}

// AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement is the
// production variant used by cmd/mcp-server. Unlike the legacy constructors —
// which derive dev-mode-open from the shared key alone — it takes the explicit
// wiring-computed authEnforcementConfigured predicate (shared key OR
// scoped-token file OR OIDC bearer audience configured) so a
// scoped-token-file-only or OIDC-bearer-only deployment, with no shared
// ESHU_API_KEY, still denies headerless requests instead of serving them open.
// See cmd/mcp-server/wiring.go.
func AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	authEnforcementConfigured bool,
) http.Handler {
	return authMiddleware(token, resolver, nil, next, audit, authEnforcementConfigured)
}
