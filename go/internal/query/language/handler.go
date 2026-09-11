// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// languageQueryCapability is the catalog capability this route reports in a
// bounded graph-read error envelope and gates on before touching GraphQuery or
// ContentStore.
//
// This is a route-level capability minted for this route (#5761), not a reused
// id. The route's own MCP tool, execute_language_query, is already bound to
// five symbol_graph.* facets (decorators, argument_names, class_methods,
// imports, inheritance) in specs/capability-matrix.v1.yaml, but each of those
// names one specific semantic facet -- none of them describe "look up entities
// of kind K in language L", which is what this route actually does across its
// graph-backed, graph-first-content, and content-only entity-type families.
// code_search.symbol_lookup (owned by code_symbol.go's
// POST /api/v0/code/symbols/search) was considered and rejected: it is a
// different route with different failure semantics, and sharing one id across
// two unrelated routes would make an operator's capability-keyed triage
// ambiguous about which route actually failed.
const languageQueryCapability = "symbol_graph.language_entities"

// Truth-envelope reasons. Only contentBackedEntityTypes has an invariant
// basis and a fixed reason: it is always a pure content-store read with no
// graph involved at all, so reasonLanguageQueryContentOnly is used directly
// at its call site below. graphBackedEntityTypes' basis is NOT invariant --
// queryByLanguageWithSemanticFilter can return TruthBasisContentIndex (nil
// Neo4j fallback), TruthBasisHybrid (content-merge), or
// TruthBasisAuthoritativeGraph, so its reason is computed per-request by
// languageQueryGraphBackedReason, whose default arm returns
// reasonLanguageQueryGraphOnly for the authoritative_graph case. The two
// graph-first branches (graphFirstContentBackedEntityTypes and the "guard"
// entity type) can likewise serve from either backend or merge both, so their
// reason is computed per-request by languageQueryGraphFirstReason from the
// TruthBasis queryGraphFirstContentByLanguageWithSemanticFilter actually
// observed -- see reasons.go.
const (
	reasonLanguageQueryGraphOnly   = "graph-only read served this entity type"
	reasonLanguageQueryContentOnly = "content-store read served this entity type"
	// reasonEmptyGrantNoBackendRead describes the empty page a scoped caller
	// with no repository grants gets. BOTH routes in this family use it --
	// language-query here and imports/investigate's grantless branch -- so the
	// two pages cannot be reworded apart. It is their normal success shape,
	// answered without reaching any backend.
	//
	// It does NOT hide the grantless case from the caller: this string is
	// serialized in the truth envelope's reason, so a grantless caller can tell
	// its empty page from a granted search that matched nothing. That is
	// deliberate -- the distinction is useful to the caller and reveals only
	// its own grant, which it already knows. What stays unprobeable is the
	// INDEX: neither answer says whether any repository, entity or row exists,
	// because no backend was read to find out.
	reasonEmptyGrantNoBackendRead = querycontract.ReasonEmptyGrantNoBackendRead
)

// languageQueryMaxLimit bounds the caller-supplied limit before it reaches
// any of the four cypher.go builders, all of which splice params["limit"]
// verbatim into `LIMIT $limit` with no ceiling of their own. Mirrors the
// sibling code_symbol.go symbolSearchMaxLimit (200) on the nearest sibling
// route in package query.
const languageQueryMaxLimit = 200

// Handler provides language-specific entity queries against the graph and
// content store. Graph-backed entity types use Neo4j. Content-only entity
// types use the Postgres content store.
//
// Package query keeps this type available as LanguageQueryHandler through a
// type alias in language_alias.go so existing wiring, callers, and tests
// compile unchanged (#6642).
type Handler struct {
	Neo4j   querycontract.GraphQuery
	Content querycontract.ContentStore
	Profile querycontract.QueryProfile
	// Logger records the unmodified cause behind a generic language-query
	// failure (one that WriteGraphReadError does not recognize as a bounded
	// graph-read sentinel) to the operator log, while the response body
	// stays the static "language query failed" message. Nil is tolerated so
	// existing constructions that do not set it keep working; logging is
	// skipped in that case.
	Logger *slog.Logger
}

