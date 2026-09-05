// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// scopedCodeFlowRoute reports whether a code-flow API route is safe for scoped
// tokens. The handlers require repo_id, apply AuthContext repository filtering
// before the store read, and return bounded counts, truncation, ambiguity, and
// unsupported-language states from that already-filtered scope.
func scopedCodeFlowRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/code/flow/taint-path",
		"/api/v0/code/flow/reaching-def",
		"/api/v0/code/flow/cfg-summary",
		"/api/v0/code/flow/pdg-summary":
		return true
	default:
		return false
	}
}

// scopedCodeContentGrantRoute reports whether a content-index-backed code route
// binds the caller's repository grant inside its own SQL. Each handler resolves
// the grant through codeContentGrantScope (code_repository_selector.go) before
// the read: an empty grant returns the route's empty page without touching the
// content store, and a corpus-wide search (no repo_id) carries the caller's
// granted repository ids into the statement's WHERE, so the LIMIT/OFFSET page
// is taken from the granted set rather than a cross-tenant-polluted one.
//
// Per route, the binding is:
//
//   - POST /api/v0/code/topics/investigate -- codeTopicFilters'
//     `repo_id = ANY($n)` branch (content_reader_code_topic.go), reached from
//     CodeHandler.codeTopicRows.
//   - POST /api/v0/code/security/secrets/investigate -- hardcodedSecretFilters'
//     grant branch (content_reader_security_secrets.go), reached from
//     CodeHandler.hardcodedSecretRows.
//   - POST /api/v0/code/symbols/search -- symbolSearchFilters' grant branch
//     (content_reader_symbol_search.go) on the batched path, and
//     CodeHandler.symbolNameFallbackEntities querying granted repositories one
//     at a time on the name-lookup fallback. The graph path was already gated
//     by searchGraphEntitiesWithExact (code.go).
//   - POST /api/v0/code/structure/inventory -- structuralInventoryWhere's grant
//     branch (content_reader_structural_inventory.go), covering both the entity
//     read and the per-file function count.
//
// All four share one predicate builder, appendRepositoryGrantFilter
// (content_reader_code_topic.go), so the SQL text cannot drift apart per route.
func scopedCodeContentGrantRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/code/topics/investigate",
		"/api/v0/code/security/secrets/investigate",
		"/api/v0/code/symbols/search",
		"/api/v0/code/structure/inventory":
		return true
	default:
		return false
	}
}

