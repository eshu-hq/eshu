package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

const (
	symbolSearchDefaultLimit = 25
	symbolSearchMaxLimit     = 200
	symbolSearchMaxOffset    = 10000
)

var (
	errSymbolBackendUnavailable = errors.New("symbol lookup backend is unavailable")
	errSymbolOffsetUnsupported  = errors.New("symbol lookup offset pagination requires content-index search")
)

type symbolSearchRequest struct {
	Symbol      string   `json:"symbol"`
	Query       string   `json:"query"`
	RepoID      string   `json:"repo_id"`
	Language    string   `json:"language"`
	EntityType  string   `json:"entity_type"`
	EntityTypes []string `json:"entity_types"`
	MatchMode   string   `json:"match_mode"`
	Limit       int      `json:"limit"`
	Offset      int      `json:"offset"`
}

type symbolContentSearcher interface {
	searchSymbols(context.Context, symbolSearchRequest) ([]EntityContent, error)
}

func (h *CodeHandler) handleSymbolSearch(w http.ResponseWriter, r *http.Request) {
	const capability = "code_search.symbol_lookup"

	var req symbolSearchRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), capability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"symbol lookup requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			capability,
			h.profile(),
			requiredProfile(capability),
		)
		return
	}
	if strings.TrimSpace(req.symbol()) == "" {
		WriteError(w, http.StatusBadRequest, "symbol is required")
		return
	}
	if req.Offset < 0 {
		WriteError(w, http.StatusBadRequest, "offset must be >= 0")
		return
	}
	if req.Offset > symbolSearchMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset must be <= 10000")
		return
	}
	if _, err := req.normalizedMatchMode(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	results, sourceBackend, truthBasis, err := h.symbolSearchResults(r.Context(), req)
	if err != nil {
		if errors.Is(err, errSymbolOffsetUnsupported) {
			WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, errSymbolBackendUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	limit := req.normalizedLimit()
	truncated := len(results) > limit
	if truncated {
		results = results[:limit]
	}
	data := map[string]any{
		"symbol":         req.symbol(),
		"query":          req.symbol(),
		"match_mode":     req.mustMatchMode(),
		"repo_id":        req.RepoID,
		"language":       strings.TrimSpace(req.Language),
		"entity_types":   req.normalizedEntityTypes(),
		"limit":          limit,
		"offset":         req.Offset,
		"results":        results,
		"matches":        results,
		"count":          len(results),
		"truncated":      truncated,
		"source_backend": sourceBackend,
		"ambiguity": map[string]any{
			"ambiguous": truncated || len(results) > 1,
			"reason":    symbolAmbiguityReason(truncated, len(results)),
		},
	}

	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), capability, truthBasis, "resolved from bounded symbol definition lookup"),
	)
}

func (h *CodeHandler) symbolSearchResults(
	ctx context.Context,
	req symbolSearchRequest,
) ([]map[string]any, string, TruthBasis, error) {
	matchMode := req.mustMatchMode()
	probeReq := req
	probeReq.Limit = req.normalizedLimit() + 1

	if h != nil && h.Content != nil {
		if searcher, ok := h.Content.(symbolContentSearcher); ok {
			entities, err := searcher.searchSymbols(ctx, probeReq)
			if err != nil {
				return nil, "", "", fmt.Errorf("search symbols: %w", err)
			}
			return symbolEntityResults(entities, matchMode), "postgres_content_store", TruthBasisContentIndex, nil
		}
		if req.Offset == 0 {
			entities, err := h.Content.SearchEntitiesByName(ctx, req.RepoID, firstEntityType(req), req.symbol(), probeReq.Limit)
			if err != nil {
				return nil, "", "", fmt.Errorf("search symbols: %w", err)
			}
			return symbolEntityResults(entities, matchMode), "postgres_content_store", TruthBasisContentIndex, nil
		}
	}

	if h == nil || h.Neo4j == nil {
		return nil, "", "", errSymbolBackendUnavailable
	}
	if req.Offset > 0 {
		return nil, "", "", errSymbolOffsetUnsupported
	}
	graphResults, err := h.searchGraphEntitiesWithExact(
		ctx,
		req.RepoID,
		req.symbol(),
		req.Language,
		probeReq.Limit,
		matchMode == "exact",
	)
	if err != nil {
		return nil, "", "", err
	}
	return normalizeSymbolGraphResults(graphResults, matchMode), "graph", TruthBasisAuthoritativeGraph, nil
}

