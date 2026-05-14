package query

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// cypherQueryTimeout caps how long a user-submitted Cypher query can run.
	cypherQueryTimeout = 30 * time.Second

	// cypherMaxQueryLength rejects excessively long query strings.
	cypherMaxQueryLength = 4096

	// cypherDefaultResultRows is the default returned row window.
	cypherDefaultResultRows = 100

	// cypherMaxResultRows caps the number of rows returned to prevent memory exhaustion.
	cypherMaxResultRows = 1000
)

// cypherMutationKeywords are keywords that indicate a write or destructive
// Cypher operation. We reject any query containing these regardless of
// position so that even obfuscated or commented-out mutations are blocked.
var cypherMutationKeywords = []string{
	"CREATE", "MERGE", "DELETE", "DETACH", "SET ", "REMOVE",
	"DROP", "CALL ", "FOREACH", "LOAD CSV",
}

// validateReadOnlyCypher returns an error if the query appears to contain
// write or administrative operations. The Neo4j driver session is also
// opened with AccessModeRead as a second line of defense, but we reject
// obvious mutations before they reach the driver.
func validateReadOnlyCypher(cypher string) error {
	if len(cypher) > cypherMaxQueryLength {
		return fmt.Errorf("query exceeds maximum length of %d characters", cypherMaxQueryLength)
	}

	upper := strings.ToUpper(cypher)

	for _, kw := range cypherMutationKeywords {
		if strings.Contains(upper, kw) {
			return fmt.Errorf("query contains disallowed keyword %q; only read-only queries are permitted", strings.TrimSpace(kw))
		}
	}

	return nil
}

// handleCypherQuery executes a user-submitted read-only Cypher query.
//
// Safety measures:
//   - Keyword validation rejects mutation keywords before the query reaches Neo4j
//   - Neo4jReader uses AccessModeRead sessions (driver-enforced read-only)
//   - Context timeout prevents runaway queries from holding resources
//   - Result rows are capped to prevent memory exhaustion
//   - Query length is bounded to reject payload abuse
func (h *CodeHandler) handleCypherQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CypherQuery string `json:"cypher_query"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.CypherQuery == "" {
		WriteError(w, http.StatusBadRequest, "cypher_query is required")
		return
	}

	cypher, limit, err := boundedReadOnlyCypher(req.CypherQuery, req.Limit)
	if err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, nil)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"results":   rows,
		"limit":     limit,
		"truncated": truncated,
	}, BuildTruthEnvelope(h.profile(), "graph_query.read_only_cypher", TruthBasisAuthoritativeGraph, "resolved from bounded read-only graph query"))
}

func boundedReadOnlyCypher(query string, requestedLimit int) (string, int, error) {
	if err := validateReadOnlyCypher(query); err != nil {
		return "", 0, err
	}
	limit := normalizeCypherResultLimit(requestedLimit)
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")
	if containsLimitClause(query) {
		queryLimit, err := explicitCypherLimit(query)
		if err != nil {
			return "", 0, err
		}
		if queryLimit > limit {
			return "", 0, fmt.Errorf("query LIMIT %d exceeds requested limit %d", queryLimit, limit)
		}
		return query, limit, nil
	}
	return fmt.Sprintf("%s\nLIMIT %d", query, limit+1), limit, nil
}

func normalizeCypherResultLimit(limit int) int {
	if limit <= 0 {
		return cypherDefaultResultRows
	}
	if limit > cypherMaxResultRows {
		return cypherMaxResultRows
	}
	return limit
}

func containsLimitClause(query string) bool {
	upper := strings.ToUpper(query)
	return strings.Contains(" "+upper+" ", " LIMIT ")
}

func explicitCypherLimit(query string) (int, error) {
	fields := strings.Fields(query)
	for i, field := range fields {
		if !strings.EqualFold(field, "LIMIT") {
			continue
		}
		if i+1 >= len(fields) {
			return 0, fmt.Errorf("query LIMIT must include an integer row cap")
		}
		raw := strings.TrimRight(fields[i+1], ";")
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return 0, fmt.Errorf("query LIMIT must be a positive integer")
		}
		return limit, nil
	}
	return 0, fmt.Errorf("query LIMIT clause not found")
}

// handleVisualizeQuery returns a Neo4j Browser URL for the given Cypher query.
func (h *CodeHandler) handleVisualizeQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CypherQuery string `json:"cypher_query"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.CypherQuery == "" {
		WriteError(w, http.StatusBadRequest, "cypher_query is required")
		return
	}

	if err := validateReadOnlyCypher(req.CypherQuery); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	browserURL := fmt.Sprintf(
		"http://localhost:7474/browser/?cmd=edit&arg=%s",
		url.QueryEscape(req.CypherQuery),
	)

	WriteJSON(w, http.StatusOK, map[string]any{"url": browserURL})
}

// handleSearchBundles searches indexed repositories as pre-indexed bundles.
func (h *CodeHandler) handleSearchBundles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string `json:"query"`
		UniqueOnly bool   `json:"unique_only"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	cypher := `MATCH (r:Repository) WHERE r.name IS NOT NULL`
	params := map[string]any{}

	if req.Query != "" {
		cypher += ` AND toLower(r.name) CONTAINS toLower($query)`
		params["query"] = req.Query
	}

	if req.UniqueOnly {
		cypher += ` RETURN DISTINCT r.name AS name, r.repo_id AS repo_id ORDER BY r.name LIMIT 100`
	} else {
		cypher += ` RETURN r.name AS name, r.repo_id AS repo_id ORDER BY r.name LIMIT 100`
	}

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{"bundles": rows})
}

func (h *CodeHandler) lookupComplexityRowByName(ctx context.Context, functionName, repoID string) (map[string]any, error) {
	params := map[string]any{"entity_name": functionName}
	cypher := `
		MATCH (e)
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE e.name = $entity_name
	`
	if repoID != "" {
		cypher += " AND repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += `
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
` + graphSemanticMetadataProjection() + `
		LIMIT 1
	`
	return h.runComplexityQuery(ctx, cypher, params)
}

func (h *CodeHandler) listMostComplexFunctions(ctx context.Context, repoID string, limit int) ([]map[string]any, error) {
	if limit <= 0 {
		limit = 10
	}
	cypher := `
		MATCH (e:Function)
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE coalesce(e.cyclomatic_complexity, 0) > 0
	`
	params := map[string]any{"limit": limit}
	if repoID != "" {
		cypher += " AND repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += `
		RETURN e.id as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `,
		       coalesce(e.cyclomatic_complexity, 0) as complexity
		ORDER BY complexity DESC, e.name
		LIMIT $limit
	`
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"entity_id":  StringVal(row, "id"),
			"name":       StringVal(row, "name"),
			"labels":     StringSliceVal(row, "labels"),
			"file_path":  StringVal(row, "file_path"),
			"repo_id":    StringVal(row, "repo_id"),
			"repo_name":  StringVal(row, "repo_name"),
			"language":   StringVal(row, "language"),
			"start_line": IntVal(row, "start_line"),
			"end_line":   IntVal(row, "end_line"),
			"complexity": IntVal(row, "complexity"),
		}
		if metadata := graphResultMetadata(row); len(metadata) > 0 {
			result["metadata"] = metadata
			attachSemanticSummary(result)
		}
		results = append(results, result)
	}
	return results, nil
}
