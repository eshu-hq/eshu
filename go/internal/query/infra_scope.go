// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Scoped-token authorization helpers for the graph-backed infra search
// (POST /api/v0/infra/resources/search) and relationship
// (POST /api/v0/infra/relationships) read routes.
//
// Both routes run their own whole-graph Cypher rather than the aggregate store,
// so they reuse infraResourceScopePredicate (infra_scope_grant.go) to bound
// results to resources attributable to a scoped token's granted repositories.
// The predicate is fail-closed and admits a node through five disjunct
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
//  3. MATCHES_STATE inline-map (#5623). TerraformStateResource nodes carry no
//     `repo_id` and anchor to a repository only through the MATCHES_STATE edge
//     a config-declared TerraformResource writes to it, e.g.
//     `(n)<-[:MATCHES_STATE]-(:TerraformResource {repo_id:$g})`. An unmatched
//     state resource (no edge) or one matched to an ungranted repository stays
//     invisible.
//  4. DEPLOYMENT_SOURCE. A node deployed from a granted repository is admitted
//     through a forward-anchored `EXISTS { (n)-[:DEPLOYMENT_SOURCE]->(:Repository) }`
//     — the one EXISTS shape the pinned build evaluates correctly.
//  5. DEFINES-collision inline-map. A Workload whose materialized `repo_id`
//     names a different tenant but which a granted repository DEFINES is
//     admitted through `(n)<-[:DEFINES]-(:Repository {id:$g})`, again as an
//     inline-map term to avoid the mis-evaluated backward EXISTS.
//
// The inline-map families (2, 3, 5) expand one term per grant and are capped at
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
func infraSearchScopeClause(access querycontract.RepositoryAccessFilter) string {
	if !access.Scoped() {
		return ""
	}
	scalars, _ := access.ScopeGrantInlineScalars()
	return " AND " + infraResourceScopePredicate("n", scalars)
}

