// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization helpers for the graph-backed infra search
// (POST /api/v0/infra/resources/search) and relationship
// (POST /api/v0/infra/relationships) read routes.
//
// Both routes run their own whole-graph Cypher rather than the aggregate store,
// so they reuse infraResourceScopePredicate (infra_resource_aggregates.go) to
// bound results to resources attributable to a scoped token's granted
// repositories. The predicate is fail-closed with two durable joins:
//
//  1. Canonical IaC entity nodes (TerraformResource, K8sResource,
//     CloudFormationResource, ArgoCDApplication, HelmChart, ...) carry a durable
//     `repo_id` property, so the direct compare against the grant arrays is the
//     join.
//  2. CloudResource nodes carry no `repo_id` and anchor to a repository only
//     through the canonical
//     (:Repository)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(:WorkloadInstance)-[:USES]->(n)
//     chain, matched by an EXISTS subquery anchored on the indexed Repository.id
//     grant filter.
//
// Nodes with no granted `repo_id` and no USES path from a granted repository
// match nothing and stay invisible to scoped tokens. Empty-grant scoped tokens
// are short-circuited in the handlers before any graph read; the clauses render
// only in scoped mode, so the unscoped Cypher for shared / admin / local callers
// is byte-identical to the pre-scoped query.

// infraSearchScopeClause returns the repository-anchored predicate appended to
// the infra search WHERE chain for scoped tokens, or the empty string for
// shared / admin / local callers (no-regression: the unscoped Cypher is
// unchanged). The seed alias is the search node `n`.
func infraSearchScopeClause(access repositoryAccessFilter) string {
	if !access.scoped() {
		return ""
	}
	return " AND " + infraResourceScopePredicate("n")
}

// infraRelationshipAnchorClause bounds the relationship seed node `n` to a
// granted repository for scoped tokens. A seed that resolves to no granted
// repository matches nothing, so the handler returns not_found with no existence
// disclosure. Returns the empty string for shared / admin / local callers.
func infraRelationshipAnchorClause(access repositoryAccessFilter) string {
	if !access.scoped() {
		return ""
	}
	return " AND " + infraResourceScopePredicate("n")
}

// infraRelationshipNeighborClause bounds an OPTIONAL MATCH neighbor (target /
// source) to a granted repository for scoped tokens so a granted seed node never
// discloses the name or id of a cross-tenant neighbor it links to. The clause is
// the OPTIONAL MATCH's own WHERE filter: a neighbor that fails the predicate does
// not bind, so OPTIONAL MATCH leaves the alias null and the edge is dropped from
// the result (fail-closed), while a seed with no edges still returns its
// identity. Neighbors with no durable repository signal are excluded the same
// way. Returns the empty string for shared / admin / local callers so the
// unscoped Cypher is unchanged.
func infraRelationshipNeighborClause(access repositoryAccessFilter, alias string) string {
	if !access.scoped() {
		return ""
	}
	return " WHERE " + infraResourceScopePredicate(alias)
}

// writeEmptyInfraSearch returns the bounded empty search page for an empty-grant
// scoped token without reading the graph, so an authenticated-but-ungranted
// token never triggers a whole-graph scan. The shape matches searchResources'
// success body (results / count / limit / truncated).
func (h *InfraHandler) writeEmptyInfraSearch(w http.ResponseWriter, r *http.Request, limit int) {
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"results":   []map[string]any{},
		"count":     0,
		"limit":     limit,
		"truncated": false,
	}, BuildTruthEnvelope(
		h.profile(),
		"platform_impact.deployment_chain",
		TruthBasisHybrid,
		"scoped token grants authorize no repositories; infrastructure search results are empty",
	))
}
