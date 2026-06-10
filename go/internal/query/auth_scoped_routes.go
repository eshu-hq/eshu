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
	if scopedComponentExtensionRoute(r) {
		return true
	}
	if scopedCollectorStatusRoute(r) {
		return true
	}
	if scopedIngesterStatusRoute(r) {
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

func scopedVulnerabilityScannerContractRoute(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		r.URL.Path == "/api/v0/supply-chain/vulnerability-scanner/contract"
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
