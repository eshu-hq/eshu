// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // #6818 move 4b: root auth surface (aliases, forwarders, route policies, handler wiring) stays in package query; moving it into auth/ strands root handler callers and turns this rename into a root-surface relocation, which is a separate follow-up.

import "net/http"

// scopedImpactCompareRoute reports whether the request targets one of the
// #5167 W3 impact/* and compare/* routes that now bind every returned row to
// the caller's grant (see impact_access_filter.go for the shared
// deny-by-default/empty-grant-short-circuit helpers each handler uses):
//
//   - investigate_contract_impact (impact/contract.go): the only implemented
//     family (http) is anchored on an exact, required provider_repo_id, so an
//     ungranted repo renders the same empty-providers shape as an unknown one.
//   - compare_environments (compare/handler.go): the resolved workload's repo_id is
//     checked against the grant before any environment/cloud-resource read;
//     an ungranted workload renders the existing "workload not found" shape.
//   - find_blast_radius (impact/blast_radius.go): every affected row is a
//     Repository, bound to the grant after the traversal.
//   - investigate_resource (impact/resource_investigation.go): resolved
//     candidates, dependent workloads, and repository-provenance paths are
//     each independently bound to the grant.
//   - find_change_surface, investigate_change_surface, analyze_pre_change_impact,
//     plan_developer_change (impact/change_surface_*.go, impact/prechange.go,
//     developer_change_plan.go): resolved target candidates and every
//     impacted row (including an explicit repo_id used for changed_paths/topic
//     evidence) are bound to the grant.
//   - trace_deployment_chain, investigate_deployment_config
//     (impact/trace_deployment.go, impact/deployment_config_influence.go): the anchor
//     workload is already grant-filtered by fetchServiceWorkloadContext
//     (shared with the already-allowlisted GET /services/{name}/context); this
//     family additionally binds cross-repository deployment-source rows to the
//     grant and skips the two free-text CloudResource fallbacks entirely for a
//     scoped caller, since those rows carry no repository property to bind to
//     a grant at all.
//
// trace_resource_to_code, explain_dependency_path and trace_exposure_path
// (impact/handler.go, impact/exposure_path.go) were promoted off the #5167
// pending ledger. Their walks cross nodes that carry no repo_id, so the grant
// cannot be one Cypher predicate: impact/ownership judges every node on the
// bounded page per class -- Repository by id, repo_id-carrying labels by
// repo_id in Go, CloudResource by USES from a granted WorkloadInstance,
// TerraformStateResource by MATCHES_STATE from a granted TerraformResource, a
// WorkloadInstance by DEPLOYMENT_SOURCE to a granted Repository -- and denies
// every other class. A path crossing any ungranted node is dropped whole; an
// ungranted anchor or endpoint renders exactly like an unknown one and issues
// no traversal; an empty grant makes no graph call; truncated comes from the
// raw row count; and every scoped response discloses the withheld sections
// (scoped: true, withheld_sections, and for exposure the withheld sink
// classes in coverage.unresolved_reason).
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
		"/api/v0/impact/deployment-config-influence",
		"/api/v0/impact/trace-resource-to-code",
		"/api/v0/impact/explain-dependency-path",
		"/api/v0/impact/trace-exposure-path":
		return true
	default:
		return false
	}
}
