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
// produce: TruthBasisAuthoritativeGraph, TruthBasisHybrid, and
// TruthBasisContentIndex are the bases language_queries.go's reading dispatch
// branches pass in, and TruthBasisNoBackendRead is the one the empty-grant page
// passes in without reading anything. The default arm returns the "unavailable"
// sentinel (the same fallback code_relationship_story.go:301 uses for its own
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
	case TruthBasisNoBackendRead:
		return noBackendReadSourceBackend
	default:
		return "unavailable"
	}
}

// noBackendReadSourceBackend is the source_backend an empty-grant page carries
// on both routes in this family: language-query's
// writeLanguageQueryEmptyGrantResult and imports/investigate's grantless
// branch. One constant, because the two pages must not drift apart on it.
//
// It is the wire spelling of TruthBasisNoBackendRead and matches it exactly, so
// the two fields on such a page say the same thing rather than one of them
// borrowing a value that means something else. Until #6544 this constant was
// the "unavailable" sentinel, reused outside its documented meaning because
// neither vocabulary had a member for a page produced without a read;
// sourceBackendForTruthBasis now derives this value from the basis like every
// other outcome, and "unavailable" is once again only the default arm's
// defensive fallback for a basis this route does not recognize. The public
// source_backend table in docs/public/reference/language-query-dsl.md carries
// both values as separate rows.
const noBackendReadSourceBackend = "no_backend_read"

// writeLanguageQueryEmptyGrantResult writes the page a scoped caller with no
// repository grants gets: the same body every other branch returns, but with
// the basis and source_backend that say nothing served it. It stays separate
// from writeLanguageQueryResult, which is reached only from the four reading
// dispatch branches and always names one of their observed bases; routing this
// page through it would mean threading a basis no read produced through the
// reading writer.
func (h *LanguageQueryHandler) writeLanguageQueryEmptyGrantResult(
	w http.ResponseWriter,
	r *http.Request,
	language, entityType, query string,
) {
	body := languageQueryResponseBody(language, entityType, query, []map[string]any{})
	body["source_backend"] = noBackendReadSourceBackend
	WriteSuccess(w, r, http.StatusOK, body, BuildTruthEnvelope(
		h.profile(), languageQueryCapability, TruthBasisNoBackendRead, reasonEmptyGrantNoBackendRead,
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
