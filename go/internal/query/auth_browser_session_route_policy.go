// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// BrowserSessionRoutePolicy controls whether a tenant-bound all-scopes
// browser session may enter routes that do not yet implement repository or
// scope filtering. Its zero value is fail-closed.
type BrowserSessionRoutePolicy struct {
	AllowTenantBoundAllScopes bool
}

// AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy
// wraps every supported authentication mode and applies the explicit browser
// session route policy. Callers must leave the zero-value policy in place
// unless their runtime is provably local or single-tenant.
//
// This constructor derives dev-mode-open from the shared key only. Production
// wiring that may configure a scoped-token file or OIDC bearer audience
// without a shared key MUST use
// AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement
// instead.
func AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditAndRoutePolicy(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	policy BrowserSessionRoutePolicy,
) http.Handler {
	return authMiddlewareWithRoutePolicy(token, resolver, sessionResolver, next, audit, policy, token != "")
}

// AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement
// is the production variant used by cmd/api. Unlike the constructor above —
// which derives dev-mode-open from the shared key alone — it takes the
// explicit wiring-computed authEnforcementConfigured predicate (shared key OR
// scoped-token file OR OIDC bearer audience configured) so a
// scoped-token-file-only or OIDC-bearer-only deployment, with no shared
// ESHU_API_KEY, still denies headerless requests. The browser-session resolver
// is deliberately not part of that predicate: the cookie path self-enforces
// before the dev-open branch, so a cookieless headerless request in the open
// posture stays open. See cmd/api/wiring.go and cmd/api/browser_sessions.go.
func AuthMiddlewareWithBrowserSessionsScopedTokensGovernanceAuditRoutePolicyAndEnforcement(
	token string,
	resolver ScopedTokenResolver,
	sessionResolver BrowserSessionResolver,
	next http.Handler,
	audit GovernanceAuditAppender,
	policy BrowserSessionRoutePolicy,
	authEnforcementConfigured bool,
) http.Handler {
	return authMiddlewareWithRoutePolicy(token, resolver, sessionResolver, next, audit, policy, authEnforcementConfigured)
}

func browserSessionRouteAllowed(
	r *http.Request,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
) bool {
	if scopedHTTPRouteSupportsTenantFilter(r) {
		return true
	}
	return policy.AllowTenantBoundAllScopes && tenantBoundAllScopesBrowserSession(auth)
}

// tenantBoundAllScopesBrowserSession reports whether the server-resolved
// browser session is the supported owner/admin session for one concrete
// tenant and workspace. This admits the normal single-tenant console workflow
// without granting the same whole-graph access to scoped bearer tokens,
// restricted browser sessions, or malformed tenantless admin contexts.
//
// This is not hosted multi-tenant graph isolation. Such deployments still
// require handler-level scope predicates before enabling a shared graph across
// tenants; see docs/internal/design/1902-tenant-workspace-isolation.md.
func tenantBoundAllScopesBrowserSession(auth AuthContext) bool {
	return auth.Mode == AuthModeBrowserSession &&
		auth.AllScopes &&
		strings.TrimSpace(auth.TenantID) != "" &&
		strings.TrimSpace(auth.WorkspaceID) != ""
}
