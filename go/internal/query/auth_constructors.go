// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

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
		token, resolver, nil, next, audit,
		BrowserSessionRoutePolicy{}, authEnforcementConfigured, oauthChallenge, allowedAudit,
	)
}

// AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAllowedReadAuditAndRoutePolicy
// is the constructor above plus an explicit route policy, and it is what
// cmd/mcp-server wires. The policy decides whether an all-scope bearer may
// enter a grant-bound allowlisted route (#6450 residual item 1); the sibling
// above keeps the fail-closed zero value, which is the right default for a
// caller that has not thought about it and the wrong one for mcp-server, whose
// local_no_policy and hosted_single_tenant deployments have always answered an
// all-scope token from the whole corpus by design.
//
// Pass ScopedRoutePolicyForGovernanceMode(governanceStatus) rather than a
// literal, so both commands read the opening off the same ESHU_GOVERNANCE_MODE
// table.
func AuthMiddlewareWithScopedTokensGovernanceAuditEnforcementOAuthChallengeAllowedReadAuditAndRoutePolicy(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	authEnforcementConfigured bool,
	oauthChallenge OAuthChallengePolicy,
	allowedAudit GovernanceAuditAppender,
	policy BrowserSessionRoutePolicy,
) http.Handler {
	return authMiddlewareWithAllowedReadAudit(
		token, resolver, nil, next, audit,
		policy, authEnforcementConfigured, oauthChallenge, allowedAudit,
	)
}

// AuthMiddlewareWithScopedTokensAndRoutePolicy is AuthMiddlewareWithScopedTokens
// plus an explicit route policy: shared-token compatibility, scoped-token
// resolution, no cookie sessions, no audit sink. It exists so a caller that
// needs only the bearer path can state the all-scope opening without also
// declaring a browser-session resolver it does not have, which the
// *WithBrowserSessions* constructors force.
func AuthMiddlewareWithScopedTokensAndRoutePolicy(
	token string,
	resolver ScopedTokenResolver,
	next http.Handler,
	policy BrowserSessionRoutePolicy,
) http.Handler {
	return authMiddlewareWithRoutePolicy(token, resolver, nil, next, nil, policy, token != "", nil, nil)
}

// ScopedRoutePolicyForGovernanceMode maps ESHU_GOVERNANCE_MODE onto the
// all-scope route opening. It opens whole-deployment reads to a tenant-bound
// all-scope caller only where one graph belongs to one local or hosted tenant.
// An empty mode is the established local_no_policy default. Unrecognized
// non-empty and hosted-multi-tenant modes stay fail-closed, because those
// handlers do not apply repository grants before counts and limits and no
// data-plane table carries a tenant column to fall back on.
//
// It lives here, not in cmd/api where it started, because cmd/mcp-server needs
// the same answer. Two commands deriving the same posture from the same
// environment variable through two private copies is how they drift, and the
// drift is silent: nothing fails, one surface just stays open a release longer
// than the other.
func ScopedRoutePolicyForGovernanceMode(governanceStatus GovernanceStatusConfig) BrowserSessionRoutePolicy {
	switch strings.TrimSpace(governanceStatus.Mode) {
	case "", "local_no_policy", "hosted_single_tenant":
		return BrowserSessionRoutePolicy{AllowTenantBoundAllScopes: true}
	default:
		return BrowserSessionRoutePolicy{}
	}
}

// The two helpers below are the governance status readback's half of the same
// ESHU_GOVERNANCE_MODE table ScopedRoutePolicyForGovernanceMode reads above.
// They sit here, next to it, rather than in status_governance.go, because the
// bug they close was the two halves living apart: admission fell to the
// fail-closed default on a mode it did not recognize while the readback
// rewrote that same value to local_no_policy and told the operator the
// deployment was permissive.

// governanceModeUnrecognized is what the readback reports for a non-empty
// ESHU_GOVERNANCE_MODE that is none of supported_modes. It is a readback
// state, never a value an operator sets: the env registry still allows only
// the three real modes.
//
// It exists because the readback used to fold such a value into
// "local_no_policy", the most permissive posture, while
// ScopedRoutePolicyForGovernanceMode was refusing every all-scope caller on a
// grant-bound route. An operator who mistyped the mode read back the posture
// they meant to configure and saw no sign of the refusals they were getting.
const governanceModeUnrecognized = "unrecognized"

// normalizeGovernanceMode keeps an unset mode on the documented
// "local_no_policy" default and reports anything else it does not recognize as
// governanceModeUnrecognized rather than rewriting it to a supported value.
func normalizeGovernanceMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return "local_no_policy"
	}
	return allowedOrDefault(mode, governanceModeUnrecognized,
		"local_no_policy", "hosted_single_tenant", "hosted_multi_tenant")
}

// governanceAllScopeRoutePolicy reports whether a tenant-bound all-scope
// credential is admitted on the routes whose handlers filter reads by the
// caller's repository or scope grant. It asks
// ScopedRoutePolicyForGovernanceMode, the same function cmd/api and
// cmd/mcp-server pass the same config to, so an operator reading the status
// route sees the decision the middleware is actually making.
func governanceAllScopeRoutePolicy(config GovernanceStatusConfig) map[string]any {
	state := "refused"
	if ScopedRoutePolicyForGovernanceMode(config).AllowTenantBoundAllScopes {
		state = "admitted"
	}
	return map[string]any{"grant_bound_routes": state}
}
