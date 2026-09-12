// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

func loadServiceDeploymentEvidence(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	workloadContext map[string]any,
) (map[string]any, error) {
	if content == nil || len(workloadContext) == 0 {
		return nil, nil
	}
	if graphEvidence := querycontract.MapValue(workloadContext, "deployment_evidence"); len(graphEvidence) > 0 {
		return graphEvidence, nil
	}

	repoID := querycontract.SafeStr(workloadContext, "repo_id")
	repoName := querycontract.SafeStr(workloadContext, "repo_name")
	if repoID == "" || repoName == "" {
		return nil, nil
	}

	files, err := content.ListRepoFiles(ctx, repoID, querycontract.RepositorySemanticEntityLimit)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = []querycontract.FileContent{}
	}

	overview := repository.BuildRepositoryInfrastructureOverview(
		querycontract.MapSliceValue(workloadContext, "infrastructure"),
		files,
	)
	overview, err = artifacts.LoadDeploymentArtifactOverview(
		ctx,
		graph,
		content,
		repoID,
		repoName,
		files,
		querycontract.MapSliceValue(workloadContext, "infrastructure"),
		overview,
	)
	if err != nil {
		return nil, err
	}
	if relationshipOverview := repository.BuildRepositoryRelationshipOverview(querycontract.MapSliceValue(workloadContext, "dependencies")); relationshipOverview != nil {
		if overview == nil {
			overview = map[string]any{}
		}
		overview["relationship_overview"] = relationshipOverview
	}
	return buildServiceDeploymentEvidenceFromOverview(overview), nil
}

func queryServiceGraphDeploymentEvidence(ctx context.Context, graph querycontract.GraphQuery, content querycontract.ContentStore, repoID string) (map[string]any, error) {
	if graph == nil || strings.TrimSpace(repoID) == "" {
		return nil, nil
	}
	return repository.QueryRepoDeploymentEvidence(ctx, graph, content, map[string]any{"repo_id": repoID})
}

func queryServiceGraphAPISurface(ctx context.Context, graph querycontract.GraphQuery, repoID string) map[string]any {
	if graph == nil || strings.TrimSpace(repoID) == "" {
		return nil
	}
	return repository.QueryRepoAPISurface(ctx, graph, map[string]any{"repo_id": repoID})
}

func mergeServiceDeploymentEvidence(contentEvidence map[string]any, graphEvidence map[string]any) map[string]any {
	if len(contentEvidence) == 0 {
		return graphEvidence
	}
	if len(graphEvidence) == 0 {
		return contentEvidence
	}
	merged := cloneAnyMap(contentEvidence)
	for key, value := range graphEvidence {
		merged[key] = value
	}
	return merged
}

func buildServiceDeploymentEvidenceFromOverview(overview map[string]any) map[string]any {
	if len(overview) == 0 {
		return nil
	}

	evidence := map[string]any{}
	repositoryOverview := repository.BuildRepositoryDeploymentOverview(
		nil,
		nil,
		collectServiceDeploymentToolFamilies(overview),
		overview,
	)
	for _, key := range []string{
		"deployment_artifacts",
		"shared_config_paths",
		"delivery_paths",
		"delivery_family_paths",
		"delivery_family_story",
		"delivery_workflows",
		"topology_story",
	} {
		if value, ok := repositoryOverview[key]; ok && value != nil {
			evidence[key] = value
		}
	}
	if toolFamilies := collectServiceDeploymentToolFamilies(overview); len(toolFamilies) > 0 {
		evidence["tool_families"] = toolFamilies
	}
	if relationshipOverview := querycontract.MapValue(overview, "relationship_overview"); len(relationshipOverview) > 0 {
		evidence["relationship_overview"] = relationshipOverview
	}
	return evidence
}

func collectServiceDeploymentToolFamilies(overview map[string]any) []string {
	families := map[string]struct{}{}
	for _, family := range querycontract.StringSliceMapValue(overview, "families") {
		if trimmed := strings.TrimSpace(family); trimmed != "" {
			families[trimmed] = struct{}{}
		}
	}
	for _, row := range querycontract.MapSliceValue(querycontract.MapValue(overview, "deployment_artifacts"), "controller_artifacts") {
		switch strings.TrimSpace(querycontract.StringVal(row, "controller_kind")) {
		case "jenkins_pipeline":
			families["jenkins"] = struct{}{}
		}
	}
	for _, row := range querycontract.MapSliceValue(overview, "delivery_family_paths") {
		if toolFamily := strings.TrimSpace(querycontract.StringVal(row, "tool_family")); toolFamily != "" {
			families[toolFamily] = struct{}{}
		}
	}

	sorted := make([]string, 0, len(families))
	for family := range families {
		sorted = append(sorted, family)
	}
	sort.Strings(sorted)
	return sorted
}

// cloneAnyMap returns a shallow copy of src. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func cloneAnyMap(src map[string]any) map[string]any {
	return querycontract.CloneAnyMap(src)
}
