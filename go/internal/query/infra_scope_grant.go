// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// scopeHopDirection, the maxScopeGrantInlineTerms cap, the
// scopeGrantInlineParamPrefix, and the scopeGrantInlineScalars /
// scopeGrantInlineMapDisjunction / bindScopeGrantInlineScalars primitives
// moved to querycontract (issue #6060) so family subpackages can build
// SHAPE-A scoped-token predicates without importing this root package. These
// forwarders keep every existing root call site (mostly tests) compiling
// under its original lowercase name; see querycontract/infra_scope_grant.go
// for the implementation and the SHAPE-A design rationale.
type scopeHopDirection = querycontract.ScopeHopDirection

const (
	scopeHopInbound  = querycontract.ScopeHopInbound
	scopeHopOutbound = querycontract.ScopeHopOutbound
)

const maxScopeGrantInlineTerms = querycontract.MaxScopeGrantInlineTerms

func scopeGrantInlineScalars(repositoryIDs, scopeIDs []string) (scalars []string, capped bool) {
	return querycontract.ScopeGrantInlineScalars(repositoryIDs, scopeIDs)
}

func scopeGrantInlineMapDisjunction(
	alias string,
	direction scopeHopDirection,
	relType, targetLabel, targetProp string,
	scalars []string,
) string {
	return querycontract.ScopeGrantInlineMapDisjunction(alias, direction, relType, targetLabel, targetProp, scalars)
}

func bindScopeGrantInlineScalars(params map[string]any, scalars []string) {
	querycontract.BindScopeGrantInlineScalars(params, scalars)
}

// workloadScopePredicate bounds a Workload-anchored alias to the caller's
// granted repositories. The implementation moved to querycontract for #6060;
// this wrapper keeps root callers unchanged.
func workloadScopePredicate(alias string, access querycontract.RepositoryAccessFilter) string {
	return querycontract.WorkloadScopePredicate(alias, access)
}

// infraResourceScopePredicate bounds a whole-graph infra node `alias` to the
// resources a scoped token's granted repositories authorize. It is a fail-closed
// disjunction: a node matches only when it resolves to a granted repository
// through one of the disjuncts below, otherwise it is excluded from every count,
// rollup, inventory bucket, search result, and relationship-neighbor result.
//
//  1. Direct ownership (flat, O(1) array params): canonical IaC entity nodes
//     (TerraformResource, K8sResource, CloudFormationResource, ArgoCDApplication,
//     HelmChart, ...) and materialized Workload / WorkloadInstance nodes carry a
//     durable `repo_id`; Repository nodes carry their grant identity as `id`. The
//     direct `IN $allowed_repository_ids` / `IN $allowed_scope_ids` compares are
//     the durable join for those. The `id` compare admits a Repository only
//     (#7220): every other label is admitted through `repo_id` or the ownership
//     disjuncts below, so a non-Repository node whose id spells a granted id is
//     not admitted.
//  2. CloudResource via USES (inline-map, O(grant)): a CloudResource carries no
//     `repo_id`; it anchors to a repository through the WorkloadInstance that
//     USES it. The SHAPE-A inline-map disjunction admits it when a using
//     WorkloadInstance's own `repo_id` is granted. (This replaces the previously
//     shipped n-last 4-hop `EXISTS` bridge, which is dead code on the pinned
//     NornicDB build — it silently under-authorized every scoped CloudResource.)
//  3. TerraformStateResource via MATCHES_STATE (inline-map, O(grant), #5623): a
//     state-observed TerraformStateResource carries no `repo_id` of its own --
//     it is not defined by a repository the way config-declared TerraformResource
//     is. It anchors to a repository only through the MATCHES_STATE edge a
//     config-declared TerraformResource writes to it once backend ownership
//     resolves and the state/config address match is unambiguous (#5443,
//     canonicalTerraformStateMatchesConfigEdgeCypher). The inline-map disjunction
//     admits it when the matching TerraformResource's own `repo_id` is granted --
//     `(alias)<-[:MATCHES_STATE]-(:TerraformResource {repo_id:$g})`. Deliberately
//     NOT the node's own `config_repo_id` property: that property is set from
//     backend-ownership resolution alone (resolveTerraformStateOwnership) and can
//     be non-null even when NO MATCHES_STATE edge was ever written (ambiguous
//     match, or no config resource at that address -- the documented
//     "applied-only" state), so a property-only disjunct would admit an unmatched
//     state resource whenever its backend happens to be owned by a granted repo,
//     violating "unmatched stays invisible" (#5623 acceptance; proven live on the
//     pinned NornicDB image -- a property-only disjunct returned 2 rows for a
//     fixture with exactly 1 matched-and-granted node and 1 unmatched node
//     sharing the same config_repo_id). Traversing the edge itself is the only
//     shape that ties visibility to an ACTUAL match, mirroring disjunct 2's own
//     inline-map-over-property choice.
//  4. WorkloadInstance via DEPLOYMENT_SOURCE (forward EXISTS, array params): an
//     instance is admitted when it deploys from a granted repository, covering
//     the case where its own `repo_id` (its defining repo) is out of grant but
//     its deployment repo is granted. Forward-anchored `EXISTS` with an
//     `IN $array` filter is the one EXISTS shape the pinned NornicDB build
//     evaluates correctly.
//  5. Workload via DEFINES (inline-map, O(grant)): a Workload node is admitted
//     when a granted Repository DEFINES it. This is required in addition to the
//     flat `repo_id` disjunct because a name-collision Workload defined by two
//     repositories materializes only ONE repo_id, so a grant for its OTHER
//     defining repository is missed by the flat compare but caught here.
//
// The USES, MATCHES_STATE, and DEFINES disjuncts are inert for node labels that
// lack those inbound edges (an IaC entity has no inbound USES; a CloudResource
// has no inbound MATCHES_STATE; a CloudResource has no inbound DEFINES), so a
// single predicate string applies uniformly across the per-label aggregate scans
// and the label-free relationship read.
//
// `scalars` MUST be the slice returned by scopeGrantInlineScalars for the same
// grant set the params bind (see infraResourceAggregateParams and the search /
// relationship handlers). The predicate renders only in scoped mode; the
// unscoped query shape for shared / admin / local callers is unchanged.
func infraResourceScopePredicate(alias string, scalars []string) string {
	return infraScopePredicate(infraResourceScopeCoreDisjuncts(alias, scalars), alias, scalars)
}

