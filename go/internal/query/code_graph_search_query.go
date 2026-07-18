// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func buildSearchGraphEntitiesQuery(
	repoID string,
	query string,
	language string,
	limit int,
	exact bool,
	access repositoryAccessFilter,
) (string, map[string]any) {
	cypher := `
		MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
	`
	params := map[string]any{
		"query": query,
		"limit": limit,
	}
	if repoID != "" {
		cypher = `
			MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)
		`
		params["repo_id"] = repoID
	}
	if exact {
		cypher += " WHERE e.name = $query"
	} else {
		cypher += " WHERE e.name CONTAINS $query"
	}
	if repoID == "" && access.scoped() {
		cypher += access.graphPredicate("r")
		params = access.graphParams(params)
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
	return cypher, params
}
