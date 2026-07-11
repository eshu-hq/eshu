// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

func scopedHTTPRouteSupportsTenantFilter(r *http.Request) bool {
	// Only add routes here after the handler filters counts, limits,
	// truncation, ambiguity, and not-found metadata from AuthContext.
	//
	// Self-service TOTP MFA enrollment (issue #4986, PR #5065 review): these
	// are auth mutations, not tenant-filtered reads, but they require an
	// authenticated identity and this allowlist is what AuthMiddleware checks
	// before admitting a browser-session (and scoped-token) request. The
	// handlers resolve the acting user from the AuthContext session subject and
	// never accept a body-supplied target user, so a caller with no resolvable
	// local identity is rejected in the handler. Without this, AuthMiddleware
	// rejects the profile-page enrollment before the handler runs.
	if r.Method == http.MethodPost &&
		(r.URL.Path == "/api/v0/auth/local/mfa/totp/begin" ||
			r.URL.Path == "/api/v0/auth/local/mfa/totp/confirm") {
		return true
	}
	if r.Method == http.MethodGet && r.URL.Path == "/api/v0/repositories" {
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v0/code/search" {
		return true
	}
	if scopedCodeFlowRoute(r) {
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v0/code/routes/callers" {
		return true
	}
	// POST /api/v0/ask orchestrates other read routes through the in-process MCP
	// runner; it holds no graph query of its own. Its tenant scoping is enforced
	// transitively: the runner re-dispatches each inner tool call through this
	// same auth middleware under the caller's token, so a scoped caller can only
	// reach routes that are themselves in this allowlist. A tool mapped to a
	// non-listed route (e.g. GET /api/v0/ecosystem/overview) returns 403 to the
	// runner instead of leaking cross-scope data.
	if r.Method == http.MethodPost && r.URL.Path == "/api/v0/ask" {
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v0/entities/resolve" {
		return true
	}
	if r.Method == http.MethodGet && scopedEntityContextRoute(r.URL.Path) {
		return true
	}
	if r.Method == http.MethodGet && scopedWorkloadContextRoute(r.URL.Path) {
		return true
	}
	if r.Method == http.MethodGet && scopedServiceContextRoute(r.URL.Path) {
		return true
	}
	if r.Method == http.MethodGet && scopedServiceInvestigationRoute(r.URL.Path) {
		return true
	}
	if r.Method == http.MethodGet && scopedServiceIntelligenceReportRoute(r.URL.Path) {
		return true
	}
	if r.Method == http.MethodGet && scopedIncidentContextRoute(r.URL.Path) {
		return true
	}
	if scopedQueryPlaybookRoute(r) {
		return true
	}
	if scopedInvestigationWorkflowRoute(r) {
		return true
	}
	if scopedCapabilityCatalogRoute(r) {
		return true
	}
	if scopedSurfaceInventoryRoute(r) {
		return true
	}
	if scopedBrowserSessionAuthRoute(r) {
		return true
	}
	if scopedLocalIdentityAPITokenRoute(r) {
		return true
	}
	if scopedAuthProfileReadRoute(r) {
		return true
	}
	if scopedAuthAdminReadRoute(r) {
		return true
	}
	if scopedAuthAdminMutationRoute(r) {
		return true
	}
	if scopedVulnerabilityScannerContractRoute(r) {
		return true
	}
	if scopedSupplyChainImpactRoute(r) {
		return true
	}
	if scopedInvestigationPacketRoute(r) {
		return true
	}
	if scopedSupplyChainAdvisoryEvidenceRoute(r) {
		return true
	}
	if scopedSecretsIAMRoute(r) {
		return true
	}
	if scopedHostedGovernanceStatusRoute(r) {
		return true
	}
	if scopedHostedReadinessRoute(r) {
		return true
	}
	if scopedOperatorControlPlaneRoute(r) {
		return true
	}
	if scopedDeadLetterListRoute(r) {
		return true
	}
	if scopedFreshnessCausalityRoute(r) {
		return true
	}
	if scopedFactSchemaVersionRoute(r) {
		return true
	}
	if scopedSemanticExtractionStatusRoute(r) {
		return true
	}
	if scopedAnswerNarrationStatusRoute(r) {
		return true
	}
	if scopedSemanticEvidenceRoute(r) {
		return true
	}
	if scopedSemanticSearchRoute(r) {
		return true
	}
	if scopedDocumentationListRoute(r) {
		return true
	}
	if scopedDocumentationAggregateRoute(r) {
		return true
	}
	if scopedDocumentationEvidencePacketRoute(r) {
		return true
	}
	if scopedServiceCatalogCorrelationRoute(r) {
		return true
	}
	if scopedPackageRegistryCorrelationRoute(r) {
		return true
	}
	if scopedPackageRegistryDependencyChainsRoute(r) {
		return true
	}
	if scopedAdmissionDecisionRoute(r) {
		return true
	}
	if scopedCICDRunCorrelationRoute(r) {
		return true
	}
	if scopedContainerImageIdentityRoute(r) {
		return true
	}
	if scopedComponentExtensionRoute(r) {
		return true
	}
	if scopedCollectorExtractionReadinessRoute(r) {
		return true
	}
	if scopedCollectorStatusRoute(r) {
		return true
	}
	if scopedCollectorReadinessRoute(r) {
		return true
	}
	if scopedIngesterStatusRoute(r) {
		return true
	}
	if scopedSBOMAttestationAttachmentRoute(r) {
		return true
	}
	if scopedSecurityAlertReconciliationRoute(r) {
		return true
	}
	if scopedInfraResourceAggregateRoute(r) {
		return true
	}
	if scopedInfraSearchRoute(r) {
		return true
	}
	if scopedInfraRelationshipsRoute(r) {
		return true
	}
	if scopedIaCResourceListRoute(r) {
		return true
	}
	if scopedWorkItemEvidenceRoute(r) {
		return true
	}
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/content/files/read",
		"/api/v0/content/files/lines",
		"/api/v0/content/entities/read",
		"/api/v0/content/files/search",
		"/api/v0/content/entities/search",
		"/api/v0/evidence/citations":
		return true
	default:
		return false
	}
}

func scopedLocalIdentityAPITokenRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	if r.URL.Path == "/api/v0/auth/local/api-tokens" {
		return true
	}
	const prefix = "/api/v0/auth/local/api-tokens/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	tokenLifecycleAction := strings.TrimPrefix(r.URL.Path, prefix)
	tokenID, action, ok := strings.Cut(tokenLifecycleAction, "/")
	if !ok || tokenID == "" || strings.Contains(tokenID, "/") {
		return false
	}
	return action == "revoke" || action == "rotate"
}

// scopedAuthProfileReadRoute reports whether the request targets one of the
// caller's own identity-profile read endpoints. These GETs derive the subject
// strictly from AuthContext and return only the caller's own profile, sessions,
// or generated API token metadata (never another subject's data and never any
// secret), so they are safe for browser-session and scoped-token callers.
func scopedAuthProfileReadRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/auth/profile",
		"/api/v0/auth/sessions",
		"/api/v0/auth/local/api-tokens":
		return true
	default:
		return false
	}
}

// scopedAuthAdminReadRoute reports whether the request targets one of the
// tenant-admin identity read endpoints. These GETs derive the tenant/workspace
// strictly from AuthContext and return only metadata within the caller's own
// tenant (never another tenant's data and never any secret). The handler
// additionally requires all-scope admin auth, so a non-admin browser-session or
// scoped-token caller that reaches here is still denied by adminScope. Admins
// drive the console with cookie sessions, which the auth middleware treats as
// tenant-filter eligible only for routes in this allowlist, so these routes
// must be listed for the admin console to function.
//
// The audit routes (/audit/events, /audit/summary) are listed here (#3717):
// they now support two caller classes via auditScope() in the handler —
// shared-operator (AuthModeShared, no tenant filter, sees all events) and
// tenant admin (AllScopes + TenantID, sees only own-tenant events). Listing
// them here allows browser-session tenant admins to reach the handler; the
// handler's auditScope gate enforces the correct scoping.
//
// The provider-config routes (#4966) are matched by scopedProviderConfigReadRoute
// rather than a literal case here because they carry a {provider_config_id}
// path parameter and a "/revisions" sub-resource (issue #5004 follow-up: this
// whole family was missing from the allowlist, the same gap sign-in-policy
// had).
func scopedAuthAdminReadRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/auth/local/invitations",
		"/api/v0/auth/admin/role-assignments",
		"/api/v0/auth/admin/roles",
		"/api/v0/auth/admin/idp-providers",
		"/api/v0/auth/admin/idp-group-mappings",
		"/api/v0/auth/admin/api-tokens",
		"/api/v0/auth/admin/audit/events",
		"/api/v0/auth/admin/audit/summary",
		"/api/v0/auth/admin/sign-in-policy":
		return true
	}
	return scopedProviderConfigReadRoute(r.URL.Path)
}