func (r symbolSearchRequest) symbol() string {
	if symbol := strings.TrimSpace(r.Symbol); symbol != "" {
		return symbol
	}
	return strings.TrimSpace(r.Query)
}

func (r symbolSearchRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return symbolSearchDefaultLimit
	case r.Limit > symbolSearchMaxLimit:
		return symbolSearchMaxLimit
	default:
		return r.Limit
	}
}

func (r symbolSearchRequest) normalizedMatchMode() (string, error) {
	matchMode := strings.ToLower(strings.TrimSpace(r.MatchMode))
	if matchMode == "" {
		return "exact", nil
	}
	switch matchMode {
	case "exact", "fuzzy":
		return matchMode, nil
	default:
		return "", fmt.Errorf("match_mode must be exact or fuzzy")
	}
}

func (r symbolSearchRequest) mustMatchMode() string {
	matchMode, _ := r.normalizedMatchMode()
	if matchMode == "" {
		return "exact"
	}
	return matchMode
}

func (r symbolSearchRequest) normalizedEntityTypes() []string {
	values := make([]string, 0, len(r.EntityTypes)+1)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		value = contentEntityTypeForResolve(value)
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	add(r.EntityType)
	for _, entityType := range r.EntityTypes {
		add(entityType)
	}
	return values
}

func firstEntityType(req symbolSearchRequest) string {
	entityTypes := req.normalizedEntityTypes()
	if len(entityTypes) == 0 {
		return ""
	}
	return entityTypes[0]
}

func symbolEntityResults(entities []EntityContent, matchMode string) []map[string]any {
	results := make([]map[string]any, 0, len(entities))
	for index, entity := range entities {
		result := map[string]any{
			"entity_id":       entity.EntityID,
			"name":            entity.EntityName,
			"entity_name":     entity.EntityName,
			"entity_type":     entity.EntityType,
			"file_path":       entity.RelativePath,
			"relative_path":   entity.RelativePath,
			"repo_id":         entity.RepoID,
			"language":        entity.Language,
			"start_line":      entity.StartLine,
			"end_line":        entity.EndLine,
			"source_cache":    entity.SourceCache,
			"metadata":        entity.Metadata,
			"classification":  "definition",
			"match_kind":      matchMode,
			"rank":            index + 1,
			"source_backend":  "postgres_content_store",
			"source_handle":   symbolSourceHandle(entity.RepoID, entity.RelativePath, entity.StartLine, entity.EndLine),
			"definition_kind": entity.EntityType,
		}
		attachSemanticSummary(result)
		results = append(results, result)
	}
	return results
}

func normalizeSymbolGraphResults(rows []map[string]any, matchMode string) []map[string]any {
	results := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		result := cloneQueryAnyMap(row)
		if result["name"] == nil && result["entity_name"] != nil {
			result["name"] = result["entity_name"]
		}
		if result["entity_name"] == nil && result["name"] != nil {
			result["entity_name"] = result["name"]
		}
		filePath := StringVal(result, "file_path")
		result["classification"] = "definition"
		result["match_kind"] = matchMode
		result["rank"] = index + 1
		result["source_backend"] = "graph"
		result["source_handle"] = symbolSourceHandle(StringVal(result, "repo_id"), filePath, IntVal(result, "start_line"), IntVal(result, "end_line"))
		results = append(results, result)
	}
	return results
}

func symbolSourceHandle(repoID, path string, startLine, endLine int) map[string]any {
	return map[string]any{
		"repo_id":    repoID,
		"file_path":  path,
		"start_line": startLine,
		"end_line":   endLine,
	}
}

func symbolAmbiguityReason(truncated bool, count int) string {
	if truncated {
		return "more results are available; use offset to page"
	}
	if count > 1 {
		return "multiple definitions matched the symbol"
	}
	return ""
}
