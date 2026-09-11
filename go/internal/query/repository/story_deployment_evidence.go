// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func loadRepositoryDeploymentEvidenceForOverview(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	repoID string,
) (map[string]any, error) {
	deploymentEvidence, err := LoadRepositoryDeploymentEvidence(ctx, content, repoID)
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
	if artifactCount := querycontract.IntVal(deploymentEvidence, "artifact_count"); artifactCount > 0 {
		overview["deployment_evidence_artifact_count"] = artifactCount
	}
	if toolFamilies := querycontract.ServiceDeploymentToolFamilies(deploymentEvidence); len(toolFamilies) > 0 {
		overview["deployment_tool_families"] = toolFamilies
	}
	if environments := querycontract.StringSliceVal(deploymentEvidence, "environments"); len(environments) > 0 {
		overview["deployment_evidence_environments"] = environments
	}
	if relationshipTypes := querycontract.StringSliceVal(deploymentEvidence, "relationship_types"); len(relationshipTypes) > 0 {
		overview["deployment_evidence_relationship_types"] = relationshipTypes
	}
	if evidencePaths := impacttrace.DeploymentEvidenceDeliveryPaths(deploymentEvidence); len(evidencePaths) > 0 {
		overview["delivery_paths"] = mergeRepositoryStoryDeliveryPaths(
			querycontract.MapSliceValue(overview, "delivery_paths"),
			evidencePaths,
		)
	}
	return overview
}

func repositoryDeploymentEvidenceStory(deploymentEvidence map[string]any) string {
	if len(deploymentEvidence) == 0 {
		return ""
	}
	artifactCount := querycontract.IntVal(deploymentEvidence, "artifact_count")
	if artifactCount == 0 {
		artifactCount = len(querycontract.MapSliceValue(deploymentEvidence, "artifacts"))
	}
	if artifactCount == 0 {
		return ""
	}
	return fmt.Sprintf(
		"Deployment evidence includes %d artifact(s) across tool families %s.",
		artifactCount,
		impact.JoinOrNone(querycontract.ServiceDeploymentToolFamilies(deploymentEvidence)),
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
		merged = append(merged, querycontract.CloneAnyMap(row))
	}
	return merged
}

func cloneMapRows(rows []map[string]any) []map[string]any {
	if len(rows) == 0 {
		return nil
	}
	cloned := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		cloned = append(cloned, querycontract.CloneAnyMap(row))
	}
	return cloned
}

func repositoryStoryDeliveryPathKey(row map[string]any) string {
	return impacttrace.NormalizedDeliveryPathKey(row)
}
