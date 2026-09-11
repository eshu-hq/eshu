// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/graph/rows"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// BuildDeadCodeGraphCypherForLabel builds the graph-fallback candidate scan for
// one label. access pushes the caller's repository grant onto the Repository
// anchor `r` inside the WHERE, before the SKIP/LIMIT, so a scoped caller's page
// is taken from the granted set. The predicate is the same
// `r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` shape the
// relationship-story reads already run on both backends; it stays in the
// MATCH-attached WHERE because a WITH-attached WHERE is not evaluated as a
// filter on NornicDB (docs/public/reference/nornicdb-query-pitfalls.md).
func BuildDeadCodeGraphCypherForLabel(
	hasRepoID bool,
	label string,
	language string,
	access querycontract.RepositoryAccessFilter,
) string {
	if !IsDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	cypher := `
			MATCH (e:` + label + `)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		`
	if hasRepoID {
		cypher = `
			MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e:` + label + `)
		`
	}
	where := make([]string, 0, 2)
	if strings.TrimSpace(language) != "" {
		where = append(where, "toLower(coalesce(e.language, f.language, '')) = $language")
	}
	if access.Scoped() {
		where = append(where, access.GraphCondition("r"))
	}
	if len(where) > 0 {
		cypher += `
		WHERE ` + strings.Join(where, " AND ") + `
	`
	}
	cypher += `
		RETURN coalesce(e.uid, e.id) as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
	` + rows.GraphSemanticMetadataProjection() + `
		ORDER BY f.relative_path, e.name, coalesce(e.uid, e.id)
		SKIP $skip
		LIMIT $limit
	`
	return cypher
}

// DeadCodeGraphParams binds the candidate-scan parameters: paging, the
// optional repository and language filters, and the caller's grant.
func DeadCodeGraphParams(
	repoID string,
	language string,
	limit int,
	skip int,
	access querycontract.RepositoryAccessFilter,
) map[string]any {
	params := access.GraphParams(map[string]any{"limit": limit, "skip": skip})
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
	}
	if strings.TrimSpace(language) != "" {
		params["language"] = strings.ToLower(strings.TrimSpace(language))
	}
	return params
}
