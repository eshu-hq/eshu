// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func BuildRepositoryStoryResponse(
	repo querycontract.RepoRef,
	fileCount int,
	languages []string,
	workloads []string,
	platforms []string,
	dependencyCount int,
	infrastructureOverview map[string]any,
	semanticOverview map[string]any,
) map[string]any {
	return buildRepositoryStoryResponseWithCoverage(
		repo,
		fileCount,
		languages,
		workloads,
		platforms,
		dependencyCount,
		infrastructureOverview,
		semanticOverview,
		nil,
		nil,
		false,
	)
}

// buildRepositoryStoryResponseWithCoverage builds the repository story
// response. extraLimitations carries limitation reasons the caller already
// discovered before this function runs (for example
// infrastructureReadDegradedReason, #5764) -- they are merged into the
// computed limitations slice before attachAnswerMetadata derives
// answer_metadata.partial_reasons, so a degraded auxiliary read is visible in
// both the top-level limitations field and the answer metadata. storyRowsTruncated
// (P1 review follow-up to #5764) reports whether the workload_names/
// platform_types/languages graph reads landed past
// repositoryStoryStringRowLimit, OR'd by the caller (repository.go, P3 review
// follow-up) with the infrastructure panel's own truncation so either bound
// being exceeded sets the response's top-level "truncated" field; this makes
// attachAnswerMetadata's BuildAnswerMetadata (which reads data["truncated"]
// directly, not the limitations slice) stop answering
// answer_metadata.truncated=false when either read was actually clipped.
func BuildRepositoryStoryResponseWithCoverage(
	repo querycontract.RepoRef,
	fileCount int,
	languages []string,
	workloads []string,
	platforms []string,
	dependencyCount int,
	infrastructureOverview map[string]any,
	semanticOverview map[string]any,
	coverageSummary map[string]any,
	extraLimitations []string,
	storyRowsTruncated bool,
) map[string]any {
	filteredLanguages := querycontract.NonEmptyStrings(languages)
	filteredPlatforms := querycontract.NonEmptyStrings(platforms)
	filteredWorkloads := querycontract.NonEmptyStrings(workloads)
	infraFamilies := querycontract.StringSliceMapValue(infrastructureOverview, "families")
	if len(infraFamilies) == 0 {
		infraFamilies = querycontract.StringSliceMapValue(semanticOverview, "infrastructure_families")
	}
	relationshipOverview := querycontract.MapValue(infrastructureOverview, "relationship_overview")
	deploymentEvidence := querycontract.MapValue(infrastructureOverview, "deployment_evidence")
	ciCDEvidence := querycontract.MapValue(infrastructureOverview, "ci_cd_evidence")
	semanticStory := buildRepositorySemanticStory(semanticOverview)
	deploymentOverview := BuildRepositoryDeploymentOverview(
		filteredWorkloads,
		filteredPlatforms,
		infraFamilies,
		infrastructureOverview,
	)
	deploymentOverview = enrichRepositoryDeploymentOverviewWithEvidence(deploymentOverview, deploymentEvidence)
	coverageSummary = repositoryStoryCoverageSummaryOrDefault(coverageSummary)
	limitations := repositoryStoryCoverageLimitations(coverageSummary)
	if !repositoryDeploymentSurfaceKnown(filteredPlatforms, filteredWorkloads, deploymentOverview) {
		limitations = append(limitations, "deployment_surface_unknown")
	}
	if len(filteredWorkloads) == 0 {
		limitations = append(limitations, "workload_surface_unknown")
	}
	limitations = appendNonEmptyLimitations(limitations, extraLimitations)

	response := map[string]any{
		"repository": repo,
		"subject": map[string]any{
			"type": "repository",
			"id":   repo.ID,
			"name": repo.Name,
		},
		"story": buildRepositoryStory(
			repo,
			fileCount,
			filteredLanguages,
			filteredWorkloads,
			filteredPlatforms,
			infraFamilies,
			semanticStory,
		),
		"story_sections": []map[string]any{
			{
				"title":   "codebase",
				"summary": fmt.Sprintf("%d indexed file(s) across %d language family(s)", fileCount, len(filteredLanguages)),
			},
			{
				"title":   "deployment",
				"summary": fmt.Sprintf("%d workload(s) and %d platform signal(s)", len(filteredWorkloads), len(filteredPlatforms)),
			},
		},
		"deployment_overview": deploymentOverview,
		"gitops_overview": map[string]any{
			"enabled":          containsRepositoryGitOpsSignals(filteredPlatforms, infraFamilies, deploymentOverview),
			"tool_families":    repositoryGitOpsToolFamilies(filteredPlatforms, infraFamilies, deploymentOverview),
			"observed_targets": filteredWorkloads,
		},
		"documentation_overview": map[string]any{
			"repo_slug":           repo.RepoSlug,
			"remote_url":          repo.RemoteURL,
			"has_remote":          repo.HasRemote,
			"local_path_present":  repo.LocalPath != "",
			"portable_identifier": repo.ID,
		},
		"support_overview": map[string]any{
			"dependency_count": dependencyCount,
			"language_count":   len(filteredLanguages),
			"languages":        filteredLanguages,
		},
		"coverage_summary": coverageSummary,
		"limitations":      limitations,
		"truncated":        storyRowsTruncated,
		"drilldowns": map[string]any{
			"context_path":  "/api/v0/repositories/" + repo.ID + "/context",
			"stats_path":    "/api/v0/repositories/" + repo.ID + "/stats",
			"coverage_path": "/api/v0/repositories/" + repo.ID + "/coverage",
		},
	}

	storySections := response["story_sections"].([]map[string]any)
	if relationshipStory := querycontract.StringVal(relationshipOverview, "story"); relationshipStory != "" {
		storySections = append(storySections, map[string]any{
			"title":   "relationships",
			"summary": relationshipStory,
		})
		response["story"] = response["story"].(string) + " " + relationshipStory
		response["relationship_overview"] = relationshipOverview
	}
	if len(semanticOverview) > 0 {
		storySections = append(storySections, map[string]any{
			"title":   "semantics",
			"summary": semanticStory,
		})
		response["semantic_overview"] = semanticOverview
	}
	if len(infrastructureOverview) > 0 {
		response["infrastructure_overview"] = infrastructureOverview
	}
	if len(ciCDEvidence) > 0 {
		response["ci_cd_evidence"] = ciCDEvidence
		storySections = append(storySections, map[string]any{
			"title":   "ci_cd",
			"summary": querycontract.CicdEvidenceStorySummary(ciCDEvidence),
		})
	}
	if deploymentOverview, ok := response["deployment_overview"].(map[string]any); ok {
		if evidenceStory := repositoryDeploymentEvidenceStory(deploymentEvidence); evidenceStory != "" {
			response["story"] = response["story"].(string) + " " + evidenceStory
			storySections = append(storySections, map[string]any{
				"title":   "deployment_evidence",
				"summary": evidenceStory,
			})
		}
		if deliveryFamilyStory := querycontract.StringSliceMapValue(deploymentOverview, "delivery_family_story"); len(deliveryFamilyStory) > 0 {
			response["story"] = response["story"].(string) + " " + strings.Join(deliveryFamilyStory, " ")
		}
		topologyStory := querycontract.StringSliceMapValue(deploymentOverview, "topology_story")
		directStory := focusedDeploymentStory(topologyStory)
		deploymentOverview["direct_story"] = directStory
		if len(topologyStory) > 0 && len(directStory) != len(topologyStory) {
			deploymentOverview["trace_limitations"] = map[string]any{
				"omitted_sections": []string{"shared_config_paths"},
				"reason":           "Keep the repository story focused on direct deployment evidence.",
			}
		}
	}
	storySections = append(storySections, map[string]any{
		"title":   "support",
		"summary": fmt.Sprintf("%d dependency link(s) and remote=%t", dependencyCount, repo.HasRemote),
	})
	response["story_sections"] = storySections
	return querycontract.AttachAnswerMetadata(response)
}

