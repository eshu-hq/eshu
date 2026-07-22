// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"sort"
	"strconv"
	"strings"
)

// SHAPE-A scoped-token authorization primitives for the graph-backed infra
// read routes (issue #5384). These convert a scoped token's granted repository
// and ingestion-scope ids into a NornicDB-safe pattern-predicate disjunction
// that admits a graph node when it is connected, in a fixed direction, to a
// granted repository through an inline-map pattern term — one term per grant.
//
// Why a pattern-predicate OR-chain instead of an EXISTS subquery: the pinned
// NornicDB build mis-evaluates EXISTS{} correlation. A backward-anchored
// `EXISTS { (n)<-[:USES]-(i) WHERE i.repo_id IN $grants }` evaluates
// unconditionally true (whole-graph leak), and the shipped n-last 4-hop bridge
// `EXISTS { (scopeRepo)-[:DEFINES]->...->(n) WHERE scopeRepo.id IN $grants }`
// evaluates unconditionally false (dead code / under-authorization). A pattern
// used directly as a boolean predicate with an inline-map property term
// (`(n)<-[:USES]-(:WorkloadInstance {repo_id:$g})`) is correct on BOTH NornicDB
// and Neo4j — proven true→match / false→no-match against representative and
// worst-case data on the pinned image. The trade-off is O(grant) fan-out: the
// grant array expands into one inline-map term per grant. See the scratch
// evidence and docs/public/reference/nornicdb-pitfalls.md.

// scopeHopDirection selects the relationship arrow used by
// scopeGrantInlineMapDisjunction relative to the bound alias.
type scopeHopDirection int

const (
	// scopeHopInbound builds `(alias)<-[:relType]-(:targetLabel {targetProp:$g})`,
	// i.e. the target node points at the bound alias (e.g. a WorkloadInstance
	// USES the CloudResource, a Repository DEFINES the Workload).
	scopeHopInbound scopeHopDirection = iota
	// scopeHopOutbound builds `(alias)-[:relType]->(:targetLabel {targetProp:$g})`,
	// i.e. the bound alias points at the target node.
	scopeHopOutbound
)

// maxScopeGrantInlineTerms caps the O(grant) inline-map OR-chain fan-out.
//
// Past this many grants, scopeGrantInlineScalars truncates the inline-map terms
// and reports capped=true. This degradation is FAIL-CLOSED and safe by
// construction: the composed scope predicates always OR the truncated inline-map
// disjunction together with the flat `alias.repo_id IN $allowed_repository_ids`
// array disjuncts, which admit ALL direct-ownership grants in O(1) regardless of
// the cap. So a pathological token with more than maxScopeGrantInlineTerms
// grants still sees every resource it directly owns; it loses only
// collision-defined / bridge admission for grants beyond the cap — an
// under-authorization (missing rows), never a leak (extra rows). Do NOT "fix"
// this by removing the cap: the cap bounds a per-node OR-chain cost that would
// otherwise grow without limit, and fail-closed degradation is the correct
// posture under the accuracy/performance life motto. 128 comfortably covers
// realistic multi-repo grants (tens); the boundary cold cost is bounded (~1.4s)
// and warm cost negligible.
const maxScopeGrantInlineTerms = 128

// scopeGrantInlineParamPrefix names the per-grant scalar params bound by
// bindScopeGrantInlineScalars and referenced by scopeGrantInlineMapDisjunction
// (keys scope_grant_0 .. scope_grant_{n-1}). Reusing this prefix across multiple
// disjunctions in one query is safe: every disjunction binds the same ordered
// scalar values to the same keys, so the writes are idempotent.
const scopeGrantInlineParamPrefix = "scope_grant_"

// scopeGrantInlineScalars returns the deduplicated, deterministically ordered
// union of granted repository and ingestion-scope ids used to build SHAPE-A
// inline-map disjunctions, truncated to maxScopeGrantInlineTerms. Empty ids are
// dropped. capped reports whether truncation dropped grants (callers may log or
// telemeter it; correctness is preserved by the flat array disjuncts — see
// maxScopeGrantInlineTerms). The predicate builder and the param binder MUST use
// the SAME returned slice so their scalar keys and count agree exactly.
func scopeGrantInlineScalars(repositoryIDs, scopeIDs []string) (scalars []string, capped bool) {
	seen := make(map[string]struct{}, len(repositoryIDs)+len(scopeIDs))
	union := make([]string, 0, len(repositoryIDs)+len(scopeIDs))
	for _, id := range repositoryIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		union = append(union, id)
	}
	for _, id := range scopeIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		union = append(union, id)
	}
	sort.Strings(union)
	if len(union) > maxScopeGrantInlineTerms {
		return union[:maxScopeGrantInlineTerms], true
	}
	return union, false
}