// infraRelationshipAnchorClause bounds the relationship seed node `n` to a
// granted repository for scoped tokens. A seed that resolves to no granted
// repository matches nothing, so the handler returns not_found with no existence
// disclosure. Returns the empty string for shared / admin / local callers.
func infraRelationshipAnchorClause(access querycontract.RepositoryAccessFilter) string {
	if !access.Scoped() {
		return ""
	}
	scalars, _ := access.ScopeGrantInlineScalars()
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
func infraRelationshipNeighborClause(access querycontract.RepositoryAccessFilter, alias string) string {
	if !access.Scoped() {
		return ""
	}
	scalars, _ := access.ScopeGrantInlineScalars()
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

// Neo4j dialect for the scoped infra search and relationships reads (#7215).
//
// On Neo4j the SHAPE-A predicate (infraResourceScopePredicate) is expensive to
// plan for two reasons that multiply. Its inline-map families expand one
// pattern term per grant scalar (3 families x up to 128 scalars), and the
// search copies the whole predicate into every one of its 27 label branches
// while the relationships route copies it into 3 aliases on each of up to 15
// anchor statements. Measured on neo4j:2026-community, a 5-repo + 5-scope
// grant took 33.6 s to plan the search cold and 2.6 s warm; a 25 + 25 grant
// never planned inside 240 s (docs/internal/evidence/7215-*.md).
//
// The Neo4j dialect therefore:
//
//   - replaces each inline-map family with one list-EXISTS term over
//     $scope_grants, so the statement text no longer depends on the grant
//     count and one plan-cache entry serves every token;
//   - on search, returns n from each branch and applies the predicate once
//     after the CALL, then projects once;
//   - on relationships, probes each anchor label unscoped first and runs the
//     scoped statement only for labels that hold the id.
//
// NornicDB must never receive this dialect: a backward-anchored list EXISTS
// such as EXISTS { MATCH (n)<-[:USES]-(i:WorkloadInstance) WHERE i.repo_id IN
// $g } evaluates always-true there, a whole-graph leak (nornicdb-pitfalls.md,
// "EXISTS {} Subquery Correctness Depends On Anchor Direction"). Only an
// explicit querycontract.GraphBackendNeo4j selects it; the zero value and any
// other value keep SHAPE-A.

// infraScopeGrantsParam is the list parameter the Neo4j list-EXISTS terms
// test against. It carries the same capped, sorted, de-duplicated slice
// ScopeGrantInlineScalars returns, so admission matches SHAPE-A exactly,
// including the fail-closed 128 cap.
const infraScopeGrantsParam = "scope_grants"

// Span attribute values for eshu.infra_scope_dialect.
const (
	infraScopeDialectUnscoped   = "unscoped"
	infraScopeDialectShapeA     = "shape_a"
	infraScopeDialectNeo4jLists = "neo4j_list_exists"
)

// scopeUsesNeo4jDialect reports whether this read uses the Neo4j list-EXISTS
// dialect: a scoped caller on an explicitly configured Neo4j backend.
func (h *InfraHandler) scopeUsesNeo4jDialect(access querycontract.RepositoryAccessFilter) bool {
	return h != nil && h.GraphBackend == querycontract.GraphBackendNeo4j && access.Scoped()
}

// infraScopeDialectLabel names the dialect a read used, for the span.
func (h *InfraHandler) infraScopeDialectLabel(access querycontract.RepositoryAccessFilter) string {
	switch {
	case !access.Scoped():
		return infraScopeDialectUnscoped
	case h.scopeUsesNeo4jDialect(access):
		return infraScopeDialectNeo4jLists
	default:
		return infraScopeDialectShapeA
	}
}

// infraResourceScopeListPredicate is the Neo4j form of
// infraResourceScopePredicate: the same five disjunct families in the same
// order, with the three inline-map OR-chains (USES, MATCHES_STATE, DEFINES)
// each replaced by one EXISTS over $scope_grants. `x IN $scope_grants` is true
// exactly when one inline-map term {prop:$scope_grant_i} would match, and a
// null property is false in both forms. The inner variables carry a scope
// prefix so they never correlate with an outer alias of the same name.
func infraResourceScopeListPredicate(alias string) string {
	return "(" + strings.Join([]string{
		alias + ".repo_id IN $allowed_repository_ids",
		alias + ".repo_id IN $allowed_scope_ids",
		alias + ".id IN $allowed_repository_ids",
		alias + ".id IN $allowed_scope_ids",
		"EXISTS { MATCH (" + alias + ")<-[:USES]-(scopeUsesInstance:WorkloadInstance) " +
			"WHERE scopeUsesInstance.repo_id IN $" + infraScopeGrantsParam + " }",
		"EXISTS { MATCH (" + alias + ")<-[:MATCHES_STATE]-(scopeStateConfig:TerraformResource) " +
			"WHERE scopeStateConfig.repo_id IN $" + infraScopeGrantsParam + " }",
		"EXISTS { MATCH (" + alias + ")-[:DEPLOYMENT_SOURCE]->(scopeDeployRepo:Repository) " +
			"WHERE (scopeDeployRepo.id IN $allowed_repository_ids OR scopeDeployRepo.id IN $allowed_scope_ids) }",
		"EXISTS { MATCH (" + alias + ")<-[:DEFINES]-(scopeDefiningRepo:Repository) " +
			"WHERE scopeDefiningRepo.id IN $" + infraScopeGrantsParam + " }",
	}, " OR ") + ")"
}

// bindInfraNeo4jScopeParams binds the grant arrays and the capped
// $scope_grants list for the Neo4j dialect. It deliberately does not call
// access.GraphParams: that also binds the SHAPE-A scope_grant_<i> scalars,
// which the Neo4j statements never reference.
func bindInfraNeo4jScopeParams(params map[string]any, access querycontract.RepositoryAccessFilter) {
	params["allowed_repository_ids"] = append([]string{}, access.AllowedRepositoryIDs...)
	params["allowed_scope_ids"] = append([]string{}, access.AllowedScopeIDs...)
	scalars, _ := access.ScopeGrantInlineScalars()
	params[infraScopeGrantsParam] = append([]string{}, scalars...)
}

// infraSearchNeo4jScopedCypher builds the hoisted Neo4j search: one
// single-label branch per label returning n (still inside CALL, for the same
// reasons the SHAPE-A search wraps its UNION), the grant predicate applied once
// after the CALL, and one projection. whereExtra must NOT carry the scope
// clause. Plain UNION over n de-duplicates by node.
func infraSearchNeo4jScopedCypher(labels []string, whereExtra string) string {
	branches := make([]string, 0, len(labels))
	for _, label := range labels {
		branches = append(branches, `
		MATCH (n:`+label+`)
		WHERE true`+whereExtra+`
		RETURN n
	`)
	}
	return "CALL {" + strings.Join(branches, "\nUNION") + `
	}
	WITH n
	WHERE ` + infraResourceScopeListPredicate("n") + `
	WITH n
	RETURN ` + strings.Join(infraSearchReturnExprs(), ",\n\t       ") + `
		ORDER BY name
		LIMIT $limit
	`
}

// infraRelationshipAnchorProbeCypher is the unscoped existence probe for one
// anchor label. Its result only decides whether the scoped statement for that
// label is worth running; it is never returned to the caller.
func infraRelationshipAnchorProbeCypher(label string) string {
	return `
		MATCH (n:` + label + `) WHERE n.id = $entity_id
		RETURN 1 AS hit
		LIMIT 1
	`
}

// infraRelationshipNeo4jAnchorRead is the result of the Neo4j scoped anchor
// loop, with the counts the span records.
type infraRelationshipNeo4jAnchorRead struct {
	row         map[string]any
	labelsTried int
	probes      int
	scopedReads int
}

// resolveNeo4jScopedRelationshipAnchor runs the two-phase anchor loop. For each
// anchor label in order it probes unscoped, and only on a hit runs the scoped
// statement; the first scoped row wins. If no labeled scoped read returns a
// row, the scoped unlabeled (n) fallback runs, as in the SHAPE-A loop. The
// scoped statement for a label whose probe missed would return no row, so the
// answer equals the SHAPE-A loop's first-non-null answer. An ungranted anchor
// whose probe hit still ends with no row and the same 404 a missing id gets.
// Every read shares ctx, the route's single bounded deadline.
func (h *InfraHandler) resolveNeo4jScopedRelationshipAnchor(
	ctx context.Context,
	entityID string,
	scopedCypher func(anchor string) string,
	params map[string]any,
) (infraRelationshipNeo4jAnchorRead, error) {
	var read infraRelationshipNeo4jAnchorRead
	probeParams := map[string]any{"entity_id": entityID}
	for _, label := range impactRelationshipAnchorLabels {
		read.labelsTried++
		read.probes++
		hit, err := h.Neo4j.RunSingle(ctx, infraRelationshipAnchorProbeCypher(label), probeParams)
		if err != nil {
			return read, err
		}
		if hit == nil {
			continue
		}
		read.scopedReads++
		row, err := h.Neo4j.RunSingle(ctx, scopedCypher("(n:"+label+")"), params)
		if err != nil || row != nil {
			read.row = row
			return read, err
		}
	}
	read.labelsTried++
	read.scopedReads++
	row, err := h.Neo4j.RunSingle(ctx, scopedCypher("(n)"), params)
	read.row = row
	return read, err
}
