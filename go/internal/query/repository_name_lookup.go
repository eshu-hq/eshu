// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func queryRepositoryNamesByID(ctx context.Context, graph GraphQuery, repoIDs []string) (map[string]string, error) {
	if graph == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	query := `
		MATCH (r:Repository) WHERE r.id IN $repo_ids
		RETURN r.id AS repo_id, r.name AS repo_name
		ORDER BY repo_name
	`
	rows, err := graph.Run(ctx, query, map[string]any{"repo_ids": sortedUniqueStrings(repoIDs)})
	if err != nil {
		return nil, fmt.Errorf("query repository names by id: %w", err)
	}

	names := make(map[string]string, len(rows))
	for _, row := range rows {
		repoID := StringVal(row, "repo_id")
		repoName := StringVal(row, "repo_name")
		if repoID == "" || repoName == "" {
			continue
		}
		names[repoID] = repoName
	}
	return names, nil
}

// sortedUniqueStrings drops blank and duplicate values and returns the set
// sorted. It is behavior-identical to querycontract.UniqueSortedStrings; the
// implementation moved to querycontract for #6060 and this wrapper keeps
// root callers unchanged.
func sortedUniqueStrings(values []string) []string {
	return querycontract.UniqueSortedStrings(values)
}
