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
	return authMiddleware(token, nil, nil, next, nil, token != "", nil)
}

// AuthMiddlewareWithGovernanceAudit wraps an HTTP handler with bearer token
// authentication and records denied read-authorization events when a private
// audit sink is available.
func AuthMiddlewareWithGovernanceAudit(
	token string,
	next http.Handler,
	audit GovernanceAuditAppender,
) http.Handler {
	return authMiddleware(token, nil, nil, next, audit, token != "", nil)
}

// AuthMiddlewareWithScopedTokens wraps an HTTP handler with shared-token
// compatibility plus optional scoped-token resolution.
func AuthMiddlewareWithScopedTokens(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
) http.Handler {
	return authMiddleware(token, resolver, nil, next, nil, token != "", nil)
}

// AuthMiddlewareWithBrowserSessionsAndScopedTokens wraps an HTTP handler with
// shared-token, scoped-token, and server-managed browser-session authentication.
func AuthMiddlewareWithBrowserSessionsAndScopedTokens(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
) http.Handler {
	return authMiddleware(token, resolver, sessionResolver, next, nil, token != "", nil)
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
	return authMiddleware(token, resolver, sessionResolver, next, audit, token != "", nil)
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
	return authMiddleware(token, resolver, nil, next, audit, token != "", nil)
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
	return authMiddleware(token, resolver, nil, next, audit, authEnforcementConfigured, nil)
}

// AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge is
// the F-2 (issue #5163) production variant: it is
// AuthMiddlewareWithScopedTokensGovernanceAuditAndEnforcement plus an
// OAuthChallengePolicy. It threads the SAME explicit wiring-computed
// authEnforcementConfigured predicate (so a scoped-token-file-only or
// OIDC-bearer-only MCP deployment still denies headerless requests instead of
// serving them open) and, when the deployment has at least one configured
// identity provider, adds RFC 9728 resource_metadata (and an RFC 6750 scope) to
// a genuine bearer-credential 401's WWW-Authenticate header. A nil
// oauthChallenge leaves every 401 byte-identical to the *AndEnforcement
// constructor's bare "Bearer". cmd/mcp-server/wiring.go uses this for both the
// /api/ authed handler and the /sse + /mcp/message transport auth.
func AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	authEnforcementConfigured bool,
	oauthChallenge OAuthChallengePolicy,
) http.Handler {
	return authMiddleware(token, resolver, nil, next, audit, authEnforcementConfigured, oauthChallenge)
}

// AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit
// is AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementAndOAuthChallenge
// plus allowedAudit, the F-9 (#5170) allowed-read governance-audit sink. It
// records an ALLOWED read_authorization event for every scoped-token or
// OIDC-bearer credential that resolves successfully, immediately before
// dispatch, mirroring the denial event audit already records on the failure
// paths. A nil allowedAudit is a safe no-op, byte-identical to the
// *AndOAuthChallenge constructor above.
//
// This is the ONLY constructor that threads a non-nil allowedAudit in
// production: cmd/mcp-server/wiring.go uses it exclusively for the MCP
// transport middleware (GET /sse, POST /mcp/message), never for the
// /api/v0/* authedHandler mcp-server also builds and never for cmd/api,
// so tools/call's internal dispatch through the same credential chain does
// not double-emit one logical MCP read. See the F-9 design addendum §2 for
// the full route-scope rationale. allowedAudit is expected to be a
// governanceauditasync.AsyncAppender in production so this call never adds a
// synchronous Postgres round trip to the read path; see that package's
// README for why.
func AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAndAllowedReadAudit(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	authEnforcementConfigured bool,
	oauthChallenge OAuthChallengePolicy,
	allowedAudit GovernanceAuditAppender,
) http.Handler {
	return authMiddlewareWithAllowedReadAudit(
		token, resolver, nil, next, audit, authEnforcementConfigured, oauthChallenge, allowedAudit,
	)
}
