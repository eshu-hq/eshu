// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

func buildResolveWorkloadQueries(
	name string,
	repoID string,
	limit int,
	access repositoryAccessFilter,
) (string, string, map[string]any) {
	params := access.graphParams(map[string]any{
		"name":  name,
		"limit": limit,
	})
	propertyWhere := []string{"w.name = $name"}
	relationshipWhere := []string{"w.name = $name"}
	switch {
	case repoID != "":
		params["repo_id"] = repoID
		propertyWhere = append(propertyWhere, "w.repo_id = $repo_id")
		relationshipWhere = append(relationshipWhere, "repo.id = $repo_id")
	case access.scoped():
		propertyWhere = append(propertyWhere,
			"(w.repo_id IN $allowed_repository_ids OR w.repo_id IN $allowed_scope_ids)")
		relationshipWhere = append(relationshipWhere, access.graphCondition("repo"))
	}

	propertyCypher := `
		MATCH (w:Workload)
		WHERE ` + strings.Join(propertyWhere, " AND ") + `
		RETURN w.id AS id,
		       labels(w) AS labels,
		       w.name AS name,
		       w.repo_id AS repo_id
		ORDER BY id
		LIMIT $limit
	`
	relationshipCypher := `
		MATCH (w:Workload)<-[:DEFINES]-(repo:Repository)
		WHERE ` + strings.Join(relationshipWhere, " AND ") + `
		RETURN w.id AS id,
		       labels(w) AS labels,
		       w.name AS name,
		       min(repo.id) AS repo_id
		ORDER BY id
		LIMIT $limit
	`
	return propertyCypher, relationshipCypher, params
}

func buildHydrateResolvedWorkloadRepoNamesQuery(
	repoIDs []string,
	access repositoryAccessFilter,
) (string, map[string]any) {
	params := access.graphParams(map[string]any{"repo_ids": repoIDs})
	cypher := `MATCH (repo:Repository) WHERE repo.id IN $repo_ids`
	if access.scoped() {
		cypher += " AND " + access.graphCondition("repo")
	}
	cypher += ` RETURN repo.id AS repo_id, repo.name AS repo_name ORDER BY repo_id`
	return cypher, params
}
