// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6818 move 4b: root auth surface (aliases, forwarders, route policies, handler wiring) stays in package query; moving it into auth/ strands root handler callers and turns this rename into a root-surface relocation, which is a separate follow-up.

import "net/http"

// scopedRouteDeniedUnspecifiedReason is the defensive fallback for a blank
// governance-audit reason code. It is unreachable from auth's
// BrowserSessionRouteDenialReason, which never returns blank for a refusal;
// seeing it in the audit means a new caller passed an empty code, and it is
// deliberately distinct from auth's two real codes
// (ScopedRouteNotEnabledReason, ScopedRouteAllScopeGrantRequiredReason) so
// that shows up rather than hiding inside one of them. It stays in root
// because its only emitter, recordScopedRouteAuthorizationDeniedWithReason
// in auth_audit.go, stays in root with the rest of the audit surface.
const scopedRouteDeniedUnspecifiedReason = "scoped_route_denied_unspecified"

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
	return authMiddlewareWithRoutePolicy(token, resolver, sessionResolver, next, audit, policy, token != "", nil, nil)
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
	return authMiddlewareWithRoutePolicy(token, resolver, sessionResolver, next, audit, policy, authEnforcementConfigured, nil, nil)
}

// scopedRouteClass records WHY a route is on the scoped-token allowlist
// (scopedTokenAdvertisedRoutes in auth_scoped_routes_completeness.go). Being
// on that allowlist means a tenant-filtered caller may enter the route; the
// class says what makes that safe, and #6450 is why the distinction has to
// exist. A grant-bound handler is safe for a scoped caller because it
// intersects every read with the caller's repository/scope grant -- but an
// all-scope caller's grant is inert
// (querycontract.RepositoryAccessFilterFromContext returns AllScopes:true,
// whose Scoped() is false), so the same handler answers from the whole graph,
// and no data-plane table carries a tenant column to fall back on. An
// identity-bound or tenant-data-free handler has no caller grant to make
// inert in the first place, so admitting an all-scope session to it does not
// widen a read the way it does on a grant-bound route: the handler answers
// from the tenant and workspace the session is CURRENTLY bound to.
// browserSessionRouteDenialReason and scopedBearerRouteDenialReason, above,
// both admit on exactly that split -- the same class, read by both credential
// kinds, since #6450's residual item 1 closed.
//
// "Currently bound to" is doing real work in that sentence, and two known
// residuals are the reason it is not the stronger claim that an all-scope
// session on these routes can never reach another tenant's data. Both are
// tracked on #6450 and neither is fixed here.
//
//  1. PATCH /api/v0/auth/browser-session/context is itself identity-bound,
//     and it takes the target tenant and workspace from the request body.
//     switchBrowserSessionWorkspaceQuery
//     (storage/postgres/browser_sessions_schema.go) gates the update on
//     sess.all_scopes = true and on the target being active, but binds
//     nothing about the session's SUBJECT to that tenant, so an all-scope
//     session can change which tenant it is bound to and then read the new
//     one through these same routes. (#6450 item 4.)
//  2. localIdentityAPITokenScope (local/api_tokens.go) falls back to
//     a body-supplied tenant and workspace when AuthContext carries neither,
//     and selfServiceTokenOwner (local/api_tokens_self_service.go)
//     returns an empty owner hash for any all-scope caller, dropping the
//     ownership predicate. The demonstrated exposure there is token minting
//     for any tenant-less credential, a shared key being the example the
//     auth-slice finding names -- not a browser session. Whether a TENANTLESS
//     all-scope browser session can exist at all is NOT established: the OIDC
//     upgrade path rejects a blank tenant or workspace
//     (browser_session_handler.go, "tenant_id and workspace_id are required
//     to create a browser session") and SAML does the same (saml_handler.go's
//     createSession), but issueLocalSessionCookies
//     (local/helpers.go), shared by local login, break-glass
//     and the setup wizard, copies auth.TenantID and auth.WorkspaceID through
//     with no non-blank guard, and the CreateBrowserSession choke point
//     validates neither. Caller shape (f) in the split table is therefore a
//     DEFENSIVE shape -- it pins what admission does with a malformed session
//     if one ever exists -- not a known-live one, and resolving the
//     reachability question is deliberately left to #6450 item 2 of the
//     auth-slice findings rather than guessed at here.
//
// The class split is still the right admission rule: it removes the
// whole-graph grant-bound read, which was the reported defect. It does not
// by itself make the identity-bound population airtight, and this comment
// should not be read as claiming that it does.
//
// A route added to the allowlist without an explicit class gets the zero
// value, scopedRouteGrantBound, which keeps it behind the
// BrowserSessionRoutePolicy mode check for both credential kinds. That is
// deliberate: a contributor who forgets the class gets the fail-closed
// answer, not an all-scope opening.
//
// This class is NOT an OpenAPI marker, and deliberately so. The markers
// ("x-scoped-token-support", "x-browser-session-only", "x-shared-key-only")
// declare WHO may call a route, which is part of the published contract. The
// class declares WHY the route is safe for that caller, which is an internal
// admission fact with no wire meaning; publishing it would invite clients to
// depend on an implementation detail of the auth middleware.
type scopedRouteClass int

const (
	// scopedRouteGrantBound is a handler that binds the caller's
	// repository/scope grant; an all-scope caller makes that binding inert.
	// Zero value on purpose, so an unclassified route fails closed.
	scopedRouteGrantBound scopedRouteClass = iota
	// scopedRouteIdentityBound is a handler that derives the subject,
	// tenant, or workspace from AuthContext and confines its read or write
	// to that identity. It needs no caller grant: the /api/v0/auth/ admin
	// and identity population.
	scopedRouteIdentityBound
	// scopedRouteTenantDataFree is a static in-binary artifact, or a pure
	// reshape of the caller's own request body. It carries no tenant data.
	scopedRouteTenantDataFree
	// scopedRouteDeploymentScoped is a deployment-wide runtime or operator
	// status read that takes no grant and redacts only by auth Mode. It is
	// treated like grant-bound for all-scope admission: the Mode-based
	// redaction in status_scoped.go is the only thing between the caller and
	// the deployment's full runtime posture, so the policy check stays.
	scopedRouteDeploymentScoped
	// scopedRouteTransitive is a handler that reads nothing itself and
	// dispatches inner calls back through this middleware (POST
	// /api/v0/ask). It is treated like grant-bound for all-scope admission,
	// because the inner calls inherit the same all-scope context.
	scopedRouteTransitive
)

// admitsAllScopesSessionWithoutPolicy reports whether an all-scope caller --
// cookie session or bearer token -- may enter a route of this class
// regardless of BrowserSessionRoutePolicy. Only identity-bound and
// tenant-data-free routes qualify: they hold no caller grant that all-scope
// access could render inert. Grant-bound, deployment-scoped, and transitive
// routes all stay behind the policy's mode check.
func (c scopedRouteClass) admitsAllScopesSessionWithoutPolicy() bool {
	return c == scopedRouteIdentityBound || c == scopedRouteTenantDataFree
}
