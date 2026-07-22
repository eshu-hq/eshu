// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"strings"
)

// This file holds the scoped-token route predicates for the #5167 F-6 W6
// cloud/aws/kubernetes/observability/ecosystem/misc family: every route in
// this family now binds its reads to AllowedRepositoryIDs/AllowedScopeIDs
// (empty-grant short-circuit, redacted or withheld cross-tenant identifiers),
// so each is safe to move off the pendingRowFilteringRoutes ledger
// (auth_scoped_routes_pending_row_filtering.go) and onto this allowlist. Each
// predicate's doc comment cites the handler file that proves the binding.

// scopedCloudFamilyRoute reports whether the request targets one of this
// file's #5167 F-6 W6 cloud/aws/kubernetes/observability/ecosystem/misc
// family routes. Bundled into one predicate (rather than one `if` per route
// in scopedHTTPRouteSupportsTenantFilter, auth_scoped_routes.go) to keep that
// dispatcher under the repository's 500-line file cap.
func scopedCloudFamilyRoute(r *http.Request) bool {
	switch {
	case scopedCloudInventoryRoute(r),
		scopedCloudResourceListRoute(r),
		scopedCloudRuntimeDriftFindingsRoute(r),
		scopedAWSRuntimeDriftFindingsRoute(r),
		scopedKubernetesCorrelationsRoute(r),
		scopedObservabilityCoverageCorrelationsRoute(r),
		scopedEcosystemOverviewRoute(r),
		scopedGraphSummaryPacketRoute(r),
		scopedRelationshipEvidenceRoute(r),
		scopedRelationshipEdgesRoute(r),
		scopedRepositoriesByLanguageRoute(r),
		scopedRepositoryLanguageInventoryRoute(r):
		return true
	default:
		return false
	}
}

// scopedCloudResourceListRoute reports whether the request targets the
// graph-backed cloud resource browse page. listCloudResources selects only
// owner-ledger identities whose active source fact is within the caller's
// repository/scope grant before LIMIT, and short-circuits an empty grant.
func scopedCloudResourceListRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/cloud/resources"
}

// scopedCloudInventoryRoute reports whether the request targets the canonical
// multi-cloud resource inventory readback. cloudInventoryReadback.go binds
// fact_records.scope_id to AllowedRepositoryIDs/AllowedScopeIDs when scoped
// and returns an empty page without a query for an empty-grant scoped caller
// (listInventory, cloud_inventory_readback.go).
func scopedCloudInventoryRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/cloud/inventory"
}

// scopedCloudRuntimeDriftFindingsRoute reports whether the request targets
// the provider-neutral multi-cloud runtime drift readback. MultiCloudRuntimeDriftStore
// is shared with GET /api/v0/investigations/drift/packet (a different #5167
// workstream's route), so the fix is a caller-side precheck on the required
// filter.ScopeID rather than a store/filter change: listFindings
// (cloud_runtime_drift.go) never calls the store for an empty-grant or
// out-of-grant scoped caller, rendering the same zero-finding page a real
// empty result would.
func scopedCloudRuntimeDriftFindingsRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/cloud/runtime-drift/findings"
}

// scopedAWSRuntimeDriftFindingsRoute reports whether the request targets the
// AWS runtime drift readback. IaCManagementStore/IaCManagementFilter are
// shared with the iac/* and replatforming/* route families (other #5167
// workstreams), so the fix is a caller-side precheck: handleAWSRuntimeDriftFindings
// (aws_runtime_drift.go) requires a scoped caller to supply an exact
// granted scope_id (an account_id-only filter fans out via a LIKE-prefix scan
// this precheck cannot safely narrow) and never calls the store otherwise.
func scopedAWSRuntimeDriftFindingsRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/aws/runtime-drift/findings"
}

