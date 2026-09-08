// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryreadmodel

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// QueryRepositoryNamesByID returns the display name for each repository id
// visible to the caller, keyed by repository id. A nil graph or empty id
// set returns nil without a query; rows missing an id or name are skipped.
func QueryRepositoryNamesByID(ctx context.Context, graph querycontract.GraphQuery, repoIDs []string) (map[string]string, error) {
	if graph == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	query := `
		MATCH (r:Repository) WHERE r.id IN $repo_ids
		RETURN r.id AS repo_id, r.name AS repo_name
		ORDER BY repo_name
	`
	rows, err := graph.Run(ctx, query, map[string]any{"repo_ids": querycontract.UniqueSortedStrings(repoIDs)})
	if err != nil {
		return nil, fmt.Errorf("query repository names by id: %w", err)
	}

	names := make(map[string]string, len(rows))
	for _, row := range rows {
		repoID := querycontract.StringVal(row, "repo_id")
		repoName := querycontract.StringVal(row, "repo_name")
		if repoID == "" || repoName == "" {
			continue
		}
		names[repoID] = repoName
	}
	return names, nil
}
