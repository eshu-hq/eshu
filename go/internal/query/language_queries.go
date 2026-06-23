package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// LanguageQueryHandler provides language-specific entity queries against the
// graph and content store. Graph-backed entity types use Neo4j. Content-only
// entity types use the Postgres content store.
type LanguageQueryHandler struct {
	Neo4j   GraphQuery
	Content ContentStore
	Profile QueryProfile
}

// Mount registers the language query endpoint on the given mux.
func (h *LanguageQueryHandler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/code/language-query", h.handleLanguageQuery)
}

// handleLanguageQuery dispatches a language-specific entity query.
func (h *LanguageQueryHandler) handleLanguageQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Language   string `json:"language"`
		EntityType string `json:"entity_type"`
		Query      string `json:"query"`
		RepoID     string `json:"repo_id"`
		Limit      int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Language == "" {
		WriteError(w, http.StatusBadRequest, "language is required")
		return
	}
	if req.EntityType == "" {
		WriteError(w, http.StatusBadRequest, "entity_type is required")
		return
	}

	req.Language = canonicalLanguage(req.Language)
	req.EntityType = strings.ToLower(strings.TrimSpace(req.EntityType))

	if !supportedLanguages[req.Language] {
		WriteError(w, http.StatusBadRequest, fmt.Sprintf(
			"unsupported language %q; supported: %s",
			req.Language, joinKeys(supportedLanguages),
		))
		return
	}

	if req.Limit <= 0 {
		req.Limit = 50
	}

	if req.EntityType == "guard" {
		results, err := h.queryGraphFirstContentByLanguageWithSemanticFilter(
			r.Context(),
			req.Language,
			"Function",
			req.Query,
			req.RepoID,
			req.Limit,
			"semantic_kind",
			"guard",
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"language":    req.Language,
			"entity_type": req.EntityType,
			"query":       req.Query,
			"results":     results,
		})
		return
	}

	if label, ok := graphBackedEntityTypes[req.EntityType]; ok {
		results, err := h.queryByLanguage(r.Context(), req.Language, label, req.Query, req.RepoID, req.Limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"language":    req.Language,
			"entity_type": req.EntityType,
			"query":       req.Query,
			"results":     results,
		})
		return
	}

	if label, ok := graphFirstContentBackedEntityTypes[req.EntityType]; ok {
		results, err := h.queryGraphFirstContentByLanguage(
			r.Context(),
			req.Language,
			label,
			req.Query,
			req.RepoID,
			req.Limit,
		)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"language":    req.Language,
			"entity_type": req.EntityType,
			"query":       req.Query,
			"results":     results,
		})
		return
	}

	if label, ok := contentBackedEntityTypes[req.EntityType]; ok {
		results, err := h.queryContentByLanguage(r.Context(), req.Language, label, req.Query, req.RepoID, req.Limit)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}

		WriteJSON(w, http.StatusOK, map[string]any{
			"language":    req.Language,
			"entity_type": req.EntityType,
			"query":       req.Query,
			"results":     results,
		})
		return
	}

	WriteError(w, http.StatusBadRequest, fmt.Sprintf(
		"unsupported entity_type %q; supported: %s",
		req.EntityType, joinKeys(allSupportedEntityTypes()),
	))
}

func (h *LanguageQueryHandler) queryContentByLanguage(
	ctx context.Context,
	language, entityType, query, repoID string,
	limit int,
) ([]map[string]any, error) {
	if h.Content == nil {
		return nil, fmt.Errorf("content reader is required for %s queries", entityType)
	}

	rows, err := h.Content.SearchEntitiesByLanguageAndType(ctx, repoID, language, entityType, query, limit)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"entity_id":  row.EntityID,
			"name":       row.EntityName,
			"labels":     []string{row.EntityType},
			"file_path":  row.RelativePath,
			"repo_id":    row.RepoID,
			"language":   row.Language,
			"start_line": row.StartLine,
			"end_line":   row.EndLine,
			"metadata":   row.Metadata,
		}
		attachSemanticSummary(result)
		results = append(results, result)
	}

	return results, nil
}

func allSupportedEntityTypes() map[string]string {
	merged := make(map[string]string, len(graphBackedEntityTypes)+len(graphFirstContentBackedEntityTypes)+len(contentBackedEntityTypes))
	for key, value := range graphBackedEntityTypes {
		merged[key] = value
	}
	for key, value := range graphFirstContentBackedEntityTypes {
		merged[key] = value
	}
	for key, value := range contentBackedEntityTypes {
		merged[key] = value
	}
	return merged
}

// queryByLanguage builds and executes a language-specific Cypher query.
func (h *LanguageQueryHandler) queryByLanguage(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
) ([]map[string]any, error) {
	return h.queryByLanguageWithSemanticFilter(ctx, language, label, query, repoID, limit, "", "")
}

func (h *LanguageQueryHandler) queryByLanguageWithSemanticFilter(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		contentLabel := graphLabelToContentEntityType(label)
		if h == nil || contentLabel == "" {
			return nil, fmt.Errorf("neo4j reader is required for %s queries", label)
		}
		return h.queryContentByLanguage(ctx, language, contentLabel, query, repoID, limit)
	}

	cypher, params := buildLanguageCypherWithSemanticFilter(
		language,
		label,
		query,
		repoID,
		limit,
		semanticFilterKey,
		semanticFilterValue,
	)

	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}

	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		results = append(results, buildLanguageResult(row, label))
	}
	return h.enrichLanguageResultsWithContentMetadata(
		ctx,
		results,
		language,
		label,
		query,
		repoID,
		limit,
	)
}

func (h *LanguageQueryHandler) queryGraphFirstContentByLanguage(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
) ([]map[string]any, error) {
	return h.queryGraphFirstContentByLanguageWithSemanticFilter(ctx, language, label, query, repoID, limit, "", "")
}

func (h *LanguageQueryHandler) queryGraphFirstContentByLanguageWithSemanticFilter(
	ctx context.Context,
	language, label, query, repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
) ([]map[string]any, error) {
	if h.Neo4j != nil {
		results, err := h.queryByLanguageWithSemanticFilter(
			ctx,
			language,
			label,
			query,
			repoID,
			limit,
			semanticFilterKey,
			semanticFilterValue,
		)
		if err != nil {
			return nil, err
		}
		if len(results) > 0 {
			return results, nil
		}
	}
	return h.queryContentByLanguage(ctx, language, label, query, repoID, limit)
}

// buildLanguageCypher constructs the Cypher query and parameters for a
// language-specific entity lookup.
