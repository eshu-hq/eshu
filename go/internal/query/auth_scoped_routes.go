package query

import (
	"net/http"
	"strings"
)

func scopedHTTPRouteSupportsTenantFilter(r *http.Request) bool {
	// Only add routes here after the handler filters counts, limits,
	// truncation, ambiguity, and not-found metadata from AuthContext.
	if r.Method == http.MethodGet && r.URL.Path == "/api/v0/repositories" {
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v0/code/search" {
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
	if scopedHostedGovernanceStatusRoute(r) {
		return true
	}
	if scopedHostedReadinessRoute(r) {
		return true
	}
	if scopedOperatorControlPlaneRoute(r) {
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
// The audit routes (/audit/events, /audit/summary) are intentionally excluded:
// they expose GLOBAL cross-tenant data and require AuthModeShared, not a
// browser-session tenant context. Shared-operator callers hold bearer tokens
// that bypass the tenant-filter gate entirely. See #3717 for per-tenant audit.
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
		"/api/v0/auth/admin/api-tokens":
		return true
	default:
		return false
	}
}

// scopedAuthAdminMutationRoute reports whether the request targets one of the
// tenant-admin identity mutation endpoints (#3703 PR-2). These unsafe-method
// (POST/DELETE) routes derive the tenant/workspace strictly from AuthContext and
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
func scopedAuthAdminMutationRoute(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost:
		switch r.URL.Path {
		case "/api/v0/auth/admin/role-assignments",
			"/api/v0/auth/admin/role-assignments/revoke",
			"/api/v0/auth/admin/idp-group-mappings":
			return true
		}
		return scopedInvitationRevokeRoute(r.URL.Path)
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

func scopedEntityContextRoute(path string) bool {
	const (
		prefix = "/api/v0/entities/"
		suffix = "/context"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	entityID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return entityID != "" && !strings.Contains(entityID, "/")
}

// scopedIncidentContextRoute reports whether the request targets the
// single-incident context read GET /api/v0/incidents/{incident_id}/context. The
// handler authorizes the read against the reducer-owned durable
// incident→repository correlation edge (reducer_incident_repository_correlation,
// exact/derived outcomes only): an incident whose durable owning repository is
// outside the scoped grant, or that has no durable edge at all, is served as
// not-found with no existence disclosure. Adjacent incident sub-resources stay
// fail-closed for scoped tokens until each is separately proven tenant-filtered.
func scopedIncidentContextRoute(path string) bool {
	const (
		prefix = "/api/v0/incidents/"
		suffix = "/context"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	incidentID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return incidentID != "" && !strings.Contains(incidentID, "/")
}

func scopedWorkloadContextRoute(path string) bool {
	return scopedContextRoute(path, "/api/v0/workloads/")
}

func scopedServiceContextRoute(path string) bool {
	return scopedContextRoute(path, "/api/v0/services/")
}

func scopedServiceInvestigationRoute(path string) bool {
	const prefix = "/api/v0/investigations/services/"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	selector := strings.TrimPrefix(path, prefix)
	return selector != "" && !strings.Contains(selector, "/")
}

// scopedServiceIntelligenceReportRoute matches the service intelligence report
// route. The report composes the service-story dossier through the same scoped
// access filter, so it qualifies for scoped-token tenant filtering exactly like
// the service-story route.
func scopedServiceIntelligenceReportRoute(path string) bool {
	const prefix = "/api/v0/services/"
	const suffix = "/intelligence-report"
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	// CutSuffix (not TrimSuffix) so the suffix must be a distinct trailing
	// segment: "/api/v0/services/intelligence-report" (no service segment) does
	// not match because its remainder lacks the leading "/" of the suffix.
	selector, ok := strings.CutSuffix(strings.TrimPrefix(path, prefix), suffix)
	return ok && selector != "" && !strings.Contains(selector, "/")
}

func scopedQueryPlaybookRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/query-playbooks":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v0/query-playbooks/resolve":
		return true
	default:
		return false
	}
}

func scopedInvestigationWorkflowRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/investigation-workflows":
		return true
	case r.Method == http.MethodPost && r.URL.Path == "/api/v0/investigation-workflows/resolve":
		return true
	default:
		return false
	}
}

func scopedFactSchemaVersionRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/fact-schema-versions" {
		return true
	}
	const prefix = "/api/v0/fact-schema-versions/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	factKind := strings.TrimPrefix(r.URL.Path, prefix)
	return factKind != "" && !strings.Contains(factKind, "/")
}

// scopedSemanticSearchRoute reports whether the request targets the curated
// semantic-search route. The handler requires repo_id, checks scoped-token
// grants before the search-document store read, and computes result limits and
// truncation from only the authorized repository corpus.
func scopedSemanticSearchRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/search/semantic"
}

// scopedSBOMAttestationAttachmentRoute reports whether the request targets one
// of the reducer-owned SBOM/attestation attachment read routes. Attachment
// facts key on an image subject_digest but carry git repository_ids, so scoped
// reads intersect repository_ids (and the missing-evidence probe) with the
// grant set; attachments with no granted-repo correlation stay invisible.
func scopedHostedGovernanceStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/governance"
}

func scopedHostedReadinessRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/hosted-readiness"
}

func scopedSemanticExtractionStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/semantic-extraction"
}

func scopedAnswerNarrationStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/answer-narration"
}

func scopedSemanticEvidenceRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/semantic/documentation-observations",
		"/api/v0/semantic/code-hints":
		return true
	default:
		return false
	}
}

func scopedComponentExtensionRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/component-extensions" {
		return true
	}
	const (
		prefix = "/api/v0/component-extensions/"
		suffix = "/diagnostics"
	)
	if !strings.HasPrefix(r.URL.Path, prefix) || !strings.HasSuffix(r.URL.Path, suffix) {
		return false
	}
	componentID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, prefix), suffix)
	return componentID != "" && !strings.Contains(componentID, "/")
}

func scopedContextRoute(path string, prefix string) bool {
	for _, suffix := range []string{"/context", "/story"} {
		if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
			continue
		}
		selector := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
		return selector != "" && !strings.Contains(selector, "/")
	}
	return false
}
