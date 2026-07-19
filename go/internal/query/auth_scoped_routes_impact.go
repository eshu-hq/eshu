// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// scopedImpactCompareRoute reports whether the request targets one of the
// #5167 W3 impact/* and compare/* routes that now bind every returned row to
// the caller's grant (see impact_access_filter.go for the shared
// deny-by-default/empty-grant-short-circuit helpers each handler uses):
//
//   - investigate_contract_impact (contract_impact.go): the only implemented
//     family (http) is anchored on an exact, required provider_repo_id, so an
//     ungranted repo renders the same empty-providers shape as an unknown one.
//   - compare_environments (compare.go): the resolved workload's repo_id is
//     checked against the grant before any environment/cloud-resource read;
//     an ungranted workload renders the existing "workload not found" shape.
//   - find_blast_radius (impact_blast_radius.go): every affected row is a
//     Repository, bound to the grant after the traversal.
//   - investigate_resource (impact_resource_investigation.go): resolved
//     candidates, dependent workloads, and repository-provenance paths are
//     each independently bound to the grant.
//   - find_change_surface, investigate_change_surface, analyze_pre_change_impact,
//     plan_developer_change (impact_change_surface_*.go, prechange_impact.go,
//     developer_change_plan.go): resolved target candidates and every
//     impacted row (including an explicit repo_id used for changed_paths/topic
//     evidence) are bound to the grant.
//   - trace_deployment_chain, investigate_deployment_config
//     (impact_trace_deployment.go, deployment_config_influence.go): the anchor
//     workload is already grant-filtered by fetchServiceWorkloadContext
//     (shared with the already-allowlisted GET /services/{name}/context); this
//     family additionally binds cross-repository deployment-source rows to the
//     grant and skips the two free-text CloudResource fallbacks entirely for a
//     scoped caller, since those rows carry no repository property to bind to
//     a grant at all.
//
// trace_resource_to_code, explain_dependency_path (impact.go/impact_anchor_resolve.go)
// and trace_exposure_path (exposure_path.go) are NOT included here: they
// resolve an arbitrary graph node across many labels (impactAnchorLabelDisjunction,
// or an unbounded CALLS chain to a cross-repo cloud sink) with no repo_id
// property on most of the reachable node types, so binding every traversal
// endpoint to a grant needs a live-graph schema check and very likely a
// NornicDB-safe Cypher rewrite (see docs/public/reference/cypher-performance.md
// and nornicdb-pitfalls.md) before it is safe to allowlist -- they remain in
// pendingRowFilteringRoutes (#5167 flagged for follow-up, not guessed at).
func scopedImpactCompareRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/impact/contracts",
		"/api/v0/compare/environments",
		"/api/v0/impact/blast-radius",
		"/api/v0/impact/resource-investigation",
		"/api/v0/impact/change-surface",
		"/api/v0/impact/change-surface/investigate",
		"/api/v0/impact/pre-change",
		"/api/v0/impact/developer-change-plan",
		"/api/v0/impact/trace-deployment-chain",
		"/api/v0/impact/deployment-config-influence":
		return true
	default:
		return false
	}
}
