// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querygraphrows"
)

// deadCodeIncomingEdge is the strongest incoming reachability edge observed for
// a dead-code candidate.
//
// It is an alias onto querycontract rather than a declaration: the type appears
// in a ContentStore read's signature, and a shared double promoted to
// querytestutil for #6060 cannot name an unexported root type. An alias
// preserves type identity, so every existing caller and every composite literal
// is unchanged.
type deadCodeIncomingEdge = querycontract.DeadCodeIncomingEdge

// deadCodeCandidateQuery is the pre-move spelling of
// codeshaping.DeadCodeCandidateQuery, kept for the digest-pinned
// deadCodeCandidateRows body below.
type deadCodeCandidateQuery = codeshaping.DeadCodeCandidateQuery

// deadCodeCandidateContentStore is the pre-move spelling of
// codeshaping.DeadCodeCandidateContentStore, kept for the same
// digest-pinned body.
type deadCodeCandidateContentStore = codeshaping.DeadCodeCandidateContentStore

// deadCodeWeakIncomingResultKey marks a kept candidate whose only incoming
// edges were weak (repo_unique_name tier). It drives the ambiguous
// classification instead of silently treating the candidate as reachable.
func (h *CodeHandler) deadCodeCandidateRows(
	ctx context.Context,
	repoID string,
	label string,
	language string,
	limit int,
	offset int,
) ([]map[string]any, error) {
	allowedRepositoryIDs, blocked := codeContentGrantScope(ctx, repoID)
	if blocked {
		return nil, nil
	}
	query := deadCodeCandidateQuery{
		RepoID:               repoID,
		Label:                label,
		Language:             language,
		Limit:                limit,
		Offset:               offset,
		AllowedRepositoryIDs: allowedRepositoryIDs,
	}
	if content, ok := h.Content.(deadCodeCandidateContentStore); ok {
		return content.DeadCodeCandidateRows(ctx, query)
	}
	access := codeGrantAccessFilter(ctx)
	cypher := buildDeadCodeGraphCypherForLabel(repoID != "", label, language, access)
	return h.Neo4j.Run(ctx, cypher, deadCodeGraphParams(repoID, language, limit, offset, access))
}

// buildDeadCodeGraphCypherForLabel builds the graph-fallback candidate scan for
// one label. access pushes the caller's repository grant onto the Repository
// anchor `r` inside the WHERE, before the SKIP/LIMIT, so a scoped caller's page
// is taken from the granted set. The predicate is the same
// `r.id IN $allowed_repository_ids OR r.id IN $allowed_scope_ids` shape the
// relationship-story reads already run on both backends; it stays in the
// MATCH-attached WHERE because a WITH-attached WHERE is not evaluated as a
// filter on NornicDB (docs/public/reference/nornicdb-query-pitfalls.md).
func buildDeadCodeGraphCypherForLabel(
	hasRepoID bool,
	label string,
	language string,
	access querycontract.RepositoryAccessFilter,
) string {
	if !deadcode.IsDeadCodeCandidateLabel(label) {
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
` + querygraphrows.GraphSemanticMetadataProjection() + `
		ORDER BY f.relative_path, e.name, coalesce(e.uid, e.id)
		SKIP $skip
		LIMIT $limit
	`
	return cypher
}

func deadCodeGraphParams(
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
