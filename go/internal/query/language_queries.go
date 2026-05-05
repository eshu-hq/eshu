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
func buildLanguageCypher(language, label, query, repoID string, limit int) (string, map[string]any) {
	return buildLanguageCypherWithSemanticFilter(language, label, query, repoID, limit, "", "")
}

func buildLanguageCypherWithSemanticFilter(
	language,
	label,
	query,
	repoID string,
	limit int,
	semanticFilterKey string,
	semanticFilterValue string,
) (string, map[string]any) {
	language = canonicalLanguage(language)
	params := map[string]any{
		"language": language,
		"limit":    limit,
	}

	// Build the extension filter for this language.
	exts := languageFileExtensions[language]
	extFilter := buildExtensionFilter(exts)

	switch label {
	case "Repository":
		return buildRepositoryCypher(language, query, repoID, limit)
	case "Directory":
		return buildDirectoryCypher(language, extFilter, query, repoID, params)
	case "File":
		return buildFileCypher(language, extFilter, query, repoID, params)
	default:
		return buildEntityCypherWithSemanticFilter(
			language,
			label,
			extFilter,
			query,
			repoID,
			params,
			semanticFilterKey,
			semanticFilterValue,
		)
	}
}

// buildRepositoryCypher returns a query for repositories that contain files
// in the given language.
func buildRepositoryCypher(language, query, repoID string, limit int) (string, map[string]any) {
	params := map[string]any{
		"language": language,
		"limit":    limit,
	}

	cypher := `
		MATCH (r:Repository)-[:REPO_CONTAINS]->(f:File)
		WHERE (f.language = $language OR f.language = $language_title)
	`
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	if query != "" {
		cypher += " AND r.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		WITH r, count(f) as file_count
		RETURN r.id as id, r.name as name,
		       coalesce(r.local_path, r.path) as local_path,
		       r.remote_url as remote_url,
		       file_count
		ORDER BY file_count DESC
		LIMIT $limit
	`
	return cypher, params
}

// buildDirectoryCypher returns a query for directories containing files in the
// given language.
func buildDirectoryCypher(language, extFilter, query, repoID string, params map[string]any) (string, map[string]any) {
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	cypher := `
		MATCH (d:Directory)<-[:REPO_CONTAINS|CONTAINS*]-(r:Repository)
		MATCH (d)-[:CONTAINS]->(f:File)
		WHERE (f.language = $language OR f.language = $language_title` + extFilter + `)
	`

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	if query != "" {
		cypher += " AND d.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		WITH d, r, count(f) as file_count
		RETURN d.id as entity_id, d.name as name, labels(d) as labels,
		       d.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       file_count
		ORDER BY file_count DESC
		LIMIT $limit
	`
	return cypher, params
}

// buildFileCypher returns a query for files in the given language.
func buildFileCypher(language, extFilter, query, repoID string, params map[string]any) (string, map[string]any) {
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	cypher := `
		MATCH (f:File)<-[:REPO_CONTAINS]-(r:Repository)
		WHERE (f.language = $language OR f.language = $language_title` + extFilter + `)
	`

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	if query != "" {
		cypher += " AND f.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		RETURN f.id as entity_id, f.name as name, labels(f) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       f.language as language
		ORDER BY f.relative_path
		LIMIT $limit
	`
	return cypher, params
}

func buildEntityCypherWithSemanticFilter(
	language, label, extFilter, query, repoID string,
	params map[string]any,
	semanticFilterKey string,
	semanticFilterValue string,
) (string, map[string]any) {
	params["language_title"] = strings.Title(language) //nolint:staticcheck

	cypher := fmt.Sprintf(`
		MATCH (e:%s)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		WHERE (e.language = $language OR e.language = $language_title
		       OR f.language = $language OR f.language = $language_title%s)
	`, label, extFilter)

	if semanticFilterKey != "" {
		cypher += fmt.Sprintf(" AND coalesce(e.%s, '') = $semantic_filter", semanticFilterKey)
		params["semantic_filter"] = semanticFilterValue
	}

	if repoID != "" {
		cypher += " AND r.id = $repo_id"
		params["repo_id"] = repoID
	}
	if query != "" {
		cypher += " AND e.name CONTAINS $query"
		params["query"] = query
	}

	cypher += `
		RETURN e.id as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line, e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY f.relative_path, e.name
		LIMIT $limit
	`
	return cypher, params
}

// buildExtensionFilter returns a Cypher OR clause fragment that matches common
// file extensions for a language. Returns an empty string when no extensions
// are registered.
func buildExtensionFilter(exts []string) string {
	if len(exts) == 0 {
		return ""
	}
	clauses := make([]string, 0, len(exts))
	for _, ext := range exts {
		clauses = append(clauses, fmt.Sprintf("f.name ENDS WITH '%s'", ext))
	}
	return " OR " + strings.Join(clauses, " OR ")
}

// buildLanguageResult converts a Neo4j result row into the response shape.
// joinKeys returns a sorted comma-separated list of map keys.
func joinKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Sort for deterministic output.
	sortStrings(keys)
	return strings.Join(keys, ", ")
}

// sortStrings sorts a string slice in place (insertion sort for small slices).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// SupportedLanguages returns the set of language names with query support.
func SupportedLanguages() []string {
	return mapKeys(supportedLanguages)
}

// SupportedEntityTypes returns the set of entity type names with query support.
func SupportedEntityTypes() []string {
	return mapKeys(allSupportedEntityTypes())
}

// mapKeys returns sorted keys from a map.
func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}