// scopedAuthAdminMutationRoute reports whether the request targets one of the
// tenant-admin identity mutation endpoints (#3703 PR-2). These unsafe-method
// (POST/PATCH/DELETE) routes derive the tenant/workspace strictly from AuthContext and
// write only within the caller's own tenant. The handler additionally requires
// all-scope admin auth, so a non-admin browser-session or scoped-token caller
// that reaches here is still denied by adminScope. Admins drive the console with
// cookie sessions, which the auth middleware treats as tenant-filter eligible
// only for routes in this allowlist, so these routes must be listed for the
// admin console to mutate.
//
// CSRF: these are unsafe methods. The browser-session auth middleware enforces
// CSRF for any non-GET/HEAD/OPTIONS/TRACE method before the request reaches the
// handler (browserSessionRequiresCSRF), so listing a mutation here does not relax
// CSRF — a cookie-session caller without a valid X-Eshu-CSRF header is rejected
// with 403 ahead of the handler. Scoped bearer tokens are not subject to CSRF
// and are still gated by the all-scope admin requirement in the handler.
//
// The provider-config POST routes (#4966) are matched by
// scopedProviderConfigMutationRoute rather than a literal case here because
// they carry a {provider_config_id} path parameter and the
// /revert, /enable, /disable, /test-connection sub-resource actions (issue
// #5004 follow-up).
func scopedAuthAdminMutationRoute(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost:
		switch r.URL.Path {
		case "/api/v0/auth/admin/role-assignments",
			"/api/v0/auth/admin/role-assignments/revoke",
			"/api/v0/auth/admin/idp-group-mappings":
			return true
		}
		if scopedInvitationRevokeRoute(r.URL.Path) {
			return true
		}
		return scopedProviderConfigMutationRoute(r.URL.Path)
	case http.MethodPatch:
		return r.URL.Path == "/api/v0/auth/admin/sign-in-policy"
	case http.MethodDelete:
		return scopedIdPGroupMappingDeleteRoute(r.URL.Path)
	default:
		return false
	}
}

// scopedInvitationRevokeRoute matches POST /api/v0/auth/local/invitations/{invite_id}/revoke.
func scopedInvitationRevokeRoute(path string) bool {
	const (
		prefix = "/api/v0/auth/local/invitations/"
		suffix = "/revoke"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	inviteID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return inviteID != "" && !strings.Contains(inviteID, "/")
}

// scopedIdPGroupMappingDeleteRoute matches DELETE /api/v0/auth/admin/idp-group-mappings/{mapping_ref}.
func scopedIdPGroupMappingDeleteRoute(path string) bool {
	const prefix = "/api/v0/auth/admin/idp-group-mappings/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	mappingRef := strings.TrimPrefix(path, prefix)
	return mappingRef != "" && !strings.Contains(mappingRef, "/")
}

// providerConfigsListPath is the admin provider-config collection path both
// scopedProviderConfigReadRoute and scopedProviderConfigMutationRoute anchor
// on: the bare path is the list (GET) / create (POST) route, and every other
// matched route is a "/{provider_config_id}[/action]" path under it.
const providerConfigsListPath = "/api/v0/auth/admin/provider-configs"

// scopedProviderConfigReadRoute matches the admin provider-config GET routes:
// the list (GET providerConfigsListPath), a single provider config
// (GET .../{provider_config_id}), and its revision history
// (GET .../{provider_config_id}/revisions). Uses the same
// prefix-then-single-segment approach as scopedIdPGroupMappingDeleteRoute,
// extended with strings.Cut to also recognize the one nested "/revisions"
// sub-resource without a naive HasPrefix that would let any deeper path
// through.
func scopedProviderConfigReadRoute(path string) bool {
	if path == providerConfigsListPath {
		return true
	}
	const prefix = providerConfigsListPath + "/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	remainder := strings.TrimPrefix(path, prefix)
	providerConfigID, sub, hasSub := strings.Cut(remainder, "/")
	if providerConfigID == "" {
		return false
	}
	if !hasSub {
		return true
	}
	return sub == "revisions"
}

// scopedProviderConfigMutationRoute matches the admin provider-config POST
// routes: create (POST providerConfigsListPath), update
// (POST .../{provider_config_id}), and the revert/enable/disable/
// test-connection sub-resource actions
// (POST .../{provider_config_id}/{action}). Same prefix-then-segment approach
// as scopedProviderConfigReadRoute; the action segment is matched against the
// closed set of implemented actions rather than accepted as a wildcard.
func scopedProviderConfigMutationRoute(path string) bool {
	if path == providerConfigsListPath {
		return true
	}
	const prefix = providerConfigsListPath + "/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	remainder := strings.TrimPrefix(path, prefix)
	providerConfigID, action, hasAction := strings.Cut(remainder, "/")
	if providerConfigID == "" {
		return false
	}
	if !hasAction {
		return true
	}
	switch action {
	case "revert", "enable", "disable", "test-connection":
		return true
	default:
		return false
	}
}

func scopedBrowserSessionAuthRoute(r *http.Request) bool {
	switch {
	case r.URL.Path == "/api/v0/auth/browser-session" &&
		(r.Method == http.MethodGet || r.Method == http.MethodPost || r.Method == http.MethodDelete):
		return true
	case r.URL.Path == "/api/v0/auth/browser-session/context" && r.Method == http.MethodPatch:
		return true
	default:
		return false
	}
}