// scopeGrantInlineMapDisjunction builds a NornicDB-safe pattern-predicate
// OR-chain that admits the bound `alias` when it is connected — in `direction`
// via `relType` — to a `targetLabel` node whose `targetProp` equals one of the
// granted ids. Each scalar in `scalars` becomes one inline-map pattern term
// binding a distinct scalar param scope_grant_<i>. The returned fragment is
// parenthesized and safe to OR into a larger WHERE predicate. It returns the
// empty string when `scalars` is empty (the caller composes the flat array
// disjuncts around it, so an empty inline-map contributes nothing and the
// predicate stays fail-closed). Bind the referenced params with
// bindScopeGrantInlineScalars using the SAME `scalars` slice.
//
// relType, targetLabel, and targetProp are fixed, code-owned identifiers (never
// request input); they are interpolated unquoted because the pinned NornicDB
// build does not match backtick-quoted labels. The grant values flow only
// through bound parameters.
func scopeGrantInlineMapDisjunction(
	alias string,
	direction scopeHopDirection,
	relType, targetLabel, targetProp string,
	scalars []string,
) string {
	if len(scalars) == 0 {
		return ""
	}
	left, right := "<-[:", "]-"
	if direction == scopeHopOutbound {
		left, right = "-[:", "]->"
	}
	terms := make([]string, 0, len(scalars))
	for i := range scalars {
		param := "$" + scopeGrantInlineParamPrefix + strconv.Itoa(i)
		terms = append(terms, "("+alias+")"+left+relType+right+"(:"+targetLabel+" {"+targetProp+":"+param+"})")
	}
	return "(" + strings.Join(terms, " OR ") + ")"
}

// bindScopeGrantInlineScalars binds scope_grant_<i> = scalars[i] into params,
// matching the keys scopeGrantInlineMapDisjunction references. Call it with the
// exact slice returned by scopeGrantInlineScalars. It is a no-op for empty
// scalars. Re-binding the same keys in one params map is safe (idempotent).
func bindScopeGrantInlineScalars(params map[string]any, scalars []string) {
	for i, value := range scalars {
		params[scopeGrantInlineParamPrefix+strconv.Itoa(i)] = value
	}
}

