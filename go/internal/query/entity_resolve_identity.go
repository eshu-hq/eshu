package query

import (
	"context"
	"fmt"
	"strings"
)

func hydrateResolvedEntityRepoIdentity(ctx context.Context, graph GraphQuery, content ContentStore, entities []map[string]any) error {
	if len(entities) == 0 {
		return nil
	}

	for _, entity := range entities {
		clearResolvedEntityRepoProjectionPlaceholders(entity)
		if resolvedEntityIsRepository(entity) {
			if entityString(entity, "repo_id") == "" {
				entity["repo_id"] = entityString(entity, "id")
			}
			if entityString(entity, "repo_name") == "" {
				entity["repo_name"] = entityString(entity, "name")
			}
			continue
		}
	}

	if err := hydrateResolvedEntityRepoIdentityFromContent(ctx, content, entities); err != nil {
		return err
	}
	entityIDs := workloadEntityIDsNeedingRepoBackfill(entities)
	if graph == nil || len(entityIDs) == 0 {
		return nil
	}

	query := `
		UNWIND $entity_ids AS entity_id
		MATCH (e) WHERE e.id = entity_id
		OPTIONAL MATCH (repo:Repository)-[:DEFINES]->(direct:Workload)
		WHERE direct = e
		OPTIONAL MATCH (repoViaInstance:Repository)-[:DEFINES]->(instanceWorkload:Workload)<-[:INSTANCE_OF]-(e)
		RETURN entity_id,
		       coalesce(repo.id, repoViaInstance.id) AS repo_id,
		       coalesce(repo.name, repoViaInstance.name) AS repo_name
	`
	rows, err := graph.Run(ctx, query, map[string]any{"entity_ids": sortedUniqueStrings(entityIDs)})
	if err != nil {
		return fmt.Errorf("hydrate resolved entity repo identity: %w", err)
	}

	reposByEntity := make(map[string]map[string]string, len(rows))
	for _, row := range rows {
		entityID := StringVal(row, "entity_id")
		repoID := StringVal(row, "repo_id")
		repoName := StringVal(row, "repo_name")
		if entityID == "" || (repoID == "" && repoName == "") {
			continue
		}
		reposByEntity[entityID] = map[string]string{
			"repo_id":   repoID,
			"repo_name": repoName,
		}
	}

	for _, entity := range entities {
		repo := reposByEntity[entityString(entity, "id")]
		if entityString(entity, "repo_id") == "" {
			entity["repo_id"] = repo["repo_id"]
		}
		if entityString(entity, "repo_name") == "" {
			entity["repo_name"] = repo["repo_name"]
		}
	}
	return nil
}

func hydrateResolvedEntityRepoIdentityFromContent(
	ctx context.Context,
	content ContentStore,
	entities []map[string]any,
) error {
	if content == nil {
		return nil
	}

	repoIDsNeedingName := make([]string, 0, len(entities))
	for _, entity := range entities {
		if repoID := entityString(entity, "repo_id"); repoID != "" && entityString(entity, "repo_name") == "" {
			repoIDsNeedingName = append(repoIDsNeedingName, repoID)
		}
		if entityString(entity, "repo_id") != "" && entityString(entity, "repo_name") != "" {
			continue
		}
		entityID := entityString(entity, "id")
		if entityID == "" || resolvedEntityIsRepository(entity) {
			continue
		}
		row, err := content.GetEntityContent(ctx, entityID)
		if err != nil {
			return fmt.Errorf("hydrate resolved entity repo identity from content: %w", err)
		}
		if row == nil || strings.TrimSpace(row.RepoID) == "" {
			continue
		}
		if entityString(entity, "repo_id") == "" {
			entity["repo_id"] = row.RepoID
		}
		if entityString(entity, "repo_name") == "" {
			repoIDsNeedingName = append(repoIDsNeedingName, row.RepoID)
		}
	}

	repoNames, err := contentRepositoryNamesByID(ctx, content, repoIDsNeedingName)
	if err != nil {
		return err
	}
	for _, entity := range entities {
		if entityString(entity, "repo_name") != "" {
			continue
		}
		repoName := repoNames[entityString(entity, "repo_id")]
		if repoName != "" {
			entity["repo_name"] = repoName
		}
	}
	return nil
}

func contentRepositoryNamesByID(
	ctx context.Context,
	content ContentStore,
	repoIDs []string,
) (map[string]string, error) {
	repoIDs = sortedUniqueStrings(repoIDs)
	if content == nil || len(repoIDs) == 0 {
		return nil, nil
	}

	entries, err := content.ListRepositories(ctx)
	if err != nil {
		return nil, fmt.Errorf("hydrate resolved entity repository names from content catalog: %w", err)
	}
	want := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		want[repoID] = struct{}{}
	}
	names := make(map[string]string, len(repoIDs))
	for _, entry := range entries {
		if _, ok := want[strings.TrimSpace(entry.ID)]; !ok {
			continue
		}
		if name := strings.TrimSpace(entry.Name); name != "" {
			names[strings.TrimSpace(entry.ID)] = name
		}
	}
	return names, nil
}

func workloadEntityIDsNeedingRepoBackfill(entities []map[string]any) []string {
	entityIDs := make([]string, 0, len(entities))
	for _, entity := range entities {
		if entityString(entity, "repo_id") != "" && entityString(entity, "repo_name") != "" {
			continue
		}
		if !resolvedEntityNeedsWorkloadRepoBackfill(entity) {
			continue
		}
		if entityID := entityString(entity, "id"); entityID != "" {
			entityIDs = append(entityIDs, entityID)
		}
	}
	return entityIDs
}

func clearResolvedEntityRepoProjectionPlaceholders(entity map[string]any) {
	if resolvedEntityRepoProjectionPlaceholder(entityString(entity, "repo_id"), "id") {
		entity["repo_id"] = ""
	}
	if resolvedEntityRepoProjectionPlaceholder(entityString(entity, "repo_name"), "name") {
		entity["repo_name"] = ""
	}
}

func resolvedEntityRepoProjectionPlaceholder(value string, property string) bool {
	value = strings.TrimSpace(value)
	property = strings.TrimSpace(property)
	switch value {
	case "r." + property,
		"repo." + property,
		"repoViaInstance." + property,
		"coalesce(repo." + property + ", repoViaInstance." + property + ")":
		return true
	default:
		return false
	}
}

func resolvedEntityIsRepository(entity map[string]any) bool {
	for _, label := range entityLabelStrings(entity["labels"]) {
		if label == "Repository" {
			return true
		}
	}
	return false
}

func resolvedEntityNeedsWorkloadRepoBackfill(entity map[string]any) bool {
	for _, label := range entityLabelStrings(entity["labels"]) {
		if label == "Workload" || label == "WorkloadInstance" {
			return true
		}
	}
	return false
}
