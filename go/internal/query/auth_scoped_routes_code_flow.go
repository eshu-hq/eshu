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

// scopedCodeGraphGrantRoute reports whether a graph-backed code route binds the
// caller's repository grant inside its own query. Like the content routes
// above, each resolves the grant before the read and returns an empty page for
// a grantless scoped caller without touching a backend.
//
// Per route, the binding is:
//
//   - POST /api/v0/code/dead-code, /dead-code/investigate, and
//     /dead-code/cross-repo -- CodeHandler.deadCodeCandidateRows
//     (code_dead_code_scan.go), the one candidate read all three share. Its SQL
//     backend gains `repo_id = ANY($n)` (content_reader_dead_code_candidates.go)
//     and its graph backend gains the `r.id IN $allowed_*` predicate on the
//     Repository anchor (buildDeadCodeGraphCypherForLabel, code_dead_code.go);
//     every probe downstream is keyed on entity ids that read already returned.
//     cross-repo additionally keeps its consumer-side post-filter,
//     filterCrossRepoDeadCodeEvidence.
//   - POST /api/v0/code/call-graph/metrics -- callGraphMetricsEdgesCypher
//     (code_call_graph_metrics.go) carries the grant on both CALLS endpoints.
//     repo_id is mandatory on this route and the selector already resolves it
//     through the grant, so the predicate is defense in depth and provably
//     row-set-neutral; the empty-grant refusal in callGraphMetricsData is the
//     part that bites when the read is reached without the selector.
func scopedCodeGraphGrantRoute(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v0/code/dead-code",
		"/api/v0/code/dead-code/investigate",
		"/api/v0/code/dead-code/cross-repo",
		"/api/v0/code/call-graph/metrics":
		return true
	default:
		return false
	}
}
