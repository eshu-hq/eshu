package reducer

import (
	"context"
	"fmt"
	"strings"
)

// GraphQueryRunner executes read-only graph queries for reducer lookups.
type GraphQueryRunner interface {
	Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error)
}

// GraphInfrastructurePlatformLookup loads already-materialized infrastructure
// platform ownership from the canonical graph.
type GraphInfrastructurePlatformLookup struct {
	Graph GraphQueryRunner
}

// ListProvisionedPlatforms returns PROVISIONS_PLATFORM rows grouped by
// infrastructure repository ID.
func (l GraphInfrastructurePlatformLookup) ListProvisionedPlatforms(
	ctx context.Context,
	repoIDs []string,
) (map[string][]InfrastructurePlatformRow, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	if l.Graph == nil {
		return nil, fmt.Errorf("graph infrastructure platform lookup requires graph query runner")
	}

	result := make(map[string][]InfrastructurePlatformRow, len(repoIDs))
	for _, repoID := range repoIDs {
		repoID = strings.TrimSpace(repoID)
		if repoID == "" {
			continue
		}
		rows, err := l.Graph.Run(ctx, provisionedInfrastructurePlatformsCypher, map[string]any{
			"repo_id": repoID,
		})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			platformRepoID := anyToString(row["repo_id"])
			platformID := anyToString(row["platform_id"])
			platformKind := anyToString(row["platform_kind"])
			if platformRepoID == "" || platformID == "" || platformKind == "" {
				continue
			}
			result[platformRepoID] = append(result[platformRepoID], InfrastructurePlatformRow{
				RepoID:           platformRepoID,
				PlatformID:       platformID,
				PlatformName:     anyToString(row["platform_name"]),
				PlatformKind:     platformKind,
				PlatformProvider: anyToString(row["platform_provider"]),
				PlatformRegion:   anyToString(row["platform_region"]),
				PlatformLocator:  anyToString(row["platform_locator"]),
			})
		}
	}
	return result, nil
}

const provisionedInfrastructurePlatformsCypher = `MATCH (repo:Repository {id: $repo_id})-[:PROVISIONS_PLATFORM]->(p:Platform)
RETURN repo.id AS repo_id,
       p.id AS platform_id,
       p.name AS platform_name,
       p.kind AS platform_kind,
       p.provider AS platform_provider,
       p.region AS platform_region,
       p.locator AS platform_locator`