// infraResourceScopeNodePredicate is infraResourceScopePredicate for an alias
// bound by a single-node MATCH (`MATCH (n:Label) WHERE ...`): a search branch, a
// per-label aggregate scan, or the relationships anchor. It spells the
// Repository-only id test as a label test, which NornicDB evaluates correctly
// there and which also admits a labeled Repository anchor; the CASE operand in
// infraResourceScopePredicate does not (#7220). Use infraResourceScopePredicate
// for an alias bound by a relationship pattern.
func infraResourceScopeNodePredicate(alias string, scalars []string) string {
	return infraScopePredicate(infraScopeDisjuncts(alias, scalars, true), alias, scalars)
}

// infraScopePredicate appends the DEFINES disjunct to core and joins the OR
// group.
func infraScopePredicate(core []string, alias string, scalars []string) string {
	if defines := scopeGrantInlineMapDisjunction(alias, scopeHopInbound, "DEFINES", "Repository", "id", scalars); defines != "" {
		core = append(core, defines)
	}
	return "(" + strings.Join(core, " OR ") + ")"
}

// infraRepositoryIDExpr is the SHAPE-A id-equality operand for an alias bound
// by a relationship pattern: the node's `id` when it is a Repository and null
// for every other label, so `<expr> IN $allowed_*` admits only a Repository
// whose id is granted (#7220). A label test cannot go here: on the pinned
// NornicDB build a label test ANDed inside this OR chain, in the WHERE of a
// relationship MATCH, is ignored or zeroes the whole chain
// (docs/public/reference/nornicdb-query-pitfalls.md, "A Label Predicate's Clause
// Position Decides Whether It Is Evaluated"), while `'Repository' IN
// labels(alias)` inside a CASE evaluates correctly there. The same CASE never
// matches a labeled Repository in a single-node MATCH, so single-node reads use
// infraRepositoryIDLabelTerm through infraResourceScopeNodePredicate.
func infraRepositoryIDExpr(alias string) string {
	return "(CASE WHEN 'Repository' IN labels(" + alias + ") THEN " + alias + ".id END)"
}