// scopedCodeGraphGrantRoute reports whether a graph-backed code route is bound
// to the caller's repository grant before it reads. Like the content routes
// above, each resolves the grant first and returns an empty page for a
// grantless scoped caller without touching a backend.
//
// Most of these bind the grant inside the query itself. One does not:
// call-graph metrics is bound by a mandatory selector the router already
// resolved through the grant, and deliberately keeps a single query text for
// every caller. Per route, the binding is:
//
//   - POST /api/v0/code/dead-code, /dead-code/investigate, and
//     /dead-code/cross-repo -- CodeHandler.deadCodeCandidateRows
//     (code_dead_code_scan.go), the one candidate read all three share. Its SQL
//     backend gains `repo_id = ANY($n)` (content_reader_dead_code_candidates.go)
//     and its graph backend gains the `r.id IN $allowed_*` predicate on the
//     Repository anchor (buildDeadCodeGraphCypherForLabel, code_dead_code.go);
//     every probe downstream is keyed on entity ids that read already returned.
//     Two of those probes bind the grant again on the consumer side, because a
//     consumer lives outside the producer's repository by definition: the
//     incoming-edge probe projects it per row as in_grant
//     (buildDeadCodeScopedIncomingBatchProbeCypher,
//     code_dead_code_candidate_entity.go), and cross-repo binds it in the
//     consumer-evidence page read ahead of that read's LIMIT
//     (crossRepoDeadCodeConsumerReadPlan, code_dead_code_cross_repo_filter.go)
//     on top of the Go-side filterCrossRepoDeadCodeEvidence.
//   - POST /api/v0/code/call-graph/metrics -- bound by its mandatory repo_id,
//     not by a predicate in its query. repo_id is required by
//     callGraphMetricsRequest.validate, and applyRepositorySelectorForCapability
//     resolves it against the caller's grant and rejects an ungranted one with
//     400 before the handler body runs. callGraphMetricsEdgesCypher
//     (code_call_graph_metrics.go) therefore carries no grant of its own: it
//     anchors both CALLS endpoints on {repo_id: $repo_id} and nothing else, so
//     every caller runs the one query text the plan manifest pins for
//     QP-CALL-GRAPH-HUBS and QP-CALL-GRAPH-RECURSIVE. The empty-grant refusal in
//     callGraphMetricsData is what bites when the read is reached without the
//     selector: a grantless scoped caller gets the empty response without
//     touching the graph.
//   - POST /api/v0/code/quality/inspect -- buildCodeQualityCypher
//     (code_quality.go) appends the grant to the same MATCH-attached WHERE its
//     optional filters use, so it lands before the SKIP/LIMIT.
//   - POST /api/v0/code/complexity -- all three builders in
//     code_complexity_queries.go: the ranked list, the by-name lookup (whose
//     ambiguity candidate list would otherwise name ungranted repositories),
//     and the by-entity-id lookup, which previously carried no repository
//     predicate at all and ignored even a repo_id the caller supplied.
//   - POST /api/v0/code/language-query -- both backends, like dead-code above.
//     Its Cypher half is buildLanguageCypherWithSemanticFilter
//     (language_query_cypher.go), whose four builders append the grant to the
//     anchoring MATCH's own WHERE; its SQL half is
//     buildLanguageTypeEntityFilters (content_reader_entity_search.go), which
//     serves the content-only entity types, the graphless and zero-row
//     fallbacks, and the metadata enrichment pass. This route is owned by
//     LanguageQueryHandler rather than CodeHandler, so it reaches the family's
//     selector and grant helpers through the free functions
//     applyRepositorySelectorForAccess and codeContentGrantScope
//     (code_repository_selector.go) instead of CodeHandler methods.
//   - POST /api/v0/code/imports/investigate -- all seven builders in
//     code_import_dependencies_queries.go, through
//     importDependencyGrantPredicates. Each writes its predicates via
//     writeCypherPredicates, which attaches its WHERE to the single anchoring
//     MATCH, so the grant lands ahead of SKIP/LIMIT on the paged builders and
//     ahead of LIMIT $scan_limit on the three that page in Go.
//     crossModuleCallRowsCypher binds source_repo and target_repo
//     independently: the Go pass that drops a mismatched pair runs after the
//     scan, so binding only the caller side would still spend the 25,000-row
//     budget on callees the caller may not see.
//   - POST /api/v0/code/relationships/story -- the grant lands in each
//     statement's ANCHORING MATCH, on the entity nodes' own repo_id, through
//     relationshipStoryRepoPredicates and relationshipStoryGrantPredicates
//     (code_relationship_story_graph.go). That placement is the whole point:
//     this route already carried grant text before #5167 batch 2b, written on
//     the sourceRepo/targetRepo aliases its OPTIONAL MATCH clauses bind, and a
//     live run against the pinned NornicDB measured it dropping no row at all.
//     Six statements carry it -- the direct read on both backends, class
//     methods on both, the inheritance walk on both -- plus the repo-scoped
//     override read, whose OVERRIDES target can leave the anchored repository.
//     Target resolution is bound too: an ambiguous name used to fall through to
//     SearchEntitiesByNameAnyRepo and list every tenant's candidate.
//   - POST /api/v0/code/call-chain -- the grant lands on the target node's own
//     repo_id in the anchoring MATCH of nornicDBCallChainOneHopRows
//     (code_call_chain_nornicdb.go) and callChainCandidateOneHopRows
//     (code_call_chain_resolution.go), and on both endpoints of the two
//     shortestPath builders. The NornicDB response path is a Go-side
//     breadth-first search over that one-hop read, so bounding each hop bounds
//     the whole chain; that is what it takes, because the
//     all(node IN nodes(path) ...) path-wide form the shortestPath builders use
//     does not evaluate a list membership test on the pinned build. The shared
//     metadata anchor (nornicDBRelationshipMetadataCypher) binds on its
//     Repository alias instead, which is correct there and not here: that
//     statement reaches Repository through two required MATCH clauses.
//     resolveExactGraphEntityCandidates carries a defense-in-depth grant check
//     because its rows become an ambiguity error that names entity ids.
func scopedCodeGraphGrantRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/code/dead-code",
		"/api/v0/code/dead-code/investigate",
		"/api/v0/code/dead-code/cross-repo",
		"/api/v0/code/call-graph/metrics",
		"/api/v0/code/quality/inspect",
		"/api/v0/code/complexity",
		"/api/v0/code/language-query",
		"/api/v0/code/imports/investigate",
		"/api/v0/code/relationships/story",
		"/api/v0/code/call-chain":
		return true
	default:
		return false
	}
}
