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

func (h *ImpactHandler) fetchDeploymentSources(
	ctx context.Context,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	result, err := h.fetchDeploymentSourceResult(ctx, workloadID, repoID)
	return result.rows, err
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
	canonicalRows, err := fetchCanonicalDeploymentSourceRows(ctx, reader, workloadID, queryLimit)
	if err != nil {
		return deploymentSourceResult{}, err
	}
	canonicalRows = deploymentSourceRowsWithEndpoints(canonicalRows, "DEPLOYMENT_SOURCE", "")

	repositoryRows, err := fetchRepositoryDeploymentSourceRows(ctx, reader, repoID, queryLimit)
	if err != nil {
		return deploymentSourceResult{}, err
	}
	repositoryRows = deploymentSourceRowsWithEndpoints(repositoryRows, "DEPLOYS_FROM", repoID)
	merged := normalizedDeploymentSources(mergeDeploymentSourceRows(canonicalRows, repositoryRows))
	sortDeploymentSources(merged)
	observedCount := len(merged)
	rows, mergedTruncated := capMapRows(merged, contextStoryItemLimit)
	lowerBound := len(canonicalRows) >= queryLimit || len(repositoryRows) >= queryLimit
	return deploymentSourceResult{
		rows: rows,
		limits: map[string]any{
			"limit":                         contextStoryItemLimit,
			"query_sentinel_limit":          queryLimit,
			"returned_count":                len(rows),
			"observed_count":                observedCount,
			"observed_count_is_lower_bound": lowerBound,
			"canonical_observed_count":      len(canonicalRows),
			"repository_observed_count":     len(repositoryRows),
			"truncated":                     lowerBound || mergedTruncated,
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
) ([]map[string]any, error) {
	return reader.Run(ctx, `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)
		WITH i.id as instance_id, repo.id as repo_id, repo.name as repo_name,
		     max(coalesce(rel.confidence, 0.0)) as confidence,
		     min(coalesce(rel.reason, '')) as reason
		RETURN instance_id, repo_id, repo_name, confidence, reason
		ORDER BY repo_name, instance_id, repo_id
		LIMIT $source_limit
	`, map[string]any{"workload_id": workloadID, "source_limit": limit})
}

func fetchRepositoryDeploymentSourceRows(
	ctx context.Context,
	reader GraphQuery,
	repoID string,
	limit int,
) ([]map[string]any, error) {
	if strings.TrimSpace(repoID) == "" {
		return nil, nil
	}
	return reader.Run(ctx, `
		MATCH (targetRepo:Repository {id: $repo_id})<-[rel:DEPLOYS_FROM]-(repo:Repository)
		WITH repo.id as repo_id, repo.name as repo_name,
		     max(coalesce(rel.confidence, 0.0)) as confidence,
		     min(coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from')) as reason
		RETURN repo_id, repo_name, confidence, reason
		ORDER BY repo_name, repo_id
		LIMIT $source_limit
	`, map[string]any{"repo_id": repoID, "source_limit": limit})
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

func normalizedDeploymentSources(rows []map[string]any) []map[string]any {
	sources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		sources = append(sources, map[string]any{
			"repo_id":           StringVal(row, "repo_id"),
			"repo_name":         StringVal(row, "repo_name"),
			"relationship_type": StringVal(row, "relationship_type"),
			"source_id":         StringVal(row, "source_id"),
			"target_id":         StringVal(row, "target_id"),
			"confidence":        floatVal(row, "confidence"),
			"reason":            StringVal(row, "reason"),
		})
	}
	return sources
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
	if relationshipType == "DEPLOYMENT_SOURCE" && sourceID == "" {
		return firstNonEmptyString(StringVal(row, "repo_id"), StringVal(row, "repo_name"))
	}
	key := strings.Join([]string{relationshipType, sourceID, targetID}, "\x00")
	if key == "\x00\x00" {
		return firstNonEmptyString(StringVal(row, "repo_id"), StringVal(row, "repo_name"))
	}
	return key
}
