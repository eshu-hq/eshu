// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// Scoped-token authorization helpers for the graph-backed infra search
// (POST /api/v0/infra/resources/search) and relationship
// (POST /api/v0/infra/relationships) read routes.
//
// Both routes run their own whole-graph Cypher rather than the aggregate store,
// so they reuse infraResourceScopePredicate (infra_scope_grant.go) to bound
// results to resources attributable to a scoped token's granted repositories.
// The predicate is fail-closed and admits a node through four disjunct
// families OR-joined together (#5384, SHAPE-A; see infraResourceScopePredicate):
//
//  1. Direct ownership. Canonical IaC entity nodes (TerraformResource,
//     K8sResource, CloudFormationResource, ArgoCDApplication, HelmChart, ...)
//     and materialized Workload / WorkloadInstance nodes carry a durable
//     `repo_id` (or, for a Repository, `id`) property, so the direct compare
//     against the grant arrays is the join.
//  2. USES inline-map. CloudResource nodes carry no `repo_id` and anchor to a
//     repository only through the WorkloadInstance that USES them. Since the
//     pinned NornicDB build mis-evaluates EXISTS{} subquery correlation for
//     this backward-anchored shape (see docs/public/reference/nornicdb-pitfalls.md),
//     admission uses a pattern-predicate OR-chain of inline-map property terms —
//     one per grant, e.g. `(n)<-[:USES]-(:WorkloadInstance {repo_id:$g})` —
//     built by scopeGrantInlineMapDisjunction.
//  3. DEPLOYMENT_SOURCE. A node deployed from a granted repository is admitted
//     through a forward-anchored `EXISTS { (n)-[:DEPLOYMENT_SOURCE]->(:Repository) }`
//     — the one EXISTS shape the pinned build evaluates correctly.
//  4. DEFINES-collision inline-map. A Workload whose materialized `repo_id`
//     names a different tenant but which a granted repository DEFINES is
//     admitted through `(n)<-[:DEFINES]-(:Repository {id:$g})`, again as an
//     inline-map term to avoid the mis-evaluated backward EXISTS.
//
// The inline-map families (2, 4) expand one term per grant and are capped at
// maxScopeGrantInlineTerms with fail-closed degradation: past the cap only the
// direct-ownership and DEPLOYMENT_SOURCE families still admit, so a pathological
// >cap-grant token loses collision/USES admission for the overflow (missing
// rows, never extra rows).
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
	scalars, _ := access.scopeGrantInlineScalars()
	return " AND " + infraResourceScopePredicate("n", scalars)
}

// infraRelationshipAnchorClause bounds the relationship seed node `n` to a
// granted repository for scoped tokens. A seed that resolves to no granted
// repository matches nothing, so the handler returns not_found with no existence
// disclosure. Returns the empty string for shared / admin / local callers.
func infraRelationshipAnchorClause(access repositoryAccessFilter) string {
	if !access.scoped() {
		return ""
	}
	scalars, _ := access.scopeGrantInlineScalars()
	return " AND " + infraResourceScopePredicate("n", scalars)
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
	scalars, _ := access.scopeGrantInlineScalars()
	return " WHERE " + infraResourceScopePredicate(alias, scalars)
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