// profile returns the handler's normalized query profile, defaulting to
// production for a nil handler. NormalizeQueryProfile maps an empty or
// invalid profile string to "", which maxTruthLevel's default case also
// resolves to ProductionMax, so both paths land on production's ceiling.
// For symbol_graph.language_entities, production is the MOST PERMISSIVE
// profile (every entity-type family is supported there), so this default is
// fail-OPEN, not fail-closed -- do not repeat the "fail closed" framing here.
// The identical CodeHandler.profile() (package codequery) carries no such
// claim.
func (h *Handler) profile() querycontract.QueryProfile {
	if h == nil {
		return querycontract.ProfileProduction
	}
	return querycontract.NormalizeQueryProfile(string(h.Profile))
}

// logQueryFailure records the unmodified failure cause and a bounded
// failure_class to the operator log while the response body stays static,
// before the handler answers a generic 500. failureClass
// identifies which of the route's guarded call sites failed
// (language_query.guard, language_query.graph_backed,
// language_query.graph_first_content_backed, or
// language_query.content_backed) so an operator can tell which entity-type
// family is degraded without the response body carrying any backend detail.
// A nil Logger is tolerated; logging is skipped.
func (h *Handler) logQueryFailure(ctx context.Context, failureClass, language, entityType string, err error) {
	if h == nil || h.Logger == nil {
		return
	}
	h.Logger.WarnContext(
		ctx, "language query failed",
		"failure_class", failureClass,
		"language", language,
		"entity_type", entityType,
		"error", err,
	)
}

// Mount registers the language query endpoint on the given mux.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/code/language-query", h.handleLanguageQuery)
}