func repositoryDeploymentSurfaceKnown(
	platforms []string,
	workloads []string,
	deploymentOverview map[string]any,
) bool {
	if len(platforms) > 0 {
		return true
	}
	if querycontract.IntVal(deploymentOverview, "deployment_evidence_artifact_count") > 0 {
		return true
	}
	for _, key := range []string{
		"delivery_paths",
		"delivery_workflows",
		"delivery_family_paths",
		"delivery_family_story",
		"topology_story",
		"direct_story",
		"shared_config_paths",
	} {
		if mapValueHasRows(deploymentOverview, key) {
			return true
		}
	}
	return false
}

func buildRepositoryStory(
	repo querycontract.RepoRef,
	fileCount int,
	languages []string,
	workloads []string,
	platforms []string,
	infraFamilies []string,
	semanticStory string,
) string {
	parts := []string{
		fmt.Sprintf("Repository %s contains %d indexed files.", repo.Name, fileCount),
	}

	if len(languages) > 0 {
		parts = append(parts, fmt.Sprintf("Languages: %s.", strings.Join(languages, ", ")))
	}
	if len(workloads) > 0 {
		parts = append(parts, fmt.Sprintf("Defines %d workload(s): %s.", len(workloads), strings.Join(workloads, ", ")))
	}
	if len(platforms) > 0 {
		parts = append(parts, fmt.Sprintf("Runs on platform signal(s): %s.", strings.Join(platforms, ", ")))
	}
	if len(infraFamilies) > 0 {
		parts = append(parts, fmt.Sprintf("Infrastructure families present: %s.", strings.Join(infraFamilies, ", ")))
	}
	if semanticStory != "" {
		parts = append(parts, semanticStory)
	}
	if repo.HasRemote && repo.RemoteURL != "" {
		parts = append(parts, fmt.Sprintf("Remote URL: %s.", repo.RemoteURL))
	}

	return strings.Join(parts, " ")
}

