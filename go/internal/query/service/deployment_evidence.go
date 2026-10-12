// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package service

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

// serviceDeploymentEvidenceLoad is the outcome of the service routes'
// content fallback for deployment evidence (#7951): the evidence, the read
// counts of the deployment-artifact load, whether the fallback listed the
// service repository at all, and whether that list was cut at the window.
type serviceDeploymentEvidenceLoad struct {
	evidence            map[string]any
	stats               artifacts.ConfigArtifactReadStats
	ran                 bool
	anchorListTruncated bool
}

// loadServiceDeploymentEvidence returns the graph deployment evidence when
// the workload already carries it. Otherwise it lists the service
// repository's files with a one-row sentinel, clips the list to the 5,000-file
// window, and runs the deployment-artifact load over it. Every bound that bit
// is appended to workloadContext["limitations"]: the anchor reason for a cut
// service-repository list, and the candidate budget and related-list reasons
// of the load (#7951). The service story folds them into
// result_limits.truncated.
func loadServiceDeploymentEvidence(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	workloadContext map[string]any,
) (serviceDeploymentEvidenceLoad, error) {
	var load serviceDeploymentEvidenceLoad
	if content == nil || len(workloadContext) == 0 {
		return load, nil
	}
	if graphEvidence := querycontract.MapValue(workloadContext, "deployment_evidence"); len(graphEvidence) > 0 {
		load.evidence = graphEvidence
		return load, nil
	}

	repoID := querycontract.SafeStr(workloadContext, "repo_id")
	repoName := querycontract.SafeStr(workloadContext, "repo_name")
	if repoID == "" || repoName == "" {
		return load, nil
	}

	const fileLimit = querycontract.RepositorySemanticEntityLimit
	files, err := content.ListRepoFiles(ctx, repoID, fileLimit+1)
	if err != nil {
		return load, err
	}
	load.ran = true
	if len(files) > fileLimit {
		files = files[:fileLimit]
		load.anchorListTruncated = true
	}
	if files == nil {
		files = []querycontract.FileContent{}
	}

	overview := repository.BuildRepositoryInfrastructureOverview(
		querycontract.MapSliceValue(workloadContext, "infrastructure"),
		files,
	)
	overview, load.stats, err = artifacts.LoadDeploymentArtifactOverviewWithStats(
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
		return load, err
	}
	bounds := load.stats.BoundReasons()
	if load.anchorListTruncated {
		bounds = append([]string{artifacts.AnchorFileListTruncatedReason}, bounds...)
	}
	if len(bounds) > 0 {
		workloadContext["limitations"] = append(querycontract.StringSliceVal(workloadContext, "limitations"), bounds...)
	}
	if relationshipOverview := repository.BuildRepositoryRelationshipOverview(querycontract.MapSliceValue(workloadContext, "dependencies")); relationshipOverview != nil {
		if overview == nil {
			overview = map[string]any{}
		}
		overview["relationship_overview"] = relationshipOverview
	}
	load.evidence = buildServiceDeploymentEvidenceFromOverview(overview)
	return load, nil
}

// deploymentEvidenceStageAttrs are the deployment_evidence stage-line
// attributes: whether evidence was found and, when the content fallback ran,
// the read and candidate counts of its deployment-artifact load plus
// anchor_list_truncated (0 or 1) for the service repository's own list.
func deploymentEvidenceStageAttrs(load serviceDeploymentEvidenceLoad) []slog.Attr {
	attrs := []slog.Attr{slog.Bool("has_result", len(load.evidence) > 0)}
	if !load.ran {
		return attrs
	}
	attrs = append(attrs, repository.DeploymentArtifactsReadCountAttrs(load.stats)...)
	anchorListTruncated := 0
	if load.anchorListTruncated {
		anchorListTruncated = 1
	}
	return append(attrs, slog.Int("anchor_list_truncated", anchorListTruncated))
}

// deploymentArtifactsBoundHit reports whether the workload limitations carry
// a deployment-artifact bound reason (#7951), which makes the service story
// a lower bound.
func deploymentArtifactsBoundHit(workloadContext map[string]any) bool {
	return querycontract.HasDeploymentArtifactBound(querycontract.StringSliceVal(workloadContext, "limitations"))
}

func queryServiceGraphDeploymentEvidence(ctx context.Context, graph querycontract.GraphQuery, content querycontract.ContentStore, repoID string) (map[string]any, error) {
	if graph == nil || strings.TrimSpace(repoID) == "" {
		return nil, nil
	}
	return repository.QueryRepoDeploymentEvidence(ctx, graph, content, map[string]any{"repo_id": repoID})
}

// queryServiceGraphAPISurface reads the repository API surface for a service
// and reports whether the graph read failed (#6810), so the caller can name
// the degradation instead of rendering an absent surface as "none".
func queryServiceGraphAPISurface(ctx context.Context, graph querycontract.GraphQuery, repoID string) (map[string]any, bool) {
	if graph == nil || strings.TrimSpace(repoID) == "" {
		return nil, false
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
