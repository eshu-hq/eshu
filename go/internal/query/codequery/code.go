// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CodeHandler provides HTTP routes for code-level queries: search, relationships,
// dead code detection, and complexity metrics.
type CodeHandler struct {
	GraphBackend GraphBackend
	Neo4j        GraphQuery
	Content      ContentStore
	CodeFlow     CodeFlowStore
	Profile      QueryProfile
	// HybridRanker, when set, reorders content-search results by fused
	// BM25+vector relevance using the shipped local embedder. It is optional:
	// when nil the handler serves the lexical content order unchanged.
	HybridRanker codemodel.CodeResultReranker
	// Logger is forwarded to the language-query sub-handler
	// (LanguageQueryHandler.Logger) so a generic language-query failure
	// records its unmodified cause to the operator log while the response
	// body stays static. Nil is tolerated; logging is skipped.
	Logger *slog.Logger
	// ContentRelationships builds an entity's content-derived relationships
	// for the POST /api/v0/code/relationships content fallback
	// (relationshipsFromEntity, code_relationships.go). It is
	// interface-typed rather than a concrete type or func value because
	// assertRouterFieldsWired (cmd/api, cmd/mcp-server wiring completeness
	// tests) only inspects reflect.Interface-kind fields; a nil value here
	// is refused with a 503 rather than silently returning empty
	// relationship lists (#6060).
	ContentRelationships querycontract.ContentRelationshipBuilder
}

// Mount registers all /api/v0/code/* routes on the given mux.
func (h *CodeHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/code/search", h.handleSearch)
	mux.HandleFunc("POST /api/v0/code/symbols/search", h.handleSymbolSearch)
	mux.HandleFunc("POST /api/v0/code/structure/inventory", h.handleStructuralInventory)
	mux.HandleFunc("POST /api/v0/code/topics/investigate", h.handleTopicInvestigation)
	mux.HandleFunc("POST /api/v0/code/security/secrets/investigate", h.handleHardcodedSecretInvestigation)
	mux.HandleFunc("POST /api/v0/code/imports/investigate", h.handleImportDependencyInvestigation)
	mux.HandleFunc("POST /api/v0/code/call-graph/metrics", h.handleCallGraphMetrics)
	mux.HandleFunc("POST /api/v0/code/flow/taint-path", h.handleTaintPath)
	mux.HandleFunc("POST /api/v0/code/flow/reaching-def", h.handleReachingDef)
	mux.HandleFunc("POST /api/v0/code/flow/cfg-summary", h.handleCFGSummary)
	mux.HandleFunc("POST /api/v0/code/flow/pdg-summary", h.handlePDGSummary)
	mux.HandleFunc("POST /api/v0/code/relationships", h.handleRelationships)
	mux.HandleFunc("POST /api/v0/code/relationships/story", h.handleRelationshipStory)
	mux.HandleFunc("POST /api/v0/code/dead-code", h.handleDeadCode)
	mux.HandleFunc("POST /api/v0/code/dead-code/cross-repo", h.handleCrossRepoDeadCode)
	mux.HandleFunc("POST /api/v0/code/dead-code/investigate", h.handleDeadCodeInvestigation)
	mux.HandleFunc("POST /api/v0/code/complexity", h.handleComplexity)
	mux.HandleFunc("POST /api/v0/code/quality/inspect", h.handleCodeQualityInspection)
	mux.HandleFunc("POST /api/v0/code/call-chain", h.handleCallChain)
	mux.HandleFunc("POST /api/v0/code/routes/callers", h.handleRouteToCaller)

	// Read-only Cypher, visualization, and bundle search.
	mux.HandleFunc("POST /api/v0/code/cypher", h.handleCypherQuery)
	mux.HandleFunc("POST /api/v0/code/visualize", h.handleVisualizeQuery)
	mux.HandleFunc("POST /api/v0/code/bundles", h.handleSearchBundles)

	// Language-specific queries mount separately, from APIRouter.Mount via
	// its own Language field (handler.go). LanguageQueryHandler stays in
	// package query -- codequery cannot import it back without an import
	// cycle -- so building and mounting it here is no longer possible once
	// this family moves. See #6060.
}

func (h *CodeHandler) profile() QueryProfile {
	if h == nil {
		return ProfileProduction
	}
	return NormalizeQueryProfile(string(h.Profile))
}

func (h *CodeHandler) graphBackend() GraphBackend {
	if h == nil {
		return GraphBackendNeo4j
	}
	if h.GraphBackend == "" {
		return GraphBackendNeo4j
	}
	backend, err := ParseGraphBackend(string(h.GraphBackend))
	if err != nil {
		panic(err)
	}
	return backend
}

