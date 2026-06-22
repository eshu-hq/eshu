package query

import (
	"context"
	"fmt"
	"net/http"
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
		writeCypherQueryError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	if req.CypherQuery == "" {
		writeCypherQueryError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "cypher_query is required")
		return
	}

	if capabilityUnsupported(h.profile(), readOnlyCypherCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"read-only Cypher queries require a graph-backed authoritative profile",
			ErrorCodeUnsupportedCapability,
			readOnlyCypherCapability,
			h.profile(),
			requiredProfile(readOnlyCypherCapability),
		)
		return
	}

	cypher, limit, err := boundedReadOnlyCypher(req.CypherQuery, req.Limit)
	if err != nil {
		writeCypherQueryError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, nil)
	if err != nil {
		writeCypherQueryError(w, r, http.StatusInternalServerError, ErrorCodeInternalError, err.Error())
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
	}, BuildTruthEnvelope(h.profile(), readOnlyCypherCapability, TruthBasisAuthoritativeGraph, "resolved from bounded read-only graph query"))
}

func writeCypherQueryError(w http.ResponseWriter, r *http.Request, status int, code ErrorCode, message string) {
	if acceptsEnvelope(r) {
		WriteJSON(w, status, ResponseEnvelope{
			Data: nil,
			Error: &ErrorEnvelope{
				Code:       code,
				Message:    message,
				Capability: readOnlyCypherCapability,
			},
		})
		return
	}
	WriteError(w, status, message)
}