// scopeGrantInlineScalars returns the capped SHAPE-A inline-map scalar set for a
// scoped access filter (the union of its granted repository and ingestion-scope
// ids). It is the access-filter companion to the package-level
// scopeGrantInlineScalars and guarantees the predicate builders and param
// binders derive an identical ordered slice from one source. Shared / admin /
// local callers (unscoped) get an empty slice.
//
// Callers currently discard the returned capped bool (scalars, _ :=). This is
// intentional and safe, not an oversight: capping only truncates the inline-map
// (USES / DEFINES-collision) admission families, which is fail-closed — a
// >maxScopeGrantInlineTerms-grant token loses collision/USES admission for the
// overflow (missing rows, never extra) while the direct-ownership and
// DEPLOYMENT_SOURCE families still admit. Surfacing the cap as an operator
// signal (log / metric) needs a logger threaded through these string-builder
// call sites and a telemetry-coverage contract update, so it is tracked
// separately in #5408 rather than wired here.
func (f repositoryAccessFilter) scopeGrantInlineScalars() (scalars []string, capped bool) {
	if !f.scoped() {
		return nil, false
	}
	return scopeGrantInlineScalars(f.allowedRepositoryIDs, f.allowedScopeIDs)
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
//     the durable join for those.
//  2. CloudResource via USES (inline-map, O(grant)): a CloudResource carries no
//     `repo_id`; it anchors to a repository through the WorkloadInstance that
//     USES it. The SHAPE-A inline-map disjunction admits it when a using
//     WorkloadInstance's own `repo_id` is granted. (This replaces the previously
//     shipped n-last 4-hop `EXISTS` bridge, which is dead code on the pinned
//     NornicDB build — it silently under-authorized every scoped CloudResource.)
//  3. WorkloadInstance via DEPLOYMENT_SOURCE (forward EXISTS, array params): an
//     instance is admitted when it deploys from a granted repository, covering
//     the case where its own `repo_id` (its defining repo) is out of grant but
//     its deployment repo is granted. Forward-anchored `EXISTS` with an
//     `IN $array` filter is the one EXISTS shape the pinned NornicDB build
//     evaluates correctly.
//  4. Workload via DEFINES (inline-map, O(grant)): a Workload node is admitted
//     when a granted Repository DEFINES it. This is required in addition to the
//     flat `repo_id` disjunct because a name-collision Workload defined by two
//     repositories materializes only ONE repo_id, so a grant for its OTHER
//     defining repository is missed by the flat compare but caught here.
//
// The USES and DEFINES disjuncts are inert for node labels that lack those
// inbound edges (an IaC entity has no inbound USES; a CloudResource has no
// inbound DEFINES), so a single predicate string applies uniformly across the
// per-label aggregate scans and the label-free relationship read.
//
// TerraformStateResource (#5443, state-observed Terraform resources) carries
// no `repo_id` -- it is not defined by a repository the way config-declared
// TerraformResource is, only optionally matched to one via the MATCHES_STATE
// edge and the node's own config_repo_id property once backend ownership
// resolves. None of the four disjuncts above admit it, so it is
// fail-closed: invisible to every scoped-token infra read even though it is
// included in allInfraLabels (infra.go). This is the SAFE failure mode
// (nothing over-authorized), not a security gap, but it is a real coverage
// gap -- a scoped caller cannot see state-observed resources through this
// path at all today. Tracked as issue #5623. A future change adding a
// `config_repo_id`-based disjunct needs the same tenant-isolation scrutiny as
// disjuncts 1-4 (see eshu-code-review's scoped-route guidance) before
// shipping.
//
// `scalars` MUST be the slice returned by scopeGrantInlineScalars for the same
// grant set the params bind (see infraResourceAggregateParams and the search /
// relationship handlers). The predicate renders only in scoped mode; the
// unscoped query shape for shared / admin / local callers is unchanged.
func infraResourceScopePredicate(alias string, scalars []string) string {
	disjuncts := infraResourceScopeCoreDisjuncts(alias, scalars)
	if defines := scopeGrantInlineMapDisjunction(alias, scopeHopInbound, "DEFINES", "Repository", "id", scalars); defines != "" {
		disjuncts = append(disjuncts, defines)
	}
	return "(" + strings.Join(disjuncts, " OR ") + ")"
}

// infraResourceScopeCoreDisjuncts returns disjuncts 1-3 of
// infraResourceScopePredicate's doc comment (direct ownership, CloudResource
// via USES, WorkloadInstance via DEPLOYMENT_SOURCE) WITHOUT disjunct 4
// (Workload via DEFINES). Every disjunct here resolves through a durable,
// per-node property or a forward-anchored deployment-source edge -- never
// through reachability into a shared graph identity.
//
// Disjunct 4 is deliberately excluded from this shared core: it admits a bare
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
func infraResourceScopeCoreDisjuncts(alias string, scalars []string) []string {
	disjuncts := []string{
		alias + ".repo_id IN $allowed_repository_ids",
		alias + ".repo_id IN $allowed_scope_ids",
		alias + ".id IN $allowed_repository_ids",
		alias + ".id IN $allowed_scope_ids",
	}
	if uses := scopeGrantInlineMapDisjunction(alias, scopeHopInbound, "USES", "WorkloadInstance", "repo_id", scalars); uses != "" {
		disjuncts = append(disjuncts, uses)
	}
	disjuncts = append(disjuncts,
		"EXISTS { MATCH ("+alias+")-[:DEPLOYMENT_SOURCE]->(scopeDeployRepo:Repository) "+
			"WHERE (scopeDeployRepo.id IN $allowed_repository_ids OR scopeDeployRepo.id IN $allowed_scope_ids) }")
	return disjuncts
}
