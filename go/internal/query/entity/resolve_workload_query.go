// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// BuildResolveWorkloadQueries renders the property and relationship workload
// resolution Cypher for name. Exported for the staying queryplan
// production-binding tests that pin builder bytes; see #6060.
func BuildResolveWorkloadQueries(
	name string,
	repoID string,
	limit int,
	access querycontract.RepositoryAccessFilter,
) (string, string, map[string]any) {
	params := access.GraphParams(map[string]any{
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
	case access.Scoped():
		propertyWhere = append(propertyWhere,
			"(w.repo_id IN $allowed_repository_ids OR w.repo_id IN $allowed_scope_ids)")
		relationshipWhere = append(relationshipWhere, access.GraphCondition("repo"))
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

// BuildHydrateResolvedWorkloadRepoNamesQuery builds the repository-name hydration query. Exported for the staying queryplan execution test via the root forwarder; see #6060.
func BuildHydrateResolvedWorkloadRepoNamesQuery(
	repoIDs []string,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := access.GraphParams(map[string]any{"repo_ids": repoIDs})
	cypher := `MATCH (repo:Repository) WHERE repo.id IN $repo_ids`
	if access.Scoped() {
		cypher += " AND " + access.GraphCondition("repo")
	}
	cypher += ` RETURN repo.id AS repo_id, repo.name AS repo_name ORDER BY repo_id`
	return cypher, params
}