func boundedReadOnlyCypher(query string, requestedLimit int) (string, int, error) {
	if err := validateReadOnlyCypher(query); err != nil {
		return "", 0, err
	}
	limit := normalizeCypherResultLimit(requestedLimit)
	query = strings.TrimSpace(query)
	query = strings.TrimSuffix(query, ";")
	queryLimit, hasLimit, err := explicitCypherLimit(query)
	if err != nil {
		return "", 0, err
	}
	if hasLimit {
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

func explicitCypherLimit(query string) (int, bool, error) {
	for i := 0; i < len(query); {
		switch {
		case isCypherSpace(query[i]):
			i++
		case startsCypherLineComment(query, i):
			i = skipCypherLineComment(query, i)
		case startsCypherBlockComment(query, i):
			i = skipCypherBlockComment(query, i)
		case query[i] == '\'' || query[i] == '"' || query[i] == '`':
			i = skipCypherQuoted(query, i)
		case isCypherIdentifierChar(query[i]):
			start := i
			for i < len(query) && isCypherIdentifierChar(query[i]) {
				i++
			}
			if !strings.EqualFold(query[start:i], "LIMIT") {
				continue
			}
			valueStart := skipCypherTrivia(query, i)
			valueEnd := valueStart
			for valueEnd < len(query) &&
				!isCypherSpace(query[valueEnd]) &&
				!startsCypherLineComment(query, valueEnd) &&
				!startsCypherBlockComment(query, valueEnd) {
				valueEnd++
			}
			if valueEnd == valueStart {
				return 0, true, fmt.Errorf("query LIMIT must include an integer row cap")
			}
			raw := strings.TrimRight(query[valueStart:valueEnd], ";")
			limit, err := strconv.Atoi(raw)
			if err != nil || limit <= 0 {
				return 0, true, fmt.Errorf("query LIMIT must be a positive integer")
			}
			return limit, true, nil
		default:
			i++
		}
	}
	return 0, false, nil
}

func skipCypherTrivia(query string, index int) int {
	for index < len(query) {
		switch {
		case isCypherSpace(query[index]):
			index++
		case startsCypherLineComment(query, index):
			index = skipCypherLineComment(query, index)
		case startsCypherBlockComment(query, index):
			index = skipCypherBlockComment(query, index)
		default:
			return index
		}
	}
	return index
}

func skipCypherQuoted(query string, index int) int {
	quote := query[index]
	index++
	for index < len(query) {
		if query[index] == '\\' && quote != '`' {
			index += 2
			continue
		}
		if query[index] == quote {
			if quote != '`' && index+1 < len(query) && query[index+1] == quote {
				index += 2
				continue
			}
			return index + 1
		}
		index++
	}
	return index
}

func startsCypherLineComment(query string, index int) bool {
	return index+1 < len(query) && query[index] == '/' && query[index+1] == '/'
}

func skipCypherLineComment(query string, index int) int {
	index += 2
	for index < len(query) && query[index] != '\n' && query[index] != '\r' {
		index++
	}
	return index
}

func startsCypherBlockComment(query string, index int) bool {
	return index+1 < len(query) && query[index] == '/' && query[index+1] == '*'
}

func skipCypherBlockComment(query string, index int) int {
	index += 2
	for index+1 < len(query) {
		if query[index] == '*' && query[index+1] == '/' {
			return index + 2
		}
		index++
	}
	return len(query)
}

func isCypherIdentifierChar(ch byte) bool {
	return ch == '_' ||
		(ch >= '0' && ch <= '9') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z')
}

func isCypherSpace(ch byte) bool {
	return ch == ' ' || ch == '\n' || ch == '\r' || ch == '\t' || ch == '\f'
}

// handleVisualizeQuery executes a caller-supplied read-only Cypher query and
// returns a bounded, renderable visualization packet (nodes and edges) projected
// from the graph entities in the result, instead of a hardcoded browser URL.
//
// It shares the read-only safety path of handleCypherQuery: mutation keywords
// are rejected, the query is bounded with an injected LIMIT, the read runs under
// a timeout against an AccessModeRead session, and the row window is capped. The
// projection is a pure transformation of the returned rows; only graph nodes,
// relationships, and paths contribute to the subgraph.
func (h *CodeHandler) handleVisualizeQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CypherQuery string `json:"cypher_query"`
		Limit       int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		writeCypherQueryError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	if req.CypherQuery == "" {
		writeCypherQueryError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, "cypher_query is required")
		return
	}

	if capabilityUnsupported(h.profile(), visualizationGraphQueryCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"graph query visualization requires a graph-backed authoritative profile",
			ErrorCodeUnsupportedCapability,
			visualizationGraphQueryCapability,
			h.profile(),
			requiredProfile(visualizationGraphQueryCapability),
		)
		return
	}

	cypher, limit, err := boundedReadOnlyCypher(req.CypherQuery, req.Limit)
	if err != nil {
		writeCypherQueryError(w, r, http.StatusBadRequest, ErrorCodeInvalidArgument, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, nil)
	if err != nil {
		writeCypherQueryError(w, r, http.StatusInternalServerError, ErrorCodeInternalError, err.Error())
		return
	}

	truncatedRows := len(rows) > limit
	if truncatedRows {
		rows = rows[:limit]
	}

	truth := h.visualizationGraphQueryTruth()
	packet := BuildGraphQueryVisualizationPacket(rows, truth)
	if truncatedRows {
		packet.Truncation.Truncated = true
		packet.Limitations = appendReason(packet.Limitations,
			"result row window was truncated to the row limit before projection; the subgraph is a bounded subset")
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"visualization_packet": packet,
		"limit":                limit,
		"truncated":            truncatedRows,
	}, truth)
}

func (h *CodeHandler) visualizationGraphQueryTruth() *TruthEnvelope {
	return BuildTruthEnvelope(
		h.profile(),
		visualizationGraphQueryCapability,
		TruthBasisAuthoritativeGraph,
		"projected renderable subgraph from a bounded read-only graph query result",
	)
}

// searchBundlesDefaultLimit and searchBundlesMaxLimit bound the registry
// bundle catalog read so a single call cannot scan the whole package graph.
const (
	searchBundlesDefaultLimit = 50
	searchBundlesMaxLimit     = 200
)

