package query

import (
	"context"
	"net/http"
	"strings"
)

// CodeHandler provides HTTP routes for code-level queries: search, relationships,
// dead code detection, and complexity metrics.
type CodeHandler struct {
	GraphBackend GraphBackend
	Neo4j        GraphQuery
	Content      ContentStore
	Profile      QueryProfile
}

// Mount registers all /api/v0/code/* routes on the given mux.
func (h *CodeHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/code/search", h.handleSearch)
	mux.HandleFunc("POST /api/v0/code/symbols/search", h.handleSymbolSearch)
	mux.HandleFunc("POST /api/v0/code/structure/inventory", h.handleStructuralInventory)
	mux.HandleFunc("POST /api/v0/code/topics/investigate", h.handleTopicInvestigation)
	mux.HandleFunc("POST /api/v0/code/security/secrets/investigate", h.handleHardcodedSecretInvestigation)
	mux.HandleFunc("POST /api/v0/code/imports/investigate", h.handleImportDependencyInvestigation)
	mux.HandleFunc("POST /api/v0/code/relationships", h.handleRelationships)
	mux.HandleFunc("POST /api/v0/code/relationships/story", h.handleRelationshipStory)
	mux.HandleFunc("POST /api/v0/code/dead-code", h.handleDeadCode)
	mux.HandleFunc("POST /api/v0/code/dead-code/investigate", h.handleDeadCodeInvestigation)
	mux.HandleFunc("POST /api/v0/code/complexity", h.handleComplexity)
	mux.HandleFunc("POST /api/v0/code/quality/inspect", h.handleCodeQualityInspection)
	mux.HandleFunc("POST /api/v0/code/call-chain", h.handleCallChain)

	// Read-only Cypher, visualization, and bundle search.
	mux.HandleFunc("POST /api/v0/code/cypher", h.handleCypherQuery)
	mux.HandleFunc("POST /api/v0/code/visualize", h.handleVisualizeQuery)
	mux.HandleFunc("POST /api/v0/code/bundles", h.handleSearchBundles)

	// Language-specific queries.
	lq := &LanguageQueryHandler{Neo4j: h.Neo4j, Content: h.Content, Profile: h.profile()}
	lq.Mount(mux)
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

	if req.Query == "" {
		WriteError(w, http.StatusBadRequest, "query is required")
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}

	ctx := r.Context()
	capability := "code_search.fuzzy_symbol"
	if strings.EqualFold(strings.TrimSpace(req.SearchType), "variable") {
		capability = "code_search.variable_lookup"
	} else if req.Exact {
		capability = "code_search.exact_symbol"
	}

	// Search graph entities by name pattern
	graphResults, err := h.searchGraphEntitiesWithExact(ctx, req.RepoID, req.Query, req.Language, req.Limit, req.Exact)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// If graph search returns results, return them
	if len(graphResults) > 0 {
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"source":         "graph",
			"source_backend": "graph",
			"query":          req.Query,
			"repo_id":        req.RepoID,
			"results":        graphResults,
			"matches":        graphResults,
		}, BuildTruthEnvelope(h.profile(), capability, TruthBasisAuthoritativeGraph, "resolved from graph-backed entity search"))
		return
	}

	// Fall back to content-based search if no graph results
	contentResults, err := h.searchEntityContentWithExact(ctx, req.RepoID, req.Query, req.Language, req.Limit, req.Exact)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"source":         "content",
		"source_backend": "postgres_content_store",
		"query":          req.Query,
		"repo_id":        req.RepoID,
		"results":        contentResults,
		"matches":        contentResults,
	}, BuildTruthEnvelope(h.profile(), capability, TruthBasisContentIndex, "resolved from content index fallback"))
}

// searchGraphEntities finds entities by name pattern in the Neo4j graph.
func (h *CodeHandler) searchGraphEntities(ctx context.Context, repoID, query, language string, limit int) ([]map[string]any, error) {
	return h.searchGraphEntitiesWithExact(ctx, repoID, query, language, limit, false)
}

func (h *CodeHandler) searchGraphEntitiesWithExact(ctx context.Context, repoID, query, language string, limit int, exact bool) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return h.searchEntityContentWithExact(ctx, repoID, query, language, limit, exact)
	}

	cypher := `
		MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
	`
	params := map[string]any{
		"query": query,
		"limit": limit,
	}
	if exact {
		cypher += " WHERE e.name = $query"
	} else {
		cypher += " WHERE e.name CONTAINS $query"
	}
	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}

	if language != "" {
		cypher += " AND (e.language = $language OR f.language = $language)"
		params["language"] = language
	}

	cypher += `
		RETURN e.id as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY e.name
		LIMIT $limit
	`

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
		if metadata := graphResultMetadata(row); len(metadata) > 0 {
			result["metadata"] = metadata
			attachSemanticSummary(result)
		}
		results = append(results, result)
	}

	return h.enrichGraphSearchResultsWithContentMetadata(ctx, results, repoID, query, limit)
}

