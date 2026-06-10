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
	if scopedQueryPlaybookRoute(r) {
		return true
	}
	if scopedVulnerabilityScannerContractRoute(r) {
		return true
	}
	if scopedSupplyChainImpactRoute(r) {
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
	if scopedSemanticEvidenceRoute(r) {
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
	if scopedCICDRunCorrelationRoute(r) {
		return true
	}
	if scopedContainerImageIdentityRoute(r) {
		return true
	}
	if scopedComponentExtensionRoute(r) {
		return true
	}
	if scopedCollectorStatusRoute(r) {
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

func scopedVulnerabilityScannerContractRoute(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		r.URL.Path == "/api/v0/supply-chain/vulnerability-scanner/contract"
}

// scopedSupplyChainImpactRoute reports whether the request targets one of the
// reducer-owned vulnerability impact read routes that compute counts, limits,
// truncation, aggregate grouping, and offsets over only the scoped-token's
// granted repositories. Adjacent supply-chain routes (impact explain, advisory
// evidence, advisory detail, SBOM attestation attachments, container-image
// identities, and security-alert reconciliations) stay fail-closed for scoped
// tokens until each is separately proven tenant-filtered.
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

func scopedDocumentationListRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/findings":
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/facts":
		return true
	default:
		return false
	}
}

func scopedDocumentationAggregateRoute(r *http.Request) bool {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/findings/count":
		return true
	case r.Method == http.MethodGet && r.URL.Path == "/api/v0/documentation/findings/inventory":
		return true
	default:
		return false
	}
}

func scopedDocumentationEvidencePacketRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if scopedDocumentationFindingPacketRoute(r.URL.Path) {
		return true
	}
	return scopedDocumentationPacketFreshnessRoute(r.URL.Path)
}

func scopedDocumentationFindingPacketRoute(path string) bool {
	const (
		prefix = "/api/v0/documentation/findings/"
		suffix = "/evidence-packet"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	findingID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return findingID != "" && !strings.Contains(findingID, "/")
}

func scopedDocumentationPacketFreshnessRoute(path string) bool {
	const (
		prefix = "/api/v0/documentation/evidence-packets/"
		suffix = "/freshness"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return false
	}
	packetID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	return packetID != "" && !strings.Contains(packetID, "/")
}

func scopedServiceCatalogCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/service-catalog/correlations"
}

func scopedPackageRegistryCorrelationRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/package-registry/correlations"
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

func scopedCollectorStatusRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/status/collectors"
}

func scopedIngesterStatusRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	if r.URL.Path == "/api/v0/status/ingesters" {
		return true
	}
	const prefix = "/api/v0/status/ingesters/"
	ingester := strings.TrimPrefix(r.URL.Path, prefix)
	return ingester != r.URL.Path && ingester != "" && !strings.Contains(ingester, "/")
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