// handleSearchBundles searches the pre-indexed package registry catalog as
// shareable bundle candidates.
//
// #3493: this handler previously ran `MATCH (r:Repository) WHERE r.name
// CONTAINS $query`, which is a repository-name search wearing a
// registry/SBOM-bundle name. A "registry bundle" is pre-indexed dependency or
// library graph content, and the only such pre-indexed, queryable registry
// catalog in the graph is the package registry (`:Package` /
// `:PackageRegistryPackage` identities materialized by the reducer). The
// handler now searches that catalog by package identity (normalized name,
// namespace, or PURL) and optionally scopes to one ecosystem, aligning with the
// list_package_registry_* read surface instead of inventing repo-name truth.
func (h *CodeHandler) handleSearchBundles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string `json:"query"`
		Ecosystem  string `json:"ecosystem"`
		UniqueOnly bool   `json:"unique_only"`
		Limit      int    `json:"limit"`
	}
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = searchBundlesDefaultLimit
	}
	if limit > searchBundlesMaxLimit {
		limit = searchBundlesMaxLimit
	}

	cypher, params := searchRegistryBundlesCypher(req.Query, req.Ecosystem, req.UniqueOnly, limit+1)

	ctx, cancel := context.WithTimeout(r.Context(), cypherQueryTimeout)
	defer cancel()

	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"bundles":   rows,
		"count":     len(rows),
		"limit":     limit,
		"truncated": truncated,
	}, BuildTruthEnvelope(h.profile(), "platform_impact.context_overview", TruthBasisAuthoritativeGraph, "resolved from bounded package registry bundle catalog"))
}

// searchRegistryBundlesCypher builds the bounded, deterministically ordered
// query over the package registry catalog. The match anchors on `:Package`
// identities (which carry the dual `:PackageRegistryPackage` label written by
// the reducer) and filters by case-insensitive substring over the package's
// normalized name, namespace, or PURL. An empty query lists the catalog head;
// a non-empty ecosystem scopes the read to one ecosystem. The query parameter
// is always bound, never interpolated, so the substring match stays
// injection-safe.
func searchRegistryBundlesCypher(query, ecosystem string, uniqueOnly bool, limit int) (string, map[string]any) {
	params := map[string]any{"limit": limit}

	cypher := `MATCH (p:Package) WHERE p.uid IS NOT NULL`
	if ecosystem != "" {
		cypher += ` AND p.ecosystem = $ecosystem`
		params["ecosystem"] = ecosystem
	}
	if query != "" {
		cypher += ` AND (toLower(coalesce(p.normalized_name, '')) CONTAINS toLower($query)` +
			` OR toLower(coalesce(p.namespace, '')) CONTAINS toLower($query)` +
			` OR toLower(coalesce(p.purl, '')) CONTAINS toLower($query))`
		params["query"] = query
	}

	projection := `RETURN`
	if uniqueOnly {
		projection += ` DISTINCT`
	}
	cypher += `
OPTIONAL MATCH (p)-[:HAS_VERSION]->(v:PackageVersion)
WITH p, count(v) AS version_count
` + projection + ` p.uid AS package_id,
       p.normalized_name AS name,
       p.ecosystem AS ecosystem,
       p.registry AS registry,
       p.namespace AS namespace,
       p.purl AS purl,
       version_count AS version_count
ORDER BY p.ecosystem, p.normalized_name, p.uid
LIMIT $limit`

	return cypher, params
}

func (h *CodeHandler) lookupComplexityRowByName(ctx context.Context, functionName, repoID string) (map[string]any, error) {
	params := map[string]any{"entity_name": functionName, "limit": complexityNameCandidateLimit + 1}
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
		ORDER BY file_path, start_line, id
		LIMIT $limit
	`
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		if err == nil && rows == nil {
			return h.runComplexityQuery(ctx, cypher, params)
		}
		return nil, err
	}
	if len(rows) > 1 {
		truncated := len(rows) > complexityNameCandidateLimit
		if truncated {
			rows = rows[:complexityNameCandidateLimit]
		}
		return nil, complexityAmbiguousError{
			FunctionName: functionName,
			RepoID:       repoID,
			Candidates:   complexityCandidateMaps(rows),
			Truncated:    truncated,
		}
	}
	return rows[0], nil
}

func (h *CodeHandler) listMostComplexFunctions(ctx context.Context, repoID string, limit int) ([]map[string]any, int, bool, error) {
	limit = normalizeComplexityListLimit(limit)
	cypher := `
		MATCH (e:Function)
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE coalesce(e.cyclomatic_complexity, 0) > 0
	`
	params := map[string]any{"limit": limit + 1}
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
		ORDER BY complexity DESC, e.name, e.id
		LIMIT $limit
	`
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, 0, false, err
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
	results, truncated := trimComplexityResults(results, limit)
	return results, limit, truncated, nil
}
