// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "net/http"

// languageQueryGraphBackedReason describes what queryByLanguage (the
// graphBackedEntityTypes dispatch branch) actually observed serving the
// result, keyed off the TruthBasis it returned. Before #5761's P1-1 review
// fix this branch always claimed reasonLanguageQueryGraphOnly even when the
// nil-Neo4j fallback or a content-store merge served the answer instead; this
// keeps the reason honest for every outcome queryByLanguageWithSemanticFilter
// can return.
func languageQueryGraphBackedReason(basis TruthBasis) string {
	switch basis {
	case TruthBasisContentIndex:
		return "no graph reader was configured for this entity type; the content-store fallback served the result"
	case TruthBasisHybrid:
		return "graph-only read served this entity type, enriched with content-store metadata"
	default:
		return reasonLanguageQueryGraphOnly
	}
}

// languageQueryGraphFirstReason describes what
// queryGraphFirstContentByLanguageWithSemanticFilter actually observed
// serving the result, keyed off the TruthBasis it returned. filterNote is
// appended for the "guard" entity type's extra semantic_kind=guard filter;
// the graphFirstContentBackedEntityTypes branch passes "". Before #5761's
// P1-1 review fix both graph-first branches always claimed TruthBasisHybrid
// with a reason stating "the serving source is not reported" -- that stopped
// being true once the handler started threading the real basis back from the
// helper, so the reason is now computed per-request instead of hardcoded.
func languageQueryGraphFirstReason(basis TruthBasis, filterNote string) string {
	switch basis {
	case TruthBasisContentIndex:
		return "graph-first content-fallback read" + filterNote + "; the graph read returned no rows (or no graph reader was configured), so the content-store fallback served the result"
	case TruthBasisHybrid:
		return "graph-first content-fallback read" + filterNote + "; the graph served the result, enriched with content-store metadata"
	default:
		return "graph-first content-fallback read" + filterNote + "; the graph served the result"
	}
}

// sourceBackendForTruthBasis derives the response's source_backend field from
// the basis the dispatch branch actually observed, mirroring code_symbol.go's
// source_backend field. It is derived rather than threaded as its own return
// value because basis already distinguishes every outcome this route can
// produce today: TruthBasisAuthoritativeGraph, TruthBasisHybrid, and
// TruthBasisContentIndex are the only bases language_queries.go's dispatch
// branches ever pass in. The default arm returns the "unavailable" sentinel
// (the same fallback code_relationship_story.go:301 uses for its own
// source_backend field) rather than an empty string: an empty string is not
// in the OpenAPI LanguageQueryResponse.source_backend enum
// (openapi_components.go), so it would be an undocumented, silently-wrong
// value on the wire if a future basis (e.g. TruthBasisSemanticFacts or
// TruthBasisRuntimeState) ever reached this route without a matching case
// added here.
func sourceBackendForTruthBasis(basis TruthBasis) string {
	switch basis {
	case TruthBasisAuthoritativeGraph:
		return "graph"
	case TruthBasisHybrid:
		return "hybrid_graph_and_content"
	case TruthBasisContentIndex:
		return "postgres_content_store"
	default:
		return "unavailable"
	}
}

// noBackendReadSourceBackend is the source_backend an empty-grant page carries
// on both routes in this family: language-query's
// writeLanguageQueryEmptyGrantResult and imports/investigate's grantless
// branch. One constant, because the two pages must not drift apart on it.
//
// It reuses the "unavailable" sentinel OUTSIDE its documented meaning, and that
// is deliberate rather than accidental. sourceBackendForTruthBasis has no
// no-read case: "unavailable" is its default arm, for a basis this route does
// not recognize. Neither does the TruthBasis enum have a member meaning "no
// read happened", which is why the empty page reports content_index -- the
// lowest-claim basis available -- rather than a description of what occurred.
// Given those two gaps, naming a backend that was never read would be worse
// than reusing the one value on the wire that claims nothing. The public
// source_backend table in docs/public/reference/language-query-dsl.md carries
// this second meaning so a caller reading the field is not misled by the
// original one. A no-read TruthBasis member would let both routes say this
// properly and is worth its own issue.
const noBackendReadSourceBackend = "unavailable"

// writeLanguageQueryEmptyGrantResult writes the page a scoped caller with no
// repository grants gets: the same body every other branch returns, but with
// source_backend saying that nothing served it. It cannot go through
// writeLanguageQueryResult, which derives source_backend from the basis and
// would answer postgres_content_store for a page that read no content store.
func (h *LanguageQueryHandler) writeLanguageQueryEmptyGrantResult(
	w http.ResponseWriter,
	r *http.Request,
	language, entityType, query string,
) {
	body := languageQueryResponseBody(language, entityType, query, []map[string]any{})
	body["source_backend"] = noBackendReadSourceBackend
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(), languageQueryCapability, TruthBasisContentIndex, reasonEmptyGrantNoBackendRead,
	))
}

// languageQueryResponseBody is the response shape every branch of this route
// returns, without source_backend, which the callers set: derived from the
// truth basis for a real read, and noBackendReadSourceBackend for a page that
// took none. Shared so the two writers cannot drift on the other four keys.
func languageQueryResponseBody(language, entityType, query string, results []map[string]any) map[string]any {
	return map[string]any{
		"language":    language,
		"entity_type": entityType,
		"query":       query,
		"results":     results,
	}
}
