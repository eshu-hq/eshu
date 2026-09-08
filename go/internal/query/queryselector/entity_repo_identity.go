// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryselector

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// HydrateResolvedEntityRepoIdentity fills in a resolved entity's repo_id and
// repo_name under the caller's access filter, first from graph-projected
// Repository/Workload shape, then from the content catalog, then from a
// bounded graph read for workload entities the first two passes could not
// place. The implementation moved from root's entity_resolve_identity.go for
// #6060 so a handler-family subpackage can hydrate the same repo identity
// without importing root.
func HydrateResolvedEntityRepoIdentity(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	entities []map[string]any,
) (bool, error) {
	if len(entities) == 0 {
		return false, nil
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)

	for _, entity := range entities {
		querycontract.ClearResolvedEntityRepoProjectionPlaceholders(entity)
		if resolvedEntityIsRepository(entity) {
			if EntityString(entity, "repo_id") == "" {
				entity["repo_id"] = EntityString(entity, "id")
			}
			if EntityString(entity, "repo_name") == "" {
				entity["repo_name"] = EntityString(entity, "name")
			}
			continue
		}
	}

	if err := hydrateResolvedEntityRepoIdentityFromContent(ctx, content, entities); err != nil {
		return false, err
	}
	entityIDs := workloadEntityIDsNeedingRepoBackfill(entities)
	if graph == nil || len(entityIDs) == 0 {
		return false, nil
	}

	query := `
		UNWIND $entity_ids AS entity_id
		MATCH (e) WHERE e.id = entity_id
		OPTIONAL MATCH (repo:Repository)-[:DEFINES]->(direct:Workload)
		WHERE direct = e
		` + access.GraphPredicate("repo") + `
		OPTIONAL MATCH (repoViaInstance:Repository)-[:DEFINES]->(instanceWorkload:Workload)<-[:INSTANCE_OF]-(e)
		` + access.GraphWhereClause("repoViaInstance") + `
		RETURN entity_id,
		       coalesce(repo.id, repoViaInstance.id) AS repo_id,
		       coalesce(repo.name, repoViaInstance.name) AS repo_name
	`
	rows, err := graph.Run(ctx, query, access.GraphParams(map[string]any{"entity_ids": querycontract.UniqueSortedStrings(entityIDs)}))
	if err != nil {
		return true, fmt.Errorf("hydrate resolved entity repo identity: %w", err)
	}

	reposByEntity := make(map[string]map[string]string, len(rows))
	for _, row := range rows {
		entityID := querycontract.StringVal(row, "entity_id")
		repoID := querycontract.StringVal(row, "repo_id")
		repoName := querycontract.StringVal(row, "repo_name")
		if entityID == "" || (repoID == "" && repoName == "") {
			continue
		}
		reposByEntity[entityID] = map[string]string{
			"repo_id":   repoID,
			"repo_name": repoName,
		}
	}

	for _, entity := range entities {
		repo := reposByEntity[EntityString(entity, "id")]
		if EntityString(entity, "repo_id") == "" {
			entity["repo_id"] = repo["repo_id"]
		}
		if EntityString(entity, "repo_name") == "" {
			entity["repo_name"] = repo["repo_name"]
		}
	}
	return true, nil
}

func hydrateResolvedEntityRepoIdentityFromContent(
	ctx context.Context,
	content querycontract.ContentStore,
	entities []map[string]any,
) error {
	if content == nil {
		return nil
	}

	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	repoIDsNeedingName := make([]string, 0, len(entities))
	for _, entity := range entities {
		if repoID := EntityString(entity, "repo_id"); repoID != "" && EntityString(entity, "repo_name") == "" {
			repoIDsNeedingName = append(repoIDsNeedingName, repoID)
		}
		if EntityString(entity, "repo_id") != "" && EntityString(entity, "repo_name") != "" {
			continue
		}
		entityID := EntityString(entity, "id")
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
		if !access.AllowsRepositoryID(row.RepoID) {
			continue
		}
		if EntityString(entity, "repo_id") == "" {
			entity["repo_id"] = row.RepoID
		}
		if EntityString(entity, "repo_name") == "" {
			repoIDsNeedingName = append(repoIDsNeedingName, row.RepoID)
		}
	}

	repoNames, err := contentRepositoryNamesByID(ctx, content, repoIDsNeedingName)
	if err != nil {
		return err
	}
	for _, entity := range entities {
		if EntityString(entity, "repo_name") != "" {
			continue
		}
		repoName := repoNames[EntityString(entity, "repo_id")]
		if repoName != "" {
			entity["repo_name"] = repoName
		}
	}
	return nil
}

func contentRepositoryNamesByID(
	ctx context.Context,
	content querycontract.ContentStore,
	repoIDs []string,
) (map[string]string, error) {
	repoIDs = querycontract.UniqueSortedStrings(repoIDs)
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
		if EntityString(entity, "repo_id") != "" && EntityString(entity, "repo_name") != "" {
			continue
		}
		if !resolvedEntityNeedsWorkloadRepoBackfill(entity) {
			continue
		}
		if entityID := EntityString(entity, "id"); entityID != "" {
			entityIDs = append(entityIDs, entityID)
		}
	}
	return entityIDs
}

func resolvedEntityIsRepository(entity map[string]any) bool {
	for _, label := range EntityLabelStrings(entity["labels"]) {
		if label == "Repository" {
			return true
		}
	}
	return false
}

func resolvedEntityNeedsWorkloadRepoBackfill(entity map[string]any) bool {
	for _, label := range EntityLabelStrings(entity["labels"]) {
		if label == "Workload" || label == "WorkloadInstance" {
			return true
		}
	}
	return false
}

// EntityString reads a string field off a decoded entity map, trimmed. The
// implementation moved from root's entity_resolve_results.go for #6060 so a
// handler-family subpackage can read the same field without importing root.
func EntityString(entity map[string]any, key string) string {
	value, _ := entity[key].(string)
	return strings.TrimSpace(value)
}

// EntityLabelStrings normalizes a decoded entity's "labels" value ([]string
// or []any of strings) into a []string, dropping non-string and blank
// entries. The implementation moved from root's entity_resolve_results.go
// for #6060 so a handler-family subpackage can read the same labels without
// importing root.
func EntityLabelStrings(raw any) []string {
	switch labels := raw.(type) {
	case []string:
		return labels
	case []any:
		result := make([]string, 0, len(labels))
		for _, label := range labels {
			value, ok := label.(string)
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			result = append(result, value)
		}
		return result
	default:
		return nil
	}
}
