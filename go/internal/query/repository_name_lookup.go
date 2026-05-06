package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
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

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
