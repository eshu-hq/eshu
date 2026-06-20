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
	if scopedVulnerabilityScannerContractRoute(r) {
		return true
	}
	if scopedSupplyChainImpactRoute(r) {
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
func scopedSBOMAttestationAttachmentRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/sbom-attestations/attachments",
		"/api/v0/supply-chain/sbom-attestations/attachments/count",
		"/api/v0/supply-chain/sbom-attestations/attachments/inventory":
		return true
	default:
		return false
	}
}

// scopedSecurityAlertReconciliationRoute reports whether the request targets one
// of the reducer-owned provider security-alert reconciliation read routes.
// Reconciliation facts carry a git repository_id plus provider keys; scoped
// reads intersect those with the grant set and out-of-grant repository
// selectors fail before the reconciliation store read.
func scopedSecurityAlertReconciliationRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/security-alerts/reconciliations",
		"/api/v0/supply-chain/security-alerts/reconciliations/count",
		"/api/v0/supply-chain/security-alerts/reconciliations/inventory":
		return true
	default:
		return false
	}
}

// scopedInfraResourceAggregateRoute reports whether the request targets one of
// the graph-backed infra resource aggregate read routes (count, inventory).
// These aggregate over the whole-graph infrastructure corpus; scoped tokens
// bind a repository-anchored predicate (see infraResourceScopePredicate) so
// totals, rollups, and inventory buckets are computed over only the resources
// attributable to granted repositories.
func scopedInfraResourceAggregateRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/infra/resources/count",
		"/api/v0/infra/resources/inventory":
		return true
	default:
		return false
	}
}

// scopedInfraSearchRoute reports whether the request targets the graph-backed
// infra resource search route. The search handler runs its own whole-graph
// Cypher; scoped tokens bind the same repository-anchored predicate
// (infraResourceScopePredicate) so the matched rows, counts, limit, and
// truncation are computed over only the resources attributable to granted
// repositories, and an empty-grant scoped token returns a bounded empty page
// without a graph read.
func scopedInfraSearchRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/infra/resources/search"
}

// scopedInfraRelationshipsRoute reports whether the request targets the
// graph-backed infra relationship route. The handler anchors the seed node and
// every returned neighbor to a granted repository via infraResourceScopePredicate
// so a relationship is visible only when both endpoints are attributable to a
// granted repository; an out-of-grant or unanchorable seed returns not_found
// (no existence disclosure) and an empty grant fails closed without a graph read.
func scopedInfraRelationshipsRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/infra/relationships"
}

// scopedIaCResourceListRoute reports whether the request targets the
// graph-backed IaC resource browse read. The handler scans one canonical
// Terraform/IaC label (TerraformResource, TerraformModule, TerraformDataSource),
// each of whose nodes carries a durable `repo_id` property; scoped tokens bind a
// repository-anchored predicate (see iacResourceScopeClause) so the listed rows,
// count, limit+1 truncation, and keyset cursor are computed over only the
// resources attributable to granted repositories, and an empty-grant scoped
// token returns a bounded empty page without a graph read.
func scopedIaCResourceListRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/iac/resources"
}

// scopedWorkItemEvidenceRoute reports whether the request targets the
// source-only work-item evidence read GET /api/v0/work-items/evidence.
// Work-item facts key on the provider project scope (scope_id, project_key,
// work_item_key), not a git repository, so scoped reads intersect the work
// item's durable linked_repository_id (resolved by the Jira collector from a
// confidently typed GitHub PR or GitLab MR link before redaction, #2160) with
// the grant set. A work item with no durable linked_repository_id — every fact
// kind except a canonicalized external_link, or an out-of-grant project
// selector — stays invisible to scoped tokens (fail-closed), never a
// provider-scope leak. The exact evidence path is matched so adjacent
// work-item sub-resources stay deny-by-default until each is separately proven
// tenant-filtered, and the admin POST query route remains admin-only.
func scopedWorkItemEvidenceRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/work-items/evidence"
}

func scopedVulnerabilityScannerContractRoute(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		r.URL.Path == "/api/v0/supply-chain/vulnerability-scanner/contract"
}

// scopedSupplyChainImpactRoute reports whether the request targets one of the
// reducer-owned vulnerability impact read routes that compute counts, limits,
// truncation, aggregate grouping, and offsets over only the scoped-token's
// granted repositories. Adjacent supply-chain routes (impact explain, advisory
// detail, SBOM attestation attachments, container-image identities, and
// security-alert reconciliations) stay fail-closed for scoped tokens until each
// is separately proven tenant-filtered.
func scopedSupplyChainImpactRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/impact/findings",
		"/api/v0/supply-chain/impact/findings/count",
		"/api/v0/supply-chain/impact/inventory":
		return true
	default:
		return false
	}
}

// scopedSupplyChainAdvisoryEvidenceRoute reports whether the request targets the
// advisory-evidence read. Advisory facts are global CVE/advisory data with no
// repository of their own, so the bare id path is public; the
// repository/service/workload-anchored path intersects the impact findings that
// derive advisory anchors with the scoped-token grant set, and an out-of-grant
// repository selector fails before any store read.
func scopedSupplyChainAdvisoryEvidenceRoute(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		r.URL.Path == "/api/v0/supply-chain/advisories/evidence"
}

func scopedServiceCatalogCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/service-catalog/correlations"
}

func scopedPackageRegistryCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/package-registry/correlations"
}

// scopedAdmissionDecisionRoute reports whether the request targets the
// reducer-owned correlation admission decision read route. The handler requires
// domain, scope_id, and generation_id before reading and intersects scoped-token
// grants with scope_id plus repository anchors, so empty or out-of-grant scoped
// tokens get a bounded empty page without a store read.
func scopedAdmissionDecisionRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/evidence/admission-decisions"
}

// scopedContainerImageIdentityRoute reports whether the request targets one of
// the reducer-owned container image identity read routes that intersect
// source_repository_ids with the scoped-token grant set before counts,
// grouping, ordering, limits, offsets, truncation, and the source bridge.
// Identity facts key on the OCI repository_id and an OCI registry ingestion
// scope, so attribution to a granted git repository flows only through the
// source_repository_ids overlap; images with no source correlation stay
// invisible to scoped tokens.
func scopedContainerImageIdentityRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/supply-chain/container-images/identities",
		"/api/v0/supply-chain/container-images/identities/count",
		"/api/v0/supply-chain/container-images/identities/inventory":
		return true
	default:
		return false
	}
}

func scopedCICDRunCorrelationRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/ci-cd/run-correlations",
		"/api/v0/ci-cd/run-correlations/count",
		"/api/v0/ci-cd/run-correlations/inventory":
		return true
	default:
		return false
	}
}

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
