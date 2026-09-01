// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// HydrateResolvedEntityRepoIdentity backfills repo_id/repo_name on entities
// that a graph or content read returned without them, using the content
// store first and a bounded graph traversal second. It returns whether the
// graph traversal ran (so a caller can attribute truth accordingly).
//
// This moved here from root package query's entity_resolve_identity.go
// (#6060) so a handler-family subpackage -- currently
// internal/query/code's code_relationships.go -- can call the exact same
// logic root's EntityHandler and entity_content_types.go use, rather than a
// package-local copy that could silently drift from the #6408 fix below.
//
// resolvedEntityRepoProjectionPlaceholder/ClearResolvedEntityRepoProjectionPlaceholders
// scrub a live backend defect (#6408): a second-hop node property reached
// through OPTIONAL MATCH can come back as the literal text of its own
// projection expression (for example repo_id == "r.id") instead of the
// property's actual value. The scrubber recognizes the four expression
// shapes the query below can produce and blanks them out rather than passing
// bad data through. When #6408 is fixed at the source, this workaround (and
// its call at the top of the entity loop below) should be removed -- do not
// change its matching behavior while it is still needed, and do not
// duplicate it: a duplicate is exactly how a partial fix (a fifth shape
// added, or the whole approach replaced) goes unnoticed on the copy nobody
// remembered to update.
func HydrateResolvedEntityRepoIdentity(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	entities []map[string]any,
) (bool, error) {
	if len(entities) == 0 {
		return false, nil
	}
	access := RepositoryAccessFilterFromContext(ctx)

	for _, entity := range entities {
		ClearResolvedEntityRepoProjectionPlaceholders(entity)
		if resolvedEntityIsRepository(entity) {
			if entityRepoIdentityString(entity, "repo_id") == "" {
				entity["repo_id"] = entityRepoIdentityString(entity, "id")
			}
			if entityRepoIdentityString(entity, "repo_name") == "" {
				entity["repo_name"] = entityRepoIdentityString(entity, "name")
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
	rows, err := graph.Run(ctx, query, access.GraphParams(map[string]any{"entity_ids": entityRepoIdentitySortedUniqueStrings(entityIDs)}))
	if err != nil {
		return true, fmt.Errorf("hydrate resolved entity repo identity: %w", err)
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
		repo := reposByEntity[entityRepoIdentityString(entity, "id")]
		if entityRepoIdentityString(entity, "repo_id") == "" {
			entity["repo_id"] = repo["repo_id"]
		}
		if entityRepoIdentityString(entity, "repo_name") == "" {
			entity["repo_name"] = repo["repo_name"]
		}
	}
	return true, nil
}

func hydrateResolvedEntityRepoIdentityFromContent(
	ctx context.Context,
	content ContentStore,
	entities []map[string]any,
) error {
	if content == nil {
		return nil
	}

	access := RepositoryAccessFilterFromContext(ctx)
	repoIDsNeedingName := make([]string, 0, len(entities))
	for _, entity := range entities {
		if repoID := entityRepoIdentityString(entity, "repo_id"); repoID != "" && entityRepoIdentityString(entity, "repo_name") == "" {
			repoIDsNeedingName = append(repoIDsNeedingName, repoID)
		}
		if entityRepoIdentityString(entity, "repo_id") != "" && entityRepoIdentityString(entity, "repo_name") != "" {
			continue
		}
		entityID := entityRepoIdentityString(entity, "id")
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
		if entityRepoIdentityString(entity, "repo_id") == "" {
			entity["repo_id"] = row.RepoID
		}
		if entityRepoIdentityString(entity, "repo_name") == "" {
			repoIDsNeedingName = append(repoIDsNeedingName, row.RepoID)
		}
	}

	repoNames, err := entityRepoIdentityContentRepositoryNamesByID(ctx, content, repoIDsNeedingName)
	if err != nil {
		return err
	}
	for _, entity := range entities {
		if entityRepoIdentityString(entity, "repo_name") != "" {
			continue
		}
		repoName := repoNames[entityRepoIdentityString(entity, "repo_id")]
		if repoName != "" {
			entity["repo_name"] = repoName
		}
	}
	return nil
}

func entityRepoIdentityContentRepositoryNamesByID(
	ctx context.Context,
	content ContentStore,
	repoIDs []string,
) (map[string]string, error) {
	repoIDs = entityRepoIdentitySortedUniqueStrings(repoIDs)
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
		if entityRepoIdentityString(entity, "repo_id") != "" && entityRepoIdentityString(entity, "repo_name") != "" {
			continue
		}
		if !resolvedEntityNeedsWorkloadRepoBackfill(entity) {
			continue
		}
		if entityID := entityRepoIdentityString(entity, "id"); entityID != "" {
			entityIDs = append(entityIDs, entityID)
		}
	}
	return entityIDs
}

// ClearResolvedEntityRepoProjectionPlaceholders is the #6408 scrubber; see
// HydrateResolvedEntityRepoIdentity's doc comment for what it is guarding
// against and why it must stay a single copy.
func ClearResolvedEntityRepoProjectionPlaceholders(entity map[string]any) {
	if resolvedEntityRepoProjectionPlaceholder(entityRepoIdentityString(entity, "repo_id"), "id") {
		entity["repo_id"] = ""
	}
	if resolvedEntityRepoProjectionPlaceholder(entityRepoIdentityString(entity, "repo_name"), "name") {
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
	for _, label := range entityRepoIdentityLabelStrings(entity["labels"]) {
		if label == "Repository" {
			return true
		}
	}
	return false
}

func resolvedEntityNeedsWorkloadRepoBackfill(entity map[string]any) bool {
	for _, label := range entityRepoIdentityLabelStrings(entity["labels"]) {
		if label == "Workload" || label == "WorkloadInstance" {
			return true
		}
	}
	return false
}

// entityRepoIdentityString, entityRepoIdentityLabelStrings and
// entityRepoIdentitySortedUniqueStrings are small, self-contained copies of
// root package query's entityString (entity_resolve_results.go),
// entityLabelStrings (entity_resolve_results.go), and sortedUniqueStrings
// (repository_name_lookup.go). They are pure, stateless, and used broadly by
// other root files outside this promoted logic, so they were copied rather
// than promoted/aliased -- promoting them would have pulled unrelated root
// files into this package's compatibility surface for no benefit. Mirrors
// the packagereg family's derefString/derefBool precedent (#6060).
func entityRepoIdentityString(entity map[string]any, key string) string {
	value, _ := entity[key].(string)
	return strings.TrimSpace(value)
}

func entityRepoIdentityLabelStrings(raw any) []string {
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

func entityRepoIdentitySortedUniqueStrings(values []string) []string {
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
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