// infraResourceScopeCoreDisjuncts returns disjuncts 1-4 of
// infraResourceScopePredicate's doc comment (direct ownership, CloudResource
// via USES, TerraformStateResource via MATCHES_STATE, WorkloadInstance via
// DEPLOYMENT_SOURCE) WITHOUT disjunct 5 (Workload via DEFINES). Every disjunct
// here resolves through a durable, per-node property, an edge to a
// single-owner target node, or a forward-anchored deployment-source edge --
// never through reachability into a shared graph identity.
//
// Disjunct 5 is deliberately excluded from this shared core: it admits a bare
// Workload node whenever ANY granted repository DEFINES it, which is safe for
// infraResourceScopePredicate's reachability-counting callers (a Workload
// admitted this way is only ever used to enumerate further per-instance
// durably-scoped nodes one hop down) but unsafe for a caller that projects the
// admitted alias's OWN id/name/edges directly -- a name-collision Workload
// (defined by two repositories, materializing only ONE durable repo_id; see
// infraResourceScopePredicate's doc comment) would expose its full edge set to
// every repository that happens to define it, regardless of which tenant's
// ingestion actually wrote a given edge. relationshipEndpointScopePredicate
// (relationships_catalog_cypher.go) is exactly that caller and composes this
// core directly instead of calling infraResourceScopePredicate.
//
// Disjunct 3 (MATCHES_STATE) has no analogous collision risk and is safe to
// include here even for direct-projection callers: the config-match resolver
// anchors on a SINGLE resolved OwningRepoID per state resource
// (TerraformStateOwnershipResolver.ResolveOwningRepoID never returns more than
// one distinct repo) and excludes ambiguous address matches from the edge write
// entirely (ConfigMatchAmbiguous, tfstate_state_match_edge.go), so a
// TerraformStateResource can have at most one MATCHES_STATE edge, from exactly
// one repository's TerraformResource.
//
// That "at most one edge" invariant is enforced by
// terraformStateMatchesConfigEdgeRetractStatements
// (go/internal/storage/cypher/tfstate_state_match_edge_retract.go), which
// deletes a stale edge whenever a state resource's resolved owning repo
// changes. #5623 P0 review finding: an earlier version of that retract skipped
// entirely on a delta cycle, so a resource reassigned to a different owning
// repo on a delta cycle briefly carried edges to BOTH the old and the new
// owner, and THIS disjunct -- built purely from graph shape, with no
// generation awareness -- admitted the resource for either repo's grant, a
// real tenant-visibility leak (not merely a theoretical one; see
// TestCanonicalNodeWriterRetractsStaleMatchesStateEdgeOnDeltaCycleLive and
// TestLiveInfraScopeShapeMatchesStateStaleEdgeExcludedAfterDeltaReassignment
// for the live reproduction and fix proof). This disjunct's correctness
// depends on that retract running every generation (skipped only on the
// scope's first generation, when nothing can be stale yet) -- do not
// reintroduce a DeltaProjection skip on that retract without re-proving this
// disjunct stays safe.
func infraResourceScopeCoreDisjuncts(alias string, scalars []string) []string {
	return infraScopeDisjuncts(alias, scalars, false)
}

// infraScopeDisjuncts renders disjuncts 1-4; singleNode selects the label-test
// id term over the CASE operand (see infraResourceScopeNodePredicate).
func infraScopeDisjuncts(alias string, scalars []string, singleNode bool) []string {
	disjuncts := []string{
		alias + ".repo_id IN $allowed_repository_ids",
		alias + ".repo_id IN $allowed_scope_ids",
	}
	if singleNode {
		disjuncts = append(disjuncts, infraRepositoryIDLabelTerm(alias))
	} else {
		disjuncts = append(disjuncts,
			infraRepositoryIDExpr(alias)+" IN $allowed_repository_ids",
			infraRepositoryIDExpr(alias)+" IN $allowed_scope_ids")
	}
	if uses := scopeGrantInlineMapDisjunction(alias, scopeHopInbound, "USES", "WorkloadInstance", "repo_id", scalars); uses != "" {
		disjuncts = append(disjuncts, uses)
	}
	if matchesState := scopeGrantInlineMapDisjunction(alias, scopeHopInbound, "MATCHES_STATE", "TerraformResource", "repo_id", scalars); matchesState != "" {
		disjuncts = append(disjuncts, matchesState)
	}
	disjuncts = append(disjuncts,
		"EXISTS { MATCH ("+alias+")-[:DEPLOYMENT_SOURCE]->(scopeDeployRepo:Repository) "+
			"WHERE (scopeDeployRepo.id IN $allowed_repository_ids OR scopeDeployRepo.id IN $allowed_scope_ids) }")
	return disjuncts
}
