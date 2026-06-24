// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
)

func loadRepositoryDeploymentEvidenceForOverview(
	ctx context.Context,
	graph GraphQuery,
	content ContentStore,
	repoID string,
) (map[string]any, error) {
	deploymentEvidence, err := loadRepositoryDeploymentEvidence(ctx, content, repoID)
	if err != nil || len(deploymentEvidence) > 0 {
		return deploymentEvidence, err
	}
	if graph == nil {
		return nil, nil
	}
	return queryRepoDeploymentEvidence(ctx, graph, nil, map[string]any{"repo_id": repoID})
}

func attachRepositoryDeploymentEvidence(
	overview map[string]any,
	deploymentEvidence map[string]any,
) map[string]any {
	if len(deploymentEvidence) == 0 {
		return overview
	}
	if overview == nil {
		overview = map[string]any{}
	}
	overview["deployment_evidence"] = deploymentEvidence
	return overview
}

func enrichRepositoryDeploymentOverviewWithEvidence(
	overview map[string]any,
	deploymentEvidence map[string]any,
) map[string]any {
	if len(deploymentEvidence) == 0 {
		return overview
	}
	if overview == nil {
		overview = map[string]any{}
	}
	if artifactCount := IntVal(deploymentEvidence, "artifact_count"); artifactCount > 0 {
		overview["deployment_evidence_artifact_count"] = artifactCount
	}
	if toolFamilies := serviceDeploymentToolFamilies(deploymentEvidence); len(toolFamilies) > 0 {
		overview["deployment_tool_families"] = toolFamilies
	}
	if environments := StringSliceVal(deploymentEvidence, "environments"); len(environments) > 0 {
		overview["deployment_evidence_environments"] = environments
	}
	if relationshipTypes := StringSliceVal(deploymentEvidence, "relationship_types"); len(relationshipTypes) > 0 {
		overview["deployment_evidence_relationship_types"] = relationshipTypes
	}
	if evidencePaths := deploymentEvidenceDeliveryPaths(deploymentEvidence); len(evidencePaths) > 0 {
		overview["delivery_paths"] = mergeRepositoryStoryDeliveryPaths(
			mapSliceValue(overview, "delivery_paths"),
			evidencePaths,
		)
	}
	return overview
}

func repositoryDeploymentEvidenceStory(deploymentEvidence map[string]any) string {
	if len(deploymentEvidence) == 0 {
		return ""
	}
	artifactCount := IntVal(deploymentEvidence, "artifact_count")
	if artifactCount == 0 {
		artifactCount = len(mapSliceValue(deploymentEvidence, "artifacts"))
	}
	if artifactCount == 0 {
		return ""
	}
	return fmt.Sprintf(
		"Deployment evidence includes %d artifact(s) across tool families %s.",
		artifactCount,
		joinOrNone(serviceDeploymentToolFamilies(deploymentEvidence)),
	)
}

func mergeRepositoryStoryDeliveryPaths(existing []map[string]any, incoming []map[string]any) []map[string]any {
	if len(existing) == 0 {
		return cloneMapRows(incoming)
	}
	if len(incoming) == 0 {
		return cloneMapRows(existing)
	}
	merged := cloneMapRows(existing)
	seen := make(map[string]struct{}, len(existing)+len(incoming))
	for _, row := range merged {
		seen[repositoryStoryDeliveryPathKey(row)] = struct{}{}
	}
	for _, row := range incoming {
		key := repositoryStoryDeliveryPathKey(row)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, cloneAnyMap(row))
	}
	return merged
}

func cloneMapRows(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned = append(cloned, cloneAnyMap(row))
	}
	return cloned
}

func repositoryStoryDeliveryPathKey(row map[string]any) string {
	return normalizedDeliveryPathKey(row)
}