// handleLanguageQuery dispatches a language-specific entity query.
func (h *Handler) handleLanguageQuery(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(r, telemetry.SpanQueryLanguageQuery, "POST /api/v0/code/language-query", languageQueryCapability)
	defer span.End()

	var req struct {
		Language   string `json:"language"`
		EntityType string `json:"entity_type"`
		Query      string `json:"query"`
		RepoID     string `json:"repo_id"`
		Limit      int    `json:"limit"`
	}
	if err := querycontract.ReadJSON(r, &req); err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Language == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "language is required")
		return
	}
	if req.EntityType == "" {
		querycontract.WriteError(w, http.StatusBadRequest, "entity_type is required")
		return
	}

	req.Language = canonicalLanguage(req.Language)
	req.EntityType = strings.ToLower(strings.TrimSpace(req.EntityType))

	if !supportedLanguages[req.Language] {
		querycontract.WriteError(w, http.StatusBadRequest, fmt.Sprintf(
			"unsupported language %q; supported: %s",
			req.Language, joinKeys(supportedLanguages),
		))
		return
	}

	if querycontract.CapabilityUnsupported(h.profile(), languageQueryCapability) {
		h.writeLanguageQueryUnsupportedCapability(w, r, "language query requires a supported query profile")
		return
	}

	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > languageQueryMaxLimit {
		req.Limit = languageQueryMaxLimit
	}

	// Entity-type validity belongs to the request, not to the caller's grant,
	// so it is answered here rather than by the dispatch tail below -- which
	// sits after the empty-grant short-circuit. See acceptLanguageQueryEntityType.
	if !acceptLanguageQueryEntityType(w, req.EntityType) {
		return
	}

	// #5167 batch 2a. This route is owned by Handler, not CodeHandler, so
	// req.RepoID used to reach both backends raw: never resolved through
	// queryselector and never checked against the caller's grant. Both
	// halves of the family's fix apply here through free functions rather
	// than a second copy of the plumbing -- the selector is resolved and an
	// ungranted one rejected with 400, then the grant the remaining reads
	// bind is resolved once for all four branches.
	if !codequery.ApplyRepositorySelectorForAccess(w, r, h.Neo4j, h.Content, &req.RepoID, languageQueryCapability) {
		return
	}
	grant, blocked := codequery.LanguageQueryGrantFor(r.Context(), req.RepoID)
	if blocked {
		h.writeLanguageQueryEmptyGrantResult(w, r, req.Language, req.EntityType, req.Query)
		return
	}

	if req.EntityType == "guard" {
		results, basis, err := h.queryGraphFirstContentByLanguageWithSemanticFilter(
			r.Context(),
			req.Language,
			"Function",
			"guard",
			req.Query,
			req.RepoID,
			req.Limit,
			"semantic_kind",
			"guard",
			grant,
		)
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, languageQueryCapability) {
				return
			}
			h.logQueryFailure(r.Context(), "language_query.guard", req.Language, req.EntityType, err)
			querycontract.WriteError(w, http.StatusInternalServerError, "language query failed")
			return
		}

		h.writeLanguageQueryResult(w, r, req.Language, req.EntityType, req.Query, results,
			basis, languageQueryGraphFirstReason(basis, " with a semantic_kind=guard filter"))
		return
	}

	if label, ok := graphBackedEntityTypes[req.EntityType]; ok {
		results, basis, err := h.queryByLanguage(r.Context(), req.Language, label, req.Query, req.RepoID, req.Limit, grant)
		if err != nil {
			if errors.Is(err, errLanguageQueryGraphOnlyEntityUnavailable) {
				h.writeLanguageQueryUnsupportedCapability(w, r,
					fmt.Sprintf("entity type %q requires a graph backend", req.EntityType))
				return
			}
			if querycontract.WriteGraphReadError(w, r, err, languageQueryCapability) {
				return
			}
			h.logQueryFailure(r.Context(), "language_query.graph_backed", req.Language, req.EntityType, err)
			querycontract.WriteError(w, http.StatusInternalServerError, "language query failed")
			return
		}

		h.writeLanguageQueryResult(w, r, req.Language, req.EntityType, req.Query, results,
			basis, languageQueryGraphBackedReason(basis))
		return
	}

	if label, ok := graphFirstContentBackedEntityTypes[req.EntityType]; ok {
		results, basis, err := h.queryGraphFirstContentByLanguage(
			r.Context(),
			req.Language,
			label,
			req.Query,
			req.RepoID,
			req.Limit,
			grant,
		)
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, languageQueryCapability) {
				return
			}
			h.logQueryFailure(r.Context(), "language_query.graph_first_content_backed", req.Language, req.EntityType, err)
			querycontract.WriteError(w, http.StatusInternalServerError, "language query failed")
			return
		}

		h.writeLanguageQueryResult(w, r, req.Language, req.EntityType, req.Query, results,
			basis, languageQueryGraphFirstReason(basis, ""))
		return
	}

	if label, ok := contentBackedEntityTypes[req.EntityType]; ok {
		results, err := h.queryContentByLanguage(r.Context(), req.Language, label, req.Query, req.RepoID, req.Limit, grant)
		if err != nil {
			if querycontract.WriteGraphReadError(w, r, err, languageQueryCapability) {
				return
			}
			h.logQueryFailure(r.Context(), "language_query.content_backed", req.Language, req.EntityType, err)
			querycontract.WriteError(w, http.StatusInternalServerError, "language query failed")
			return
		}

		h.writeLanguageQueryResult(w, r, req.Language, req.EntityType, req.Query, results,
			querycontract.TruthBasisContentIndex, reasonLanguageQueryContentOnly)
		return
	}

	writeLanguageQueryUnsupportedEntityType(w, req.EntityType)
}

// languageQueryGrant is the pre-move spelling of codequery.LanguageQueryGrant.
// The queryplan source_sha256 for queryByLanguageWithSemanticFilter covers
// the grant parameter text, so the alias keeps the moved signature
// byte-identical instead of re-freezing the digest.
type languageQueryGrant = codequery.LanguageQueryGrant

// queryByLanguage builds and executes a language-specific Cypher query.
func (h *Handler) queryByLanguage(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
	grant languageQueryGrant,
) ([]map[string]any, querycontract.TruthBasis, error) {
	return h.queryByLanguageWithSemanticFilter(ctx, language, label, query, repoID, limit, "", "", grant)
}

