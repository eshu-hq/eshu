// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

func (h *CodeHandler) searchGlobalEntityNames(ctx context.Context, name, language string, limit int, exact bool) ([]map[string]any, error) {
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return []map[string]any{}, nil
	}
	searcher, ok := h.Content.(EntityNameSearcher)
	if !ok {
		return nil, errEntityNameSearchUnavailable
	}
	search := EntityNameSearch{Name: name, Match: EntityNameMatchSubstring, Scope: EntityNameScopeAll, Limit: limit}
	if exact {
		search.Match = EntityNameMatchExact
	}
	if access.scoped() {
		search.Scope = EntityNameScopeRepositories
		search.RepositoryIDs = access.repositorySearchIDs()
	}
	if strings.TrimSpace(language) != "" {
		search.Languages = normalizedLanguageVariants(language)
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
		attachSemanticSummary(result)
		results = append(results, result)
	}
	return results, nil
}
