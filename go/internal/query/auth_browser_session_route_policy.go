// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// BrowserSessionRoutePolicy controls whether a tenant-bound all-scopes
// browser session may enter a route whose repository/scope filtering cannot
// bind it. That is two populations, not one: a route with no tenant filtering
// at all (absent from the scoped-token allowlist), and, since #6450, an
// allowlisted route whose own grant predicate goes inert for an all-scope
// caller -- the grant-bound, deployment-scoped, and transitive classes in
// scopedTokenAdvertisedRoutes. Identity-bound and tenant-data-free
// allowlisted routes hold no caller grant to make inert and are admitted
// without consulting this policy. Its zero value is fail-closed.
type BrowserSessionRoutePolicy struct {
	// AllowTenantBoundAllScopes opens both populations above to a browser
	// session that is all-scopes AND bound to one concrete tenant and
	// workspace (tenantBoundAllScopesBrowserSession). Leave it false unless
	// the runtime is provably local or single-tenant.
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

// browserSessionRouteAllowed decides whether a browser-session request may
// reach the handler. It has three outcomes.
//
// A shared-key-only route (POST /api/v0/code/cypher and friends, which run
// caller-supplied Cypher with no selector to intersect against a grant) is
// refused outright, whatever the session or the policy.
//
// A route that is not on the scoped-token allowlist has no tenant filtering
// at all, so it is admitted only under the explicit policy, and then only for
// the supported tenant-and-workspace-bound all-scopes console session.
//
// A route that IS on the allowlist splits by why it is there (#6450). A
// restricted session carries a real repository/scope grant and the handler
// binds it, so the session is admitted. An all-scope session has no such
// grant to bind: on an identity-bound or tenant-data-free route
// (scopedRouteNeedsNoCallerGrant) there was never a grant to make inert, so
// it is admitted; on a grant-bound, deployment-scoped, or transitive route
// the handler's own predicate goes inert and the request falls back to the
// same explicit policy the non-allowlisted routes use. Before #6450 every
// allowlisted route took the second branch's "true" unconditionally, so an
// all-scope console session read the whole graph on a grant-bound route in a
// hosted multi-tenant deployment that had deliberately left the policy at its
// fail-closed zero value.
//
// The all-scope fallback is a deliberate tightening as well as a loosening: a
// malformed tenantless all-scope session, which used to be admitted to every
// allowlisted route, is now refused on the grant-bound ones too, matching
// tenantBoundAllScopesBrowserSession's contract everywhere instead of only
// off the allowlist.
func browserSessionRouteAllowed(
	r *http.Request,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
) bool {
	if IsSharedKeyOnlyRoute(r) {
		return false
	}
	if !scopedHTTPRouteSupportsTenantFilter(r) {
		return policy.AllowTenantBoundAllScopes && tenantBoundAllScopesBrowserSession(auth)
	}
	if !auth.AllScopes {
		// The handler binds this session's own repository/scope grant.
		return true
	}
	if scopedRouteNeedsNoCallerGrant(r) {
		// Identity-bound or tenant-data-free: no caller grant to make inert.
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
// inert in the first place, so an all-scope session on it is not a
// cross-tenant read. browserSessionRouteAllowed, above, admits on exactly
// that split.
//
// A route added to the allowlist without an explicit class gets the zero
// value, scopedRouteGrantBound, which keeps it behind the
// BrowserSessionRoutePolicy mode check. That is deliberate: a contributor who
// forgets the class gets the fail-closed answer, not an all-scope opening.
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

// admitsAllScopesSessionWithoutPolicy reports whether an all-scope browser
// session may enter a route of this class regardless of
// BrowserSessionRoutePolicy. Only identity-bound and tenant-data-free routes
// qualify: they hold no caller grant that all-scope access could render
// inert. Grant-bound, deployment-scoped, and transitive routes all stay
// behind the policy's mode check.
func (c scopedRouteClass) admitsAllScopesSessionWithoutPolicy() bool {
	return c == scopedRouteIdentityBound || c == scopedRouteTenantDataFree
}

// scopedRouteNeedsNoCallerGrant is the request-shaped accessor for the same
// question admitsAllScopesSessionWithoutPolicy answers from the ledger: it is
// true for exactly the identity-bound and tenant-data-free populations.
//
// It is a closed union of the allowlist's existing matchers, NOT a path
// prefix test. A "/api/v0/auth/" prefix would be shorter and wrong: it would
// silently admit every future auth route, including one whose handler turns
// out to read tenant data, and it would miss the static catalog routes that
// live elsewhere in the path space. Every route that is not in this union --
// including the non-ledger MCP transport paths (GET /sse, POST /mcp/message)
// -- is policy-gated for all-scope sessions, which is the fail-closed
// default.
//
// TestScopedRouteClassLedgerAgreesWithPredicate keeps this function and the
// scopedTokenAdvertisedRoutes classes in lockstep: adding a matcher here
// without reclassifying the route, or reclassifying a route without wiring
// its matcher, fails that test.
func scopedRouteNeedsNoCallerGrant(r *http.Request) bool {
	switch {
	case scopedTOTPEnrollmentRoute(r):
		return true
	case scopedBrowserSessionAuthRoute(r):
		return true
	case scopedLocalIdentityAPITokenRoute(r):
		return true
	case scopedAuthProfileReadRoute(r):
		return true
	case scopedAuthAdminReadRoute(r):
		return true
	case scopedAuthAdminMutationRoute(r):
		return true
	case scopedCapabilityCatalogRoute(r):
		return true
	case scopedSurfaceInventoryRoute(r):
		return true
	case scopedQueryPlaybookRoute(r):
		return true
	case scopedInvestigationWorkflowRoute(r):
		return true
	case scopedFactSchemaVersionRoute(r):
		return true
	case scopedVulnerabilityScannerContractRoute(r):
		return true
	case scopedCollectorExtractionReadinessRoute(r):
		return true
	// POST /api/v0/visualizations/derive has no named matcher of its own in
	// scopedHTTPRouteSupportsTenantFilter: VisualizationHandler holds no
	// graph, content, or store reference and only reshapes the caller's own
	// source_response (#5167 task 4), so it is tenant-data-free.
	case r.Method == http.MethodPost && r.URL.Path == "/api/v0/visualizations/derive":
		return true
	default:
		// The break-it-to-prove-it run recorded in
		// docs/internal/evidence/6450-all-scope-browser-session-admission.md
		// flips the literal below to true, which reopens the pre-#6450
		// admission on every allowlisted route and confirms the #6450
		// regression tests go red. The trailing marker is what that run
		// seds on, so keep it on the same line as the literal.
		return false // bites-6450-neuter-anchor
	}
}
