// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
	"strings"
)

type deploymentSourceResult struct {
	rows   []map[string]any
	limits map[string]any
}

func (h *ImpactHandler) fetchDeploymentSourceResult(
	ctx context.Context,
	workloadID string,
	repoID string,
) (deploymentSourceResult, error) {
	if h == nil || h.Neo4j == nil {
		return deploymentSourceResult{}, nil
	}
	return fetchDeploymentSourceResultFromGraph(ctx, h.Neo4j, workloadID, repoID)
}

func fetchDeploymentSourcesFromGraph(
	ctx context.Context,
	reader GraphQuery,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	result, err := fetchDeploymentSourceResultFromGraph(ctx, reader, workloadID, repoID)
	return result.rows, err
}

func fetchDeploymentSourceResultFromGraph(
	ctx context.Context,
	reader GraphQuery,
	workloadID string,
	repoID string,
) (deploymentSourceResult, error) {
	queryLimit := contextStoryItemLimit + 1
	access := repositoryAccessFilterFromContext(ctx)
	canonicalRows, err := fetchCanonicalDeploymentSourceRows(ctx, reader, workloadID, queryLimit, access)
	if err != nil {
		return deploymentSourceResult{}, err
	}
	canonicalReachedSentinel := len(canonicalRows) >= queryLimit
	canonicalRows = deploymentSourceRowsWithEndpoints(canonicalRows, "DEPLOYMENT_SOURCE", "")
	canonicalRows = deploymentSourceRowsWithCanonicalEndpoints(canonicalRows)

	repositoryRows, err := fetchRepositoryDeploymentSourceRows(ctx, reader, repoID, queryLimit, access)
	if err != nil {
		return deploymentSourceResult{}, err
	}
	repositoryReachedSentinel := len(repositoryRows) >= queryLimit
	repositoryRows = deploymentSourceRowsWithEndpoints(repositoryRows, "DEPLOYS_FROM", repoID)
	fluxTargetBindings, err := fetchFluxDeploymentSourceTargetBindings(ctx, reader, repoID, queryLimit, access)
	if err != nil {
		return deploymentSourceResult{}, err
	}
	fluxTargetBindingsReachedSentinel := len(fluxTargetBindings) >= queryLimit
	repositoryRows = attachFluxDeploymentSourceTargetBindings(repositoryRows, fluxTargetBindings, fluxTargetBindingsReachedSentinel)
	repositoryRows = deploymentSourceRowsWithCanonicalEndpoints(repositoryRows)
	merged, err := normalizedDeploymentSources(mergeDeploymentSourceRows(canonicalRows, repositoryRows))
	if err != nil {
		return deploymentSourceResult{}, err
	}
	sortDeploymentSources(merged)
	observedCount := len(merged)
	rows, mergedTruncated := capMapRows(merged, contextStoryItemLimit)
	lowerBound := canonicalReachedSentinel || repositoryReachedSentinel || fluxTargetBindingsReachedSentinel
	return deploymentSourceResult{
		rows: rows,
		limits: map[string]any{
			"limit":                                             contextStoryItemLimit,
			"query_sentinel_limit":                              queryLimit,
			"returned_count":                                    len(rows),
			"observed_count":                                    observedCount,
			"observed_count_is_lower_bound":                     lowerBound,
			"canonical_observed_count":                          len(canonicalRows),
			"repository_observed_count":                         len(repositoryRows),
			"flux_target_binding_observed_count":                len(fluxTargetBindings),
			"flux_target_binding_observed_count_is_lower_bound": fluxTargetBindingsReachedSentinel,
			"truncated":                                         lowerBound || mergedTruncated,
			"ordering": []string{
				"relationship_type_priority",
				"repo_name",
				"source_id",
				"target_id",
			},
		},
	}, nil
}

func fetchCanonicalDeploymentSourceRows(
	ctx context.Context,
	reader GraphQuery,
	workloadID string,
	limit int,
	access repositoryAccessFilter,
) ([]map[string]any, error) {
	cypher := `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)
		` + access.graphWhereClause("repo") + `
		WITH i.id as instance_id, repo.id as repo_id, repo.name as repo_name,
		     max(coalesce(rel.confidence, 0.0)) as confidence,
		     min(coalesce(rel.reason, '')) as reason
		RETURN instance_id, repo_id, repo_name, confidence, reason
		ORDER BY repo_name, instance_id, repo_id
		LIMIT $source_limit
	`
	params := access.graphParams(map[string]any{"workload_id": workloadID, "source_limit": limit})
	return reader.Run(ctx, cypher, params)
}