// scopedKubernetesCorrelationsRoute reports whether the request targets the
// reducer-owned Kubernetes correlation reads. listCorrelations
// (kubernetes.go) binds fact.scope_id to AllowedRepositoryIDs/AllowedScopeIDs
// when scoped (PostgresKubernetesCorrelationStore, kubernetes_correlations.go)
// and returns an empty page without a query for an empty-grant scoped caller.
func scopedKubernetesCorrelationsRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/kubernetes/correlations"
}

// scopedObservabilityCoverageCorrelationsRoute reports whether the request
// targets the reducer-owned observability coverage correlation reads.
// listCorrelations (observability_coverage.go) binds fact.scope_id to
// AllowedRepositoryIDs/AllowedScopeIDs when scoped
// (PostgresObservabilityCoverageCorrelationStore,
// observability_coverage_correlations.go) and returns an empty page without a
// query for an empty-grant scoped caller.
func scopedObservabilityCoverageCorrelationsRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/observability/coverage/correlations"
}

// scopedEcosystemOverviewRoute reports whether the request targets the
// ecosystem-wide graph entity counts. getEcosystemOverview
// (infra_ecosystem_overview.go) restricts every count to entities reachable
// from a granted Repository via DEFINES/INSTANCE_OF/RUNS_ON
// (runEcosystemOverviewCounts) and reports all-zero counts without a graph
// read for an empty-grant scoped caller.
func scopedEcosystemOverviewRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/ecosystem/overview"
}

// scopedGraphSummaryPacketRoute reports whether the request targets the
// bounded graph summary packet. getGraphSummaryPacket
// (infra_graph_summary_packet.go): the repo-scoped branch requires the
// caller-supplied repo_id to be granted (not_found otherwise, no existence
// disclosure); the ecosystem-wide branch reuses runEcosystemOverviewCounts,
// the same grant-bound counts as scopedEcosystemOverviewRoute.
func scopedGraphSummaryPacketRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/ecosystem/graph-summary"
}

// scopedRelationshipEvidenceRoute matches GET /api/v0/evidence/relationships/{resolved_id}.
// getRelationshipEvidence (evidence.go) requires BOTH the resolved
// relationship's source and target repo_id to be attributable to a granted
// repository or ingestion scope (relationshipEvidenceRowWithinAccess),
// serving not_found otherwise so neither the edge's existence nor either
// endpoint's identity is disclosed to an out-of-grant caller.
func scopedRelationshipEvidenceRoute(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	const prefix = "/api/v0/evidence/relationships/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	resolvedID := strings.TrimPrefix(r.URL.Path, prefix)
	return resolvedID != "" && !strings.Contains(resolvedID, "/")
}

// scopedRelationshipEdgesRoute reports whether the request targets the
// whole-graph typed-edge slice. getRelationshipEdges (relationships_catalog.go)
// binds the source endpoint to a granted repository/ingestion scope for every
// verb, and additionally binds the target endpoint for every verb whose
// target carries tenant attribution (relationshipVerbEntry.targetAttributable,
// relationships_catalog_cypher.go), returning an empty page without a graph
// read for an empty-grant scoped caller.
func scopedRelationshipEdgesRoute(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v0/relationships/edges"
}

// scopedRepositoriesByLanguageRoute reports whether the request targets the
// by-language repository readback. listRepositoriesByLanguage
// (repository_language_inventory.go) binds content_files.repo_id to
// AllowedRepositoryIDs/AllowedScopeIDs when scoped (ContentReader,
// content_reader_language_inventory.go) and returns an empty page without a
// query for an empty-grant scoped caller.
func scopedRepositoriesByLanguageRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/repositories/by-language"
}

// scopedRepositoryLanguageInventoryRoute reports whether the request targets
// the aggregate language inventory. getRepositoryLanguageInventory
// (repository_language_inventory.go) binds content_files.repo_id to
// AllowedRepositoryIDs/AllowedScopeIDs when scoped (ContentReader,
// content_reader_language_inventory.go) and returns an empty page without a
// query for an empty-grant scoped caller.
func scopedRepositoryLanguageInventoryRoute(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/api/v0/repositories/language-inventory"
}
