// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
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
	// #5419 Phase 4b: GET /api/v0/codeowners/ownership now gates both its read
	// paths (the DECLARES_CODEOWNER graph and the service-catalog correlation
	// store used by resolveEffectiveRepositoryOwner) on the caller's grant --
	// see writeEmptyCodeownersOwnership in codeowners_ownership.go.
	if r.Method == http.MethodGet && r.URL.Path == "/api/v0/codeowners/ownership" {
		return true
	}
	// Single-repository {repo_id} routes are matched against the ESCAPED path,
	// not r.URL.Path. The MCP dispatchers build the path with url.PathEscape
	// (go/internal/mcp/dispatch_repositories.go), so an org/repo-style selector
	// arrives as one escaped segment (org%2Frepo); http.NewRequest decodes
	// r.URL.Path back to org/repo -- reintroducing a slash that would trip
	// scopedRepositorySingleResourceRoute's single-segment guard and 403 a
	// legitimate scoped caller before the grant-filtering selector resolver
	// runs (#5167 PR #5324 review, P1). r.URL.EscapedPath() preserves the
	// single escaped segment and equals r.URL.Path for slash-free selectors.
	repoRoutePath := r.URL.EscapedPath()
	if r.Method == http.MethodGet && scopedRepositoryFreshnessRoute(repoRoutePath) {
		return true
	}
	// #5167 Group A: already grant-filtered single-repository GET routes that
	// only needed the allowlist matcher (see auth_scoped_routes_repository.go
	// doc comments for each handler's resolution chain).
	if r.Method == http.MethodGet && scopedRepositoryStatsRoute(repoRoutePath) {
		return true
	}
	if r.Method == http.MethodGet && scopedRepositoryContextRoute(repoRoutePath) {
		return true
	}
	if r.Method == http.MethodGet && scopedRepositoryStoryRoute(repoRoutePath) {
		return true
	}
	if r.Method == http.MethodGet && scopedRepositoryCoverageRoute(repoRoutePath) {
		return true
	}
	if r.Method == http.MethodGet && scopedRepositoryTreeRoute(repoRoutePath) {
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
	// #5167 task 4: VisualizationHandler holds no graph/content/store
	// reference (visualization_packet_handler.go) -- it only reshapes the
	// caller-supplied source_response, so there is no tenant data to filter.
	if r.Method == http.MethodPost && r.URL.Path == "/api/v0/visualizations/derive" {
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
	if scopedMCPTransportRoute(r) {
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
	if scopedOperationsRoute(r) {
		return true
	}
	if scopedDeadLetterListRoute(r) {
		return true
	}
	if scopedInputInvalidFactListRoute(r) {
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
	if scopedPackageRegistryIdentityRoute(r) {
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
	if scopedIaCDeadRoute(r) {
		return true
	}
	if scopedIaCManagementRoute(r) {
		return true
	}
	if scopedReplatformingSelectorRoute(r) {
		return true
	}
	if scopedReplatformingPlanFamilyRoute(r) {
		return true
	}
	if scopedWorkItemEvidenceRoute(r) {
		return true
	}
	if scopedImpactCompareRoute(r) {
		return true
	}
	if scopedCloudFamilyRoute(r) {
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
