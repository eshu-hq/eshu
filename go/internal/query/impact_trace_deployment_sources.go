// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

func (h *ImpactHandler) fetchDeploymentSources(
	ctx context.Context,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, nil
	}
	return fetchDeploymentSourcesFromGraph(ctx, h.Neo4j, workloadID, repoID)
}

func fetchDeploymentSourcesFromGraph(
	ctx context.Context,
	reader GraphQuery,
	workloadID string,
	repoID string,
) ([]map[string]any, error) {
	canonicalRows, err := reader.Run(ctx, `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:DEPLOYMENT_SOURCE]->(repo:Repository)
		RETURN DISTINCT i.id as instance_id, repo.id as repo_id, repo.name as repo_name,
		       rel.confidence as confidence, rel.reason as reason
		ORDER BY repo_name
	`, map[string]any{"workload_id": workloadID})
	if err != nil {
		return nil, err
	}
	canonicalRows = deploymentSourceRowsWithEndpoints(canonicalRows, "DEPLOYMENT_SOURCE", "")

	repositoryRows := []map[string]any{}
	if strings.TrimSpace(repoID) != "" {
		repositoryRows, err = reader.Run(ctx, `
			MATCH (targetRepo:Repository {id: $repo_id})<-[rel:DEPLOYS_FROM]-(repo:Repository)
			RETURN DISTINCT repo.id as repo_id, repo.name as repo_name, rel.confidence as confidence,
			       coalesce(rel.reason, rel.evidence_type, 'repository_deploys_from') as reason
			ORDER BY repo_name
		`, map[string]any{"repo_id": repoID})
		if err != nil {
			return nil, err
		}
		repositoryRows = deploymentSourceRowsWithEndpoints(repositoryRows, "DEPLOYS_FROM", repoID)
	}
	return normalizedDeploymentSources(mergeDeploymentSourceRows(canonicalRows, repositoryRows)), nil
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
		repoID := StringVal(row, "repo_id")
		if _, legacyDuplicate := seen[repoID]; repoID != "" && legacyDuplicate {
			return
		}
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