func focusedDeploymentStory(lines []string) []string {
	focused := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		focused = append(focused, trimmed)
	}
	return focused
}

// appendNonEmptyLimitations appends every non-empty reason from extra onto
// limitations, preserving order. Extracted from
// buildRepositoryStoryResponseWithCoverage to keep that function under the
// repo's funlen budget.
func appendNonEmptyLimitations(limitations []string, extra []string) []string {
	for _, reason := range extra {
		if reason != "" {
			limitations = append(limitations, reason)
		}
	}
	return limitations
}

// containsGitOpsSignals reports whether any platform or infrastructure-family
// label names a GitOps delivery tool. Flux is deliberately not one of the
// matched literals: no parser or collector emits a "flux_kustomization" or
// "flux_helmrelease" platform/family value today (issue #5342 confirmed the
// Flux Kustomization parse path only captures typed evidence, not a platform
// label), so those cases were dead and are not restored until Flux modeling
// lands a real emitter (#5360).
func ContainsGitOpsSignals(platforms []string, infraFamilies []string) bool {
	for _, platform := range mergeStringSets(platforms, infraFamilies) {
		switch platform {
		case "argocd_application", "argocd_applicationset", "argocd", "helm", "kustomize":
			return true
		}
	}
	return false
}

func containsRepositoryGitOpsSignals(platforms []string, infraFamilies []string, deploymentOverview map[string]any) bool {
	if containsGitOpsSignals(platforms, infraFamilies) {
		return true
	}
	for _, row := range querycontract.MapSliceValue(deploymentOverview, "delivery_family_paths") {
		if querycontract.StringVal(row, "family") == "gitops" {
			return true
		}
	}
	return false
}

func repositoryGitOpsToolFamilies(platforms []string, infraFamilies []string, deploymentOverview map[string]any) []string {
	toolFamilies := mergeStringSets(platforms, infraFamilies)
	for _, row := range querycontract.MapSliceValue(deploymentOverview, "delivery_family_paths") {
		if toolFamily := querycontract.StringVal(row, "tool_family"); toolFamily != "" {
			toolFamilies = mergeStringSets(toolFamilies, []string{toolFamily})
		}
	}
	return toolFamilies
}

func mapValueHasRows(value map[string]any, key string) bool {
	if len(value) == 0 {
		return false
	}
	if len(querycontract.MapSliceValue(value, key)) > 0 {
		return true
	}
	return len(querycontract.StringSliceMapValue(value, key)) > 0
}

// mergeStringSets merges two string sets in order. The implementation moved
// to querycontract for #6060; this wrapper keeps package callers unchanged.
func mergeStringSets(left []string, right []string) []string {
	return querycontract.MergeStringSets(left, right)
}

// stringSliceMapValue extracts a []string from a map value. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.

// buildRepositoryStoryResponseWithCoverage keeps the in-package spelling after
// the #6060 export; root tests name BuildRepositoryStoryResponseWithCoverage.
func buildRepositoryStoryResponseWithCoverage(
	repo querycontract.RepoRef,
	fileCount int,
	languages []string,
	workloads []string,
	platforms []string,
	dependencyCount int,
	infrastructureOverview map[string]any,
	semanticOverview map[string]any,
	coverageSummary map[string]any,
	extraLimitations []string,
	storyRowsTruncated bool,
) map[string]any {
	return BuildRepositoryStoryResponseWithCoverage(repo, fileCount, languages, workloads, platforms, dependencyCount, infrastructureOverview, semanticOverview, coverageSummary, extraLimitations, storyRowsTruncated)
}

// buildRepositoryStoryResponse keeps the in-package spelling after the #6060
// export; root tests name BuildRepositoryStoryResponse.
func buildRepositoryStoryResponse(
	repo querycontract.RepoRef,
	fileCount int,
	languages []string,
	workloads []string,
	platforms []string,
	dependencyCount int,
	infrastructureOverview map[string]any,
	semanticOverview map[string]any,
) map[string]any {
	return BuildRepositoryStoryResponse(repo, fileCount, languages, workloads, platforms, dependencyCount, infrastructureOverview, semanticOverview)
}

// containsGitOpsSignals keeps the in-package spelling after the #6060 export;
// root tests name ContainsGitOpsSignals.
func containsGitOpsSignals(platforms []string, infraFamilies []string) bool {
	return ContainsGitOpsSignals(platforms, infraFamilies)
}
