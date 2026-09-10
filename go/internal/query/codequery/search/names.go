// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// SearchGlobalEntityNames runs a global entity-name lookup over the
// content store: substring or exact match across every repository for an
// unscoped caller, or bounded to the granted repositories for a scoped
// one. A grantless caller resolves to no rows.
func SearchGlobalEntityNames(
	ctx context.Context,
	store querycontract.ContentStore,
	name, language string,
	limit int,
	exact bool,
) ([]map[string]any, error) {
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return []map[string]any{}, nil
	}
	searcher, ok := store.(querycontract.EntityNameSearcher)
	if !ok {
		return nil, querycontract.ErrEntityNameSearchUnavailable
	}
	search := querycontract.EntityNameSearch{Name: name, Match: querycontract.EntityNameMatchSubstring, Scope: querycontract.EntityNameScopeAll, Limit: limit}
	if exact {
		search.Match = querycontract.EntityNameMatchExact
	}
	if access.Scoped() {
		search.Scope = querycontract.EntityNameScopeRepositories
		search.RepositoryIDs = access.RepositorySearchIDs()
	}
	if strings.TrimSpace(language) != "" {
		search.Languages = querycontract.NormalizedLanguageVariants(language)
	}
	rows, err := searcher.SearchEntityNames(ctx, search)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]any, 0, len(rows))
	for _, entity := range rows {
		result := map[string]any{
			"entity_id": entity.EntityID, "entity_name": entity.EntityName, "entity_type": entity.EntityType,
			"name": entity.EntityName, "labels": []string{entity.EntityType}, "repo_name": entity.RepoName,
			"file_path": entity.RelativePath, "start_line": entity.StartLine, "end_line": entity.EndLine,
			"language": entity.Language, "metadata": entity.Metadata, "repo_id": entity.RepoID,
		}
		entitysemantics.AttachSemanticSummary(result)
		results = append(results, result)
	}
	return results, nil
}