func fetchRepositoryDeploymentSourceRows(
	ctx context.Context,
	reader GraphQuery,
	repoID string,
	limit int,
	access repositoryAccessFilter,
) ([]map[string]any, error) {
	if strings.TrimSpace(repoID) == "" {
		return nil, nil
	}
	scopeClause := access.graphWhereClause("repo")
	if access.scoped() {
		scopeClause += " AND " + access.graphCondition("targetRepo")
	}
	cypher := `
		MATCH (targetRepo:Repository {id: $repo_id})<-[rel:DEPLOYS_FROM]-(repo:Repository)
		` + scopeClause + `
		WITH repo.id as repo_id, repo.name as repo_name,
		     max(coalesce(rel.confidence, 0.0)) as confidence,
		     min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason
		RETURN repo_id, repo_name, confidence, reason
		ORDER BY repo_name, repo_id
		LIMIT $source_limit
	`
	params := access.graphParams(map[string]any{"repo_id": repoID, "source_limit": limit})
	return reader.Run(ctx, cypher, params)
}

func sortDeploymentSources(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		left, right := rows[i], rows[j]
		leftPriority := deploymentSourceRelationshipPriority(StringVal(left, "relationship_type"))
		rightPriority := deploymentSourceRelationshipPriority(StringVal(right, "relationship_type"))
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftKey := strings.Join([]string{
			StringVal(left, "repo_name"),
			StringVal(left, "source_id"),
			StringVal(left, "target_id"),
		}, "\x00")
		rightKey := strings.Join([]string{
			StringVal(right, "repo_name"),
			StringVal(right, "source_id"),
			StringVal(right, "target_id"),
		}, "\x00")
		return leftKey < rightKey
	})
}

func deploymentSourceRelationshipPriority(relationshipType string) int {
	if relationshipType == "DEPLOYMENT_SOURCE" {
		return 0
	}
	return 1
}

func normalizedDeploymentSources(rows []map[string]any) ([]map[string]any, error) {
	sources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		confidence, err := finiteGraphFloat(row, "confidence", "deployment source")
		if err != nil {
			return nil, err
		}
		source := map[string]any{
			"repo_id":           StringVal(row, "repo_id"),
			"repo_name":         StringVal(row, "repo_name"),
			"relationship_type": StringVal(row, "relationship_type"),
			"source_id":         StringVal(row, "source_id"),
			"target_id":         StringVal(row, "target_id"),
			"confidence":        confidence,
			"reason":            StringVal(row, "reason"),
		}
		if names := StringSliceVal(row, "flux_git_repository_names"); len(names) > 0 {
			source["flux_git_repository_names"] = names
		}
		if BoolVal(row, "flux_target_bindings_saturated") {
			source["flux_target_bindings_saturated"] = true
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func deploymentSourceRowsWithEndpoints(
	rows []map[string]any,
	relationshipType string,
	targetRepoID string,
) []map[string]any {
	for _, row := range rows {
		row["relationship_type"] = relationshipType
		if relationshipType == "DEPLOYMENT_SOURCE" {
			row["source_id"] = StringVal(row, "instance_id")
			row["target_id"] = StringVal(row, "repo_id")
			continue
		}
		row["source_id"] = StringVal(row, "repo_id")
		row["target_id"] = targetRepoID
	}
	return rows
}

func deploymentSourceRowsWithCanonicalEndpoints(rows []map[string]any) []map[string]any {
	valid := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if deploymentSourceRelationshipKey(row) != "" {
			valid = append(valid, row)
		}
	}
	return valid
}

func mergeDeploymentSourceRows(
	canonicalRows []map[string]any,
	repositoryRows []map[string]any,
) []map[string]any {
	merged := make([]map[string]any, 0, len(canonicalRows)+len(repositoryRows))
	seen := make(map[string]struct{}, len(canonicalRows)+len(repositoryRows))
	appendRow := func(row map[string]any) {
		key := deploymentSourceRelationshipKey(row)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, row)
	}
	for _, row := range canonicalRows {
		appendRow(row)
	}
	for _, row := range repositoryRows {
		appendRow(row)
	}
	return merged
}

func deploymentSourceRelationshipKey(row map[string]any) string {
	relationshipType := StringVal(row, "relationship_type")
	sourceID := StringVal(row, "source_id")
	targetID := StringVal(row, "target_id")
	if relationshipType == "" || sourceID == "" || targetID == "" {
		return ""
	}
	return strings.Join([]string{relationshipType, sourceID, targetID}, "\x00")
}