// queryByLanguageWithSemanticFilter reports the TruthBasis it actually
// observed serving the result, rather than a caller-assumed constant (#5761
// P1-1): TruthBasisContentIndex when h.Neo4j is nil OR not
// querycontract.GraphConfigured (no live graph backend, so the content store
// served it -- #5761 F1: a non-nil but undriven *Neo4jReader, the shape
// wiring.go always constructs for a graphless profile, is treated the same
// as nil here), TruthBasisHybrid when enrichLanguageResultsWithContentMetadata
// merged at least one content value into the graph rows, and
// TruthBasisAuthoritativeGraph otherwise (a pure graph read with nothing for
// the content store to add or no content reader configured).
func (h *Handler) queryByLanguageWithSemanticFilter(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
	grant languageQueryGrant,
) ([]map[string]any, querycontract.TruthBasis, error) {
	if h == nil || !querycontract.GraphConfigured(h.Neo4j) {
		contentLabel := graphLabelToContentEntityType(label)
		if h == nil || contentLabel == "" {
			return nil, "", errLanguageQueryGraphOnlyEntityUnavailable
		}
		results, err := h.queryContentByLanguage(ctx, language, contentLabel, query, repoID, limit, grant)
		if err != nil {
			return nil, "", err
		}
		return results, querycontract.TruthBasisContentIndex, nil
	}

	cypher, params := BuildCypherWithSemanticFilter(
		language,
		label,
		query,
		repoID,
		limit,
		semanticFilterKey,
		semanticFilterValue,
		grant.Access,
	)

	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, "", err
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		results = append(results, buildLanguageResult(row, label))
	}
	results, merged, err := h.enrichLanguageResultsWithContentMetadata(
		ctx,
		results,
		language,
		label,
		query,
		repoID,
		limit,
		grant,
	)
	if err != nil {
		return nil, "", err
	}
	if merged {
		return results, querycontract.TruthBasisHybrid, nil
	}
	return results, querycontract.TruthBasisAuthoritativeGraph, nil
}

func (h *Handler) queryGraphFirstContentByLanguage(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
	grant codequery.LanguageQueryGrant,
) ([]map[string]any, querycontract.TruthBasis, error) {
	return h.queryGraphFirstContentByLanguageWithSemanticFilter(ctx, language, label, label, query, repoID, limit, "", "", grant)
}

// queryGraphFirstContentByLanguageWithSemanticFilter reports the same real
// TruthBasis queryByLanguageWithSemanticFilter observed when the graph served
// non-empty rows, and TruthBasisContentIndex when it fell through to the
// content-store fallback (no live graph backend -- querycontract.GraphConfigured
// is false, #5761 F1 -- or the graph read zero rows) -- see
// queryByLanguageWithSemanticFilter's doc comment for the full outcome set.
//
// contentEntityType names the entity type the content-store fallback queries,
// separately from label (the Neo4j node label the graph read uses). They
// coincide for every graphFirstContentBackedEntityTypes caller (via the
// queryGraphFirstContentByLanguage wrapper above, which passes label for
// both), but the "guard" entity type diverges: its graph read uses
// label="Function" plus a semantic_kind=guard Cypher filter, while its
// content fallback must query contentEntityType="guard" so
// contentEntityTypeFilter (querycontract) applies the matching
// entity_type=Function AND metadata->>semantic_kind=guard predicate. Before
// #5761 F2, the guard call site passed label ("Function") to this fallback
// too, so a graphless or zero-row guard read silently returned every
// Function instead of only guard clauses.
func (h *Handler) queryGraphFirstContentByLanguageWithSemanticFilter(
	ctx context.Context,
	language, label, contentEntityType, query, repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
	grant languageQueryGrant,
) ([]map[string]any, querycontract.TruthBasis, error) {
	if querycontract.GraphConfigured(h.Neo4j) {
		results, basis, err := h.queryByLanguageWithSemanticFilter(
			ctx,
			language,
			label,
			query,
			repoID,
			limit,
			semanticFilterKey,
			semanticFilterValue,
			grant,
		)
		if err != nil {
			return nil, "", err
		}
		if len(results) > 0 {
			return results, basis, nil
		}
	}
	results, err := h.queryContentByLanguage(ctx, language, contentEntityType, query, repoID, limit, grant)
	if err != nil {
		return nil, "", err
	}
	return results, querycontract.TruthBasisContentIndex, nil
}