// searchEntityContent searches entity source code in the content store.
func (h *CodeHandler) searchEntityContent(ctx context.Context, repoID, pattern, language string, limit int) ([]map[string]any, error) {
	return h.searchEntityContentWithExact(ctx, repoID, pattern, language, limit, false)
}

func (h *CodeHandler) searchEntityContentWithExact(ctx context.Context, repoID, pattern, language string, limit int, exact bool) ([]map[string]any, error) {
	var (
		nameMatches   []EntityContent
		sourceMatches []EntityContent
		err           error
	)
	if repoID != "" {
		nameMatches, err = h.Content.SearchEntitiesByName(ctx, repoID, "", pattern, limit)
		if err != nil {
			return nil, err
		}
		sourceMatches, err = h.Content.SearchEntityContent(ctx, repoID, pattern, limit)
		if err != nil {
			return nil, err
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
		for _, variant := range normalizedLanguageVariants(language) {
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
		attachSemanticSummary(results[len(results)-1])
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

// handleComplexity returns relationship-based complexity metrics for an entity.
func (h *CodeHandler) handleComplexity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RepoID       string `json:"repo_id"`
		EntityID     string `json:"entity_id"`
		FunctionName string `json:"function_name"`
		Limit        int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}
	if req.EntityID == "" && req.FunctionName == "" {
		results, limit, truncated, err := h.listMostComplexFunctions(ctx, req.RepoID, req.Limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		WriteSuccess(w, r, http.StatusOK, map[string]any{
			"repo_id":    req.RepoID,
			"results":    results,
			"limit":      limit,
			"truncated":  truncated,
			"result_key": "entity_id",
		}, BuildTruthEnvelope(h.profile(), "code_quality.complexity", TruthBasisHybrid, "resolved from graph-derived complexity metrics"))
		return
	}

	row, err := h.lookupComplexityRow(ctx, req.EntityID, req.FunctionName, req.RepoID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		WriteError(w, http.StatusNotFound, "entity not found")
		return
	}

	response := map[string]any{
		"entity_id":           StringVal(row, "id"),
		"name":                StringVal(row, "name"),
		"labels":              StringSliceVal(row, "labels"),
		"file_path":           StringVal(row, "file_path"),
		"repo_id":             StringVal(row, "repo_id"),
		"repo_name":           StringVal(row, "repo_name"),
		"language":            StringVal(row, "language"),
		"start_line":          IntVal(row, "start_line"),
		"end_line":            IntVal(row, "end_line"),
		"complexity":          IntVal(row, "complexity"),
		"outgoing_count":      IntVal(row, "outgoing_count"),
		"incoming_count":      IntVal(row, "incoming_count"),
		"total_relationships": IntVal(row, "total_relationships"),
	}
	if metadata := graphResultMetadata(row); len(metadata) > 0 {
		response["metadata"] = metadata
	}
	enriched, err := h.enrichGraphSearchResultsWithContentMetadata(
		ctx,
		[]map[string]any{response},
		StringVal(row, "repo_id"),
		StringVal(row, "name"),
		1,
	)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteSuccess(w, r, http.StatusOK, enriched[0], BuildTruthEnvelope(h.profile(), "code_quality.complexity", TruthBasisHybrid, "resolved from graph-derived complexity metrics"))
}

func (h *CodeHandler) lookupComplexityRow(ctx context.Context, entityID, functionName, repoID string) (map[string]any, error) {
	if functionName != "" {
		return h.lookupComplexityRowByName(ctx, functionName, repoID)
	}
	row, err := h.runComplexityQuery(ctx, `
		MATCH (e) WHERE e.id = $entity_id
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		OPTIONAL MATCH (e)-[outgoingRel]->()
		OPTIONAL MATCH ()-[incomingRel]->(e)
		RETURN e.id as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
		       coalesce(e.cyclomatic_complexity, 0) as complexity,
		       count(DISTINCT outgoingRel) as outgoing_count,
		       count(DISTINCT incomingRel) as incoming_count,
		       count(DISTINCT outgoingRel) + count(DISTINCT incomingRel) as total_relationships
`+graphSemanticMetadataProjection()+`
	`, map[string]any{"entity_id": entityID})
	if row == nil {
		return h.lookupComplexityRowByName(ctx, entityID, repoID)
	}
	return row, err
}

func (h *CodeHandler) runComplexityQuery(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	return h.Neo4j.RunSingle(ctx, cypher, params)
}
