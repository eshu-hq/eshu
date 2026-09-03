// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// BrowserSessionRoutePolicy controls whether a tenant-bound all-scopes caller
// may enter a route whose repository/scope filtering cannot bind it. That is
// two populations, not one: a route with no tenant filtering at all (absent
// from the scoped-token allowlist), and, since #6450, an allowlisted route
// whose own grant predicate goes inert for an all-scope caller -- the
// grant-bound, deployment-scoped, and transitive classes in
// scopedTokenAdvertisedRoutes. Identity-bound and tenant-data-free
// allowlisted routes hold no caller grant to make inert and are admitted
// without consulting this policy, confined to the tenant the caller is
// currently bound to; see scopedRouteClass below for the two residuals that
// qualification is protecting against. Its zero value is fail-closed.
//
// The name is narrower than the reach. It was introduced for the cookie
// console session and now governs the all-scope BEARER population too --
// #6450's residual item 1, closed by scopedBearerRouteDenialReason below --
// because an all-scope OIDC or registry bearer makes the same grant
// predicates inert on the same routes, and answering it from the whole corpus
// in a hosted multi-tenant deployment is the same defect with a different
// credential. The type keeps its name so the ~50 call sites and the committed
// #6450 evidence that cite it by symbol stay true; read it as "the policy for
// all-scope callers on grant-bound routes".
type BrowserSessionRoutePolicy struct {
	// AllowTenantBoundAllScopes opens both populations above to a caller that
	// is all-scopes AND bound to one concrete tenant and workspace
	// (tenantBoundAllScopes). For a bearer it opens only the second
	// population: a bearer never reaches a route off the scoped-token
	// allowlist whatever this field says. Leave it false unless the runtime
	// is provably local or single-tenant.
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

// Governance-audit reason codes for the two ways a browser session is refused
// a route. They are separate because an operator reading
// governance_audit_events has to act on them differently, and because after
// #6450 the older code would otherwise be wrong for half the refusals it
// covered.
const (
	// scopedRouteNotEnabledReason is the pre-#6450 code, and still means what
	// it always meant: the route has no scoped authorization for this caller
	// at all. Either it is shared-key-only, or it is absent from the
	// scoped-token allowlist. The remedy is to wire the route up, or to stop
	// pointing a cookie session at it.
	scopedRouteNotEnabledReason = "scoped_route_not_enabled"
	// scopedRouteAllScopeGrantRequiredReason is the #6450 code. The route IS
	// enabled -- a restricted session with a real repository/scope grant
	// enters it and gets grant-bound results -- but this caller is all-scope,
	// so the handler's own predicate would go inert and answer from the whole
	// graph. The remedy is a narrower credential, or an explicit
	// BrowserSessionRoutePolicy opt-in on a deployment where whole-graph
	// reads are acceptable. Emitting scopedRouteNotEnabledReason here would
	// send an operator to look for a missing allowlist entry that is present.
	scopedRouteAllScopeGrantRequiredReason = "scoped_route_all_scope_grant_required"
	// scopedRouteDeniedUnspecifiedReason is the defensive fallback for a
	// blank reason code. It is unreachable from browserSessionRouteDenialReason,
	// which never returns blank for a refusal; seeing it in the audit means a
	// new caller passed an empty code, and it is deliberately distinct so that
	// shows up rather than hiding inside one of the two real codes.
	scopedRouteDeniedUnspecifiedReason = "scoped_route_denied_unspecified"
)

// browserSessionRouteDenialReason decides whether a browser-session request
// may reach the handler, and says why when it may not: it returns "" for an
// admitted request, and otherwise the governance-audit reason code for the
// refusal. There are three outcomes.
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
// allowlisted route, is now refused on the grant-bound, deployment-scoped and
// transitive ones as well as off the allowlist, which is where
// tenantBoundAllScopesBrowserSession's contract now holds. It is still
// admitted on the 48 identity-bound and tenant-data-free routes, so this
// closes the whole-graph read, not every path a tenantless session has.
//
// The decision and its reason are one function on purpose. They were briefly
// two -- a boolean admission test plus a reason lookup -- and that shape lets
// them disagree, which is the worst possible failure here: a request admitted
// while an audit row says it was denied, or the reverse. One return value
// cannot drift from itself.
func browserSessionRouteDenialReason(
	r *http.Request,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
) string {
	if IsSharedKeyOnlyRoute(r) {
		return scopedRouteNotEnabledReason
	}
	if !scopedHTTPRouteSupportsTenantFilter(r) {
		if policy.AllowTenantBoundAllScopes && tenantBoundAllScopesBrowserSession(auth) {
			return ""
		}
		return scopedRouteNotEnabledReason
	}
	if !auth.AllScopes {
		// The handler binds this session's own repository/scope grant.
		return ""
	}
	if scopedRouteNeedsNoCallerGrant(r) {
		// Identity-bound or tenant-data-free: no caller grant to make inert.
		return ""
	}
	if policy.AllowTenantBoundAllScopes && tenantBoundAllScopesBrowserSession(auth) {
		return ""
	}
	return scopedRouteAllScopeGrantRequiredReason
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
	return auth.Mode == AuthModeBrowserSession && tenantBoundAllScopes(auth)
}

// tenantBoundAllScopes is the mode-neutral half of the test above: an
// all-scope caller bound to one concrete tenant and workspace. Both admission
// paths need it -- the cookie session through the wrapper above, and the
// scoped bearer through scopedBearerRouteDenialReason -- and they must agree
// on what "tenant-bound" means, so it is one function rather than two
// copies that can drift apart on a blank-string check.
//
// The blank checks are not defensive padding on the bearer path either, even
// though scopedtoken.Entry.normalize rejects an entry with no tenant_id or
// workspace_id and oidcbearer.Resolver copies both from the provider config.
// The AuthContext this sees came through a ScopedTokenResolver interface, and
// nothing in the type system says every implementation of that interface
// enforces what those two do.
func tenantBoundAllScopes(auth AuthContext) bool {
	return auth.AllScopes &&
		strings.TrimSpace(auth.TenantID) != "" &&
		strings.TrimSpace(auth.WorkspaceID) != ""
}

// scopedBearerRouteDenialReason is browserSessionRouteDenialReason's sibling
// for a scoped bearer token -- an ESHU_SCOPED_TOKENS_FILE registry entry or an
// OIDC bearer resolved to AuthModeScoped. It returns "" for an admitted
// request and otherwise the governance-audit reason code for the refusal, in
// the same single-return-value shape and for the same reason: an admission
// decision and the audit row explaining it must not be able to disagree.
//
// It closes #6450's residual item 1. Before it, the whole bearer gate was
// scopedHTTPRouteSupportsTenantFilter -- allowlist membership alone -- so a
// bearer carrying AllScopes entered every grant-bound allowlisted route with
// its grant predicate inert (querycontract.RepositoryAccessFilterFromContext
// returns AllScopes:true, whose Scoped() is false) and read every tenant's
// rows, in hosted_multi_tenant as readily as on a laptop.
//
// It differs from the browser-session function in exactly two ways, and both
// are deliberate.
//
// It has no policy escape off the allowlist. A bearer on a route with no
// tenant filtering at all is refused whatever the policy says, which is the
// pre-existing bearer behaviour and stays that way: the routes still on
// pendingRowFilteringRoutes (GET /api/v0/freshness/services/changed-since
// among them) answer a bearer with a 403 in every deployment, and this change
// must not quietly promote them. The console session's off-allowlist opening
// is a dashboard affordance, not a token one.
//
// It needs no IsSharedKeyOnlyRoute branch. A shared-key-only route is by
// definition absent from the allowlist, so the first check already refuses it
// with the same scoped_route_not_enabled code the browser-session path emits
// there.
//
// A restricted bearer -- one carrying real repository or scope ids -- is
// untouched: the handler binds its grant, so it is admitted on every
// allowlisted route exactly as before.
func scopedBearerRouteDenialReason(
	r *http.Request,
	auth AuthContext,
	policy BrowserSessionRoutePolicy,
) string {
	if !scopedHTTPRouteSupportsTenantFilter(r) {
		return scopedRouteNotEnabledReason
	}
	if !auth.AllScopes {
		// The handler binds this token's own repository/scope grant.
		return ""
	}
	if scopedRouteNeedsNoCallerGrant(r) {
		// Identity-bound or tenant-data-free: no caller grant to make inert.
		return ""
	}
	if policy.AllowTenantBoundAllScopes && tenantBoundAllScopes(auth) {
		return ""
	}
	// The break-it-to-prove-it run recorded in
	// docs/internal/evidence/5167-freshness-family-allowlist.md flips the
	// literal below to "", which restores the unconditional admission an
	// all-scope bearer had before this function existed, and must turn the
	// hosted_multi_tenant refusal cases red. The trailing marker is what that run seds on, so keep
	// it on the same line as the literal.
	return scopedRouteAllScopeGrantRequiredReason // bites-6450-bearer-neuter-anchor
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
//  2. localIdentityAPITokenScope (local_identity_api_tokens.go) falls back to
//     a body-supplied tenant and workspace when AuthContext carries neither,
//     and selfServiceTokenOwner (local_identity_api_tokens_selfservice.go)
//     returns an empty owner hash for any all-scope caller, dropping the
//     ownership predicate. The demonstrated exposure there is token minting
//     for any tenant-less credential, a shared key being the example the
//     auth-slice finding names -- not a browser session. Whether a TENANTLESS
//     all-scope browser session can exist at all is NOT established: the OIDC
//     upgrade path rejects a blank tenant or workspace
//     (browser_session_handler.go, "tenant_id and workspace_id are required
//     to create a browser session") and SAML does the same (saml_handler.go's
//     createSession), but issueLocalSessionCookies
//     (local_identity_handler_helpers.go), shared by local login, break-glass
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
// -- is policy-gated for all-scope callers of either kind, which is the
// fail-closed default.
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
	case scopedVisualizationDeriveRoute(r):
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
