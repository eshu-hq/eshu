// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

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

// ScopeHopDirection selects the relationship arrow used by
// ScopeGrantInlineMapDisjunction relative to the bound alias.
type ScopeHopDirection int

const (
	// ScopeHopInbound builds `(alias)<-[:relType]-(:targetLabel {targetProp:$g})`,
	// i.e. the target node points at the bound alias (e.g. a WorkloadInstance
	// USES the CloudResource, a Repository DEFINES the Workload).
	ScopeHopInbound ScopeHopDirection = iota
	// ScopeHopOutbound builds `(alias)-[:relType]->(:targetLabel {targetProp:$g})`,
	// i.e. the bound alias points at the target node.
	ScopeHopOutbound
)

// MaxScopeGrantInlineTerms caps the O(grant) inline-map OR-chain fan-out.
//
// Past this many grants, ScopeGrantInlineScalars truncates the inline-map terms
// and reports capped=true. This degradation is FAIL-CLOSED and safe by
// construction: the composed scope predicates always OR the truncated inline-map
// disjunction together with the flat `alias.repo_id IN $allowed_repository_ids`
// array disjuncts, which admit ALL direct-ownership grants in O(1) regardless of
// the cap. So a pathological token with more than MaxScopeGrantInlineTerms
// grants still sees every resource it directly owns; it loses only
// collision-defined / bridge admission for grants beyond the cap — an
// under-authorization (missing rows), never a leak (extra rows). Do NOT "fix"
// this by removing the cap: the cap bounds a per-node OR-chain cost that would
// otherwise grow without limit, and fail-closed degradation is the correct
// posture under the accuracy/performance life motto. 128 comfortably covers
// realistic multi-repo grants (tens); the boundary cold cost is bounded (~1.4s)
// and warm cost negligible.
const MaxScopeGrantInlineTerms = 128

// ScopeGrantInlineParamPrefix names the per-grant scalar params bound by
// BindScopeGrantInlineScalars and referenced by ScopeGrantInlineMapDisjunction
// (keys scope_grant_0 .. scope_grant_{n-1}). Reusing this prefix across multiple
// disjunctions in one query is safe: every disjunction binds the same ordered
// scalar values to the same keys, so the writes are idempotent.
const ScopeGrantInlineParamPrefix = "scope_grant_"

// ScopeGrantInlineScalars returns the deduplicated, deterministically ordered
// union of granted repository and ingestion-scope ids used to build SHAPE-A
// inline-map disjunctions, truncated to MaxScopeGrantInlineTerms. Empty ids are
// dropped. capped reports whether truncation dropped grants (callers may log or
// telemeter it; correctness is preserved by the flat array disjuncts — see
// MaxScopeGrantInlineTerms). The predicate builder and the param binder MUST use
// the SAME returned slice so their scalar keys and count agree exactly.
func ScopeGrantInlineScalars(repositoryIDs, scopeIDs []string) (scalars []string, capped bool) {
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
	if len(union) > MaxScopeGrantInlineTerms {
		return union[:MaxScopeGrantInlineTerms], true
	}
	return union, false
}

// ScopeGrantInlineMapDisjunction builds a NornicDB-safe pattern-predicate
// OR-chain that admits the bound `alias` when it is connected — in `direction`
// via `relType` — to a `targetLabel` node whose `targetProp` equals one of the
// granted ids. Each scalar in `scalars` becomes one inline-map pattern term
// binding a distinct scalar param scope_grant_<i>. The returned fragment is
// parenthesized and safe to OR into a larger WHERE predicate. It returns the
// empty string when `scalars` is empty (the caller composes the flat array
// disjuncts around it, so an empty inline-map contributes nothing and the
// predicate stays fail-closed). Bind the referenced params with
// BindScopeGrantInlineScalars using the SAME `scalars` slice.
//
// relType, targetLabel, and targetProp are fixed, code-owned identifiers (never
// request input); they are interpolated unquoted because the pinned NornicDB
// build does not match backtick-quoted labels. The grant values flow only
// through bound parameters.
func ScopeGrantInlineMapDisjunction(
	alias string,
	direction ScopeHopDirection,
	relType, targetLabel, targetProp string,
	scalars []string,
) string {
	if len(scalars) == 0 {
		return ""
	}
	left, right := "<-[:", "]-"
	if direction == ScopeHopOutbound {
		left, right = "-[:", "]->"
	}
	terms := make([]string, 0, len(scalars))
	for i := range scalars {
		param := "$" + ScopeGrantInlineParamPrefix + strconv.Itoa(i)
		terms = append(terms, "("+alias+")"+left+relType+right+"(:"+targetLabel+" {"+targetProp+":"+param+"})")
	}
	return "(" + strings.Join(terms, " OR ") + ")"
}

// BindScopeGrantInlineScalars binds scope_grant_<i> = scalars[i] into params,
// matching the keys ScopeGrantInlineMapDisjunction references. Call it with the
// exact slice returned by ScopeGrantInlineScalars. It is a no-op for empty
// scalars. Re-binding the same keys in one params map is safe (idempotent).
func BindScopeGrantInlineScalars(params map[string]any, scalars []string) {
	for i, value := range scalars {
		params[ScopeGrantInlineParamPrefix+strconv.Itoa(i)] = value
	}
}

// ScopeGrantInlineScalars returns the capped SHAPE-A inline-map scalar set for a
// scoped access filter (the union of its granted repository and ingestion-scope
// ids). It is the access-filter companion to the package-level
// ScopeGrantInlineScalars and guarantees the predicate builders and param
// binders derive an identical ordered slice from one source. Shared / admin /
// local callers (unscoped) get an empty slice.
//
// Callers discard the returned capped bool (scalars, _ :=). That is safe, not
// an oversight: capping only truncates the inline-map (USES /
// DEFINES-collision) admission families, which is fail-closed — a
// >MaxScopeGrantInlineTerms-grant token loses collision/USES admission for the
// overflow (missing rows, never extra) while the direct-ownership and
// DEPLOYMENT_SOURCE families still admit.
//
// Operators learn about the cap through GrantInlineCapExceeded, which handlers
// consult once per read, rather than through this return value. See #5408 and
// the comment on GrantInlineCapExceeded for why the signal is not emitted from
// here.
func (f RepositoryAccessFilter) ScopeGrantInlineScalars() (scalars []string, capped bool) {
	if !f.Scoped() {
		return nil, false
	}
	return ScopeGrantInlineScalars(f.AllowedRepositoryIDs, f.AllowedScopeIDs)
}

// GrantInlineCapExceeded reports whether this filter's grant set overflows
// MaxScopeGrantInlineTerms, so a handler can emit one operator signal per
// degraded read (#5408).
//
// This exists instead of returning the cap out of the string-builder call
// sites, for two reasons.
//
// The cap is a property of the TOKEN's grant set, not of any individual clause:
// it depends only on the filter's id union, so asking the filter is asking the
// thing that actually decides. And a single request calls
// ScopeGrantInlineScalars more than once — infraSearchScopeClause alone calls
// it three times — so emitting per call site would record one degraded read as
// three, and "how many reads lost USES admission" would no longer be
// answerable from the metric.
//
// It shares ScopeGrantInlineScalars' truncation rule by calling it rather than
// recomputing the comparison, because a second copy of "len(union) >
// MaxScopeGrantInlineTerms" could drift from the builder and then report a
// degradation that did not happen.
func (f RepositoryAccessFilter) GrantInlineCapExceeded() bool {
	_, capped := f.ScopeGrantInlineScalars()
	return capped
}