// handleSearch searches code entities by name pattern or content.
func (h *CodeHandler) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string `json:"query"`
		RepoID     string `json:"repo_id"`
		Language   string `json:"language"`
		Limit      int    `json:"limit"`
		Exact      bool   `json:"exact"`
		SearchType string `json:"search_type"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		WriteError(w, http.StatusBadRequest, "query is required")
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, "code_search.fuzzy_symbol") {
		return
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > querycontract.EntityNameSearchMaxLimit {
		req.Limit = querycontract.EntityNameSearchMaxLimit
	}
	probeLimit := codemodel.CodeSearchProbeLimit(req.Limit)
	if req.RepoID == "" && !req.Exact && len([]rune(req.Query)) < 3 {
		WriteError(w, http.StatusBadRequest, "global substring code search requires at least 3 Unicode characters")
		return
	}

	ctx := r.Context()
	capability := "code_search.fuzzy_symbol"
	if strings.EqualFold(strings.TrimSpace(req.SearchType), "variable") {
		capability = "code_search.variable_lookup"
	} else if req.Exact {
		capability = "code_search.exact_symbol"
	}

	if req.RepoID == "" {
		results, err := h.searchGlobalEntityNames(r.Context(), req.Query, req.Language, probeLimit, req.Exact)
		if err != nil {
			if errors.Is(err, querycontract.ErrEntityNameSearchUnavailable) {
				WriteError(w, http.StatusServiceUnavailable, err.Error())
				return
			}
			if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteSuccess(w, r, http.StatusOK, codemodel.CodeSearchPagePayload(
			"content", "postgres_content_name_index", req.Query, "", results, req.Limit,
		), BuildTruthEnvelope(h.profile(), capability, TruthBasisContentIndex, "resolved from the current content entity name index"))
		return
	}

	// Repository-selected search retains the indexed graph query path.
	graphResults, err := h.searchGraphEntitiesWithExact(ctx, req.RepoID, req.Query, req.Language, probeLimit, req.Exact)
	if err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		if WriteGraphReadError(w, r, err, capability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If graph search returns results, return them
	if len(graphResults) > 0 {
		WriteSuccess(w, r, http.StatusOK, codemodel.CodeSearchPagePayload(
			"graph", "graph", req.Query, req.RepoID, graphResults, req.Limit,
		), BuildTruthEnvelope(h.profile(), capability, TruthBasisAuthoritativeGraph, "resolved from graph-backed entity search"))
		return
	}

	// Fall back to content-based search if no graph results
	contentResults, err := h.searchEntityContentWithExact(ctx, req.RepoID, req.Query, req.Language, probeLimit, req.Exact)
	if err != nil {
		if querycontract.WriteContentSubstringIndexUnavailable(w, err) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Re-rank the lexical content results by fused BM25+vector relevance when a
	// hybrid ranker is configured. The ranker is bounded to the already-retrieved
	// result set and falls back to lexical order when no vector/lexical signal is
	// available, so the response never drops a row or invents canonical truth.
	sourceBackend := "postgres_content_store"
	truthDetail := "resolved from content index fallback"
	if h.HybridRanker != nil && !req.Exact {
		if reranked, applied := h.HybridRanker.Rerank(ctx, req.RepoID, req.Query, contentResults); applied {
			contentResults = reranked
			sourceBackend = "hybrid_content_store"
			truthDetail = "resolved from content index fallback ranked by hybrid BM25+vector retrieval"
		}
	}

	WriteSuccess(w, r, http.StatusOK, codemodel.CodeSearchPagePayload(
		"content", sourceBackend, req.Query, req.RepoID, contentResults, req.Limit,
	), BuildTruthEnvelope(h.profile(), capability, TruthBasisContentIndex, truthDetail))
}

// searchGraphEntities finds entities by name pattern in the Neo4j graph.
func (h *CodeHandler) searchGraphEntities(ctx context.Context, repoID, query, language string, limit int) ([]map[string]any, error) {
	return h.searchGraphEntitiesWithExact(ctx, repoID, query, language, limit, false)
}

// buildSearchGraphEntitiesQuery forwards to
// codemodel.BuildSearchGraphEntitiesQuery so searchGraphEntitiesWithExact
// keeps its pre-move declaration bytes: the queryplan source_sha256 for that
// symbol covers the call text, and Go has no function aliases to preserve
// the bare name.
func buildSearchGraphEntitiesQuery(repoID, query, language string, limit int, exact bool, access querycontract.RepositoryAccessFilter) (string, map[string]any) {
	return codemodel.BuildSearchGraphEntitiesQuery(repoID, query, language, limit, exact, access)
}

func (h *CodeHandler) searchGraphEntitiesWithExact(ctx context.Context, repoID, query, language string, limit int, exact bool) ([]map[string]any, error) {
	if strings.TrimSpace(repoID) == "" {
		return nil, querycontract.ErrGlobalGraphEntitySearchUnsupported
	}
	if h == nil || h.Neo4j == nil {
		return h.searchEntityContentWithExact(ctx, repoID, query, language, limit, exact)
	}
	access := codeGrantAccessFilter(ctx)
	if access.Empty() || (repoID != "" && !access.AllowsRepositoryID(repoID)) {
		return []map[string]any{}, nil
	}

	cypher, params := buildSearchGraphEntitiesQuery(repoID, query, language, limit, exact, access)

	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"entity_id":  StringVal(row, "entity_id"),
			"name":       StringVal(row, "name"),
			"labels":     StringSliceVal(row, "labels"),
			"file_path":  StringVal(row, "file_path"),
			"repo_id":    StringVal(row, "repo_id"),
			"repo_name":  StringVal(row, "repo_name"),
			"language":   StringVal(row, "language"),
			"start_line": IntVal(row, "start_line"),
			"end_line":   IntVal(row, "end_line"),
		}
		if metadata := querycontract.GraphResultMetadata(row); len(metadata) > 0 {
			result["metadata"] = metadata
			entitysemantics.AttachSemanticSummary(result)
		}
		results = append(results, result)
	}

	return h.enrichGraphSearchResultsWithContentMetadata(ctx, results, repoID, query, limit)
}

func (h *CodeHandler) searchEntityContentWithExact(ctx context.Context, repoID, pattern, language string, limit int, exact bool) ([]map[string]any, error) {
	var (
		nameMatches   []EntityContent
		sourceMatches []EntityContent
		err           error
	)
	access := codeGrantAccessFilter(ctx)
	if access.Empty() || (repoID != "" && !access.AllowsRepositoryID(repoID)) {
		return []map[string]any{}, nil
	}
	if repoID != "" {
		nameMatches, err = h.Content.SearchEntitiesByName(ctx, repoID, "", pattern, limit)
		if err != nil {
			return nil, err
		}
		sourceMatches, err = h.Content.SearchEntityContent(ctx, repoID, pattern, limit)
		if err != nil {
			return nil, err
		}
	} else if access.Scoped() {
		for _, allowedRepoID := range access.RepositorySearchIDs() {
			if len(nameMatches) < limit {
				rows, searchErr := h.Content.SearchEntitiesByName(ctx, allowedRepoID, "", pattern, limit-len(nameMatches))
				if searchErr != nil {
					return nil, searchErr
				}
				nameMatches = append(nameMatches, rows...)
			}
			if len(sourceMatches) < limit {
				rows, searchErr := h.Content.SearchEntityContent(ctx, allowedRepoID, pattern, limit-len(sourceMatches))
				if searchErr != nil {
					return nil, searchErr
				}
				sourceMatches = append(sourceMatches, rows...)
			}
			if len(nameMatches) >= limit && len(sourceMatches) >= limit {
				break
			}
		}
	} else {
		nameMatches, err = h.Content.SearchEntitiesByNameAnyRepo(ctx, "", pattern, limit)
		if err != nil {
			return nil, err
		}
		sourceMatches, err = h.Content.SearchEntityContentAnyRepo(ctx, pattern, limit)
		if err != nil {
			return nil, err
		}
	}

	allowedLanguages := make(map[string]struct{})
	if strings.TrimSpace(language) != "" {
		for _, variant := range querycontract.NormalizedLanguageVariants(language) {
			allowedLanguages[variant] = struct{}{}
		}
	}

	results := make([]map[string]any, 0, len(nameMatches)+len(sourceMatches))
	seen := make(map[string]struct{}, len(nameMatches)+len(sourceMatches))
	appendResult := func(entity EntityContent) {
		if entity.EntityID == "" {
			return
		}
		if len(allowedLanguages) > 0 {
			if _, ok := allowedLanguages[entity.Language]; !ok {
				return
			}
		}
		if _, ok := seen[entity.EntityID]; ok {
			return
		}
		seen[entity.EntityID] = struct{}{}
		results = append(results, map[string]any{
			"entity_id":    entity.EntityID,
			"entity_name":  entity.EntityName,
			"entity_type":  entity.EntityType,
			"file_path":    entity.RelativePath,
			"start_line":   entity.StartLine,
			"end_line":     entity.EndLine,
			"language":     entity.Language,
			"source_cache": entity.SourceCache,
			"metadata":     entity.Metadata,
			"repo_id":      entity.RepoID,
		})
		entitysemantics.AttachSemanticSummary(results[len(results)-1])
	}

	for _, entity := range nameMatches {
		if exact && entity.EntityName != pattern {
			continue
		}
		appendResult(entity)
	}
	for _, entity := range sourceMatches {
		if exact && entity.EntityName != pattern {
			continue
		}
		appendResult(entity)
	}

	return results, nil
}

func (h *CodeHandler) runComplexityQuery(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return h.Neo4j.RunSingle(ctx, cypher, params)
}
