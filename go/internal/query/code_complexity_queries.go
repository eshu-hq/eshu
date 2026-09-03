// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// lookupComplexityRowByName resolves a complexity row by function name. access
// appends the caller's grant to the Repository anchor's WHERE, so a scoped
// caller can neither read nor be told about a function in an ungranted
// repository -- including through the ambiguity candidate list this returns
// when the name matches more than one entity.
func (h *CodeHandler) lookupComplexityRowByName(
	ctx context.Context,
	functionName, repoID string,
	access repositoryAccessFilter,
) (map[string]any, error) {
	params := map[string]any{"entity_name": functionName, "limit": complexityNameCandidateLimit + 1}
	cypher := "\n\t\tMATCH (repo:Repository)-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)\n\t\tWHERE e.name = $entity_name"
	if repoID != "" {
		cypher = "\n\t\tMATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)\n\t\tWHERE e.name = $entity_name AND repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("repo") + "\n"
	params = access.GraphParams(params)
	cypher += complexityCandidateProjection() + `
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

// lookupComplexityRowByID resolves a complexity row by entity id.
//
// This branch previously carried no repository predicate at all: it ignored
// even a repo_id the caller supplied, so an entity id alone returned that
// entity's repository, path, and metrics from any repository in the index. It
// now anchors on the supplied repository when there is one, and always appends
// the caller's grant (#5167).
func (h *CodeHandler) lookupComplexityRowByID(
	ctx context.Context,
	entityID, repoID string,
	access repositoryAccessFilter,
) (map[string]any, error) {
	params := map[string]any{"entity_id": entityID}
	where := ""
	if repoID != "" {
		where = "\n\t\tWHERE repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	if predicate := access.GraphCondition("repo"); access.Scoped() {
		if where == "" {
			where = "\n\t\tWHERE " + predicate
		} else {
			where += " AND " + predicate
		}
	}
	row, err := h.runComplexityQuery(ctx, `
		MATCH (repo:Repository)-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e {id: $entity_id})`+where+`
`+complexityCandidateProjection()+`
		LIMIT 1
	`, access.GraphParams(params))
	return row, err
}

func complexityCandidateProjection() string {
	return `
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
		       count(DISTINCT outgoingRel) + count(DISTINCT incomingRel) as total_relationships,
` + graphSemanticMetadataProjection()
}

// complexityListAnchor picks the clause that binds the Repository side of the
// complexity list scan.
//
// An unscoped caller keeps the long-standing shape: a bare MATCH (e:Function)
// with the File/Repository hops in an OPTIONAL MATCH, so a function the graph
// has not attributed to a repository still appears with null repository
// columns. That text is unchanged by #5167.
//
// A scoped caller gets the hops as a required MATCH instead, because the
// alternative does not filter. In Cypher a WHERE attached to an OPTIONAL MATCH
// constrains the optional pattern, not the driving row set, so appending a
// grant predicate there returns every Function in the corpus with only the
// repository columns nulled -- name, language, line span, complexity and the
// semantic metadata all still reach the caller. That was measured against
// NornicDB v1.2.3, where the same shape additionally projected the literal text
// "repo.id" into the repo_id column; both results are recorded in
// docs/internal/evidence/5167-code-family-batch-1.md. With a required MATCH the
// grant predicate sits in a MATCH-attached WHERE ahead of the ORDER BY and
// LIMIT, so the page is drawn from the granted set, and a function with no
// repository path at all is dropped -- the fail-closed answer for a row whose
// tenant cannot be determined.
func complexityListAnchor(access repositoryAccessFilter) string {
	if access.Scoped() {
		return `
		MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE coalesce(e.cyclomatic_complexity, 0) > 0
	`
	}
	return `
		MATCH (e:Function)
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		WHERE coalesce(e.cyclomatic_complexity, 0) > 0
	`
}

// listMostComplexFunctions ranks the most complex functions in scope. The
// caller's grant, and any repo_id it supplied, land in the WHERE attached to
// the clause complexityListAnchor chose -- required for a scoped caller,
// optional for an unscoped one.
func (h *CodeHandler) listMostComplexFunctions(
	ctx context.Context,
	repoID string,
	limit int,
	access repositoryAccessFilter,
) ([]map[string]any, int, bool, error) {
	limit = normalizeComplexityListLimit(limit)
	cypher := complexityListAnchor(access)
	params := map[string]any{"limit": limit + 1}
	if repoID != "" {
		cypher += " AND repo.id = $repo_id"
		params["repo_id"] = repoID
	}
	cypher += access.GraphPredicate("repo")
	params = access.GraphParams(params)
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
