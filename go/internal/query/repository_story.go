package query

import (
	"fmt"
	"strings"
)

func buildRepositoryStoryResponse(
	repo RepoRef,
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
	)
}

func buildRepositoryStoryResponseWithCoverage(
	repo RepoRef,
	fileCount int,
	languages []string,
	workloads []string,
	platforms []string,
	dependencyCount int,
	infrastructureOverview map[string]any,
	semanticOverview map[string]any,
	contentCoverage *RepositoryContentCoverage,
) map[string]any {
	filteredLanguages := nonEmptyStrings(languages)
	filteredPlatforms := nonEmptyStrings(platforms)
	filteredWorkloads := nonEmptyStrings(workloads)
	infraFamilies := stringSliceMapValue(infrastructureOverview, "families")
	if len(infraFamilies) == 0 {
		infraFamilies = stringSliceMapValue(semanticOverview, "infrastructure_families")
	}
	relationshipOverview := mapValue(infrastructureOverview, "relationship_overview")
	semanticStory := buildRepositorySemanticStory(semanticOverview)
	deploymentOverview := BuildRepositoryDeploymentOverview(
		filteredWorkloads,
		filteredPlatforms,
		infraFamilies,
		infrastructureOverview,
	)
	coverageSummary := repositoryStoryCoverageSummary(contentCoverage)
	limitations := repositoryStoryLimitations(coverageSummary)
	if !repositoryDeploymentSurfaceKnown(filteredPlatforms, filteredWorkloads, deploymentOverview) {
		limitations = append(limitations, "deployment_surface_unknown")
	}
	if len(filteredWorkloads) == 0 {
		limitations = append(limitations, "workload_surface_unknown")
	}

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
		"drilldowns": map[string]any{
			"context_path":  "/api/v0/repositories/" + repo.ID + "/context",
			"stats_path":    "/api/v0/repositories/" + repo.ID + "/stats",
			"coverage_path": "/api/v0/repositories/" + repo.ID + "/coverage",
		},
	}

	storySections := response["story_sections"].([]map[string]any)
	if relationshipStory := StringVal(relationshipOverview, "story"); relationshipStory != "" {
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
	if deploymentOverview, ok := response["deployment_overview"].(map[string]any); ok {
		if deliveryFamilyStory := stringSliceMapValue(deploymentOverview, "delivery_family_story"); len(deliveryFamilyStory) > 0 {
			response["story"] = response["story"].(string) + " " + strings.Join(deliveryFamilyStory, " ")
		}
		topologyStory := stringSliceMapValue(deploymentOverview, "topology_story")
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
	return response
}

func repositoryStoryCoverageSummary(contentCoverage *RepositoryContentCoverage) map[string]any {
	if contentCoverage == nil || !contentCoverage.Available || !repositoryStatsCoverageHasEvidence(*contentCoverage) {
		return map[string]any{
			"status":           "unknown",
			"reason":           "content_store_coverage_missing",
			"counts_available": false,
			"missing_evidence": []string{"content_store_coverage"},
		}
	}
	summary := map[string]any{
		"status":                 "available",
		"source_backend":         "content_store",
		"query_shape":            repositoryStatsContentCoverageShape,
		"counts_available":       true,
		"entity_types_available": true,
		"whole_graph_traversal":  false,
		"missing_evidence":       []string{},
		"file_count":             contentCoverage.FileCount,
		"entity_count":           contentCoverage.EntityCount,
		"language_count":         len(repositoryStatsLanguageNames(contentCoverage.Languages)),
		"entity_type_count":      len(repositoryStatsEntityTypeNames(contentCoverage.EntityTypes)),
	}
	if latest := latestCoverageTimestamp(contentCoverage.FileIndexedAt, contentCoverage.EntityIndexedAt); !latest.IsZero() {
		summary["content_last_indexed_at"] = formatCoverageTimestamp(latest)
	}
	return summary
}

func repositoryStoryLimitations(coverageSummary map[string]any) []string {
	if StringVal(coverageSummary, "status") == "available" {
		return []string{}
	}
	return []string{"coverage_not_computed"}
}

func repositoryDeploymentSurfaceKnown(
	platforms []string,
	workloads []string,
	deploymentOverview map[string]any,
) bool {
	if len(platforms) > 0 {
		return true
	}
	if len(workloads) == 0 {
		return false
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
	repo RepoRef,
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

func nonEmptyStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}

func containsGitOpsSignals(platforms []string, infraFamilies []string) bool {
	for _, platform := range mergeStringSets(platforms, infraFamilies) {
		switch platform {
		case "argocd_application", "argocd_applicationset", "flux_kustomization", "flux_helmrelease",
			"argocd", "helm", "kustomize":
			return true
		}
	}
	return false
}

func containsRepositoryGitOpsSignals(platforms []string, infraFamilies []string, deploymentOverview map[string]any) bool {
	if containsGitOpsSignals(platforms, infraFamilies) {
		return true
	}
	for _, row := range mapSliceValue(deploymentOverview, "delivery_family_paths") {
		if StringVal(row, "family") == "gitops" {
			return true
		}
	}
	return false
}

func repositoryGitOpsToolFamilies(platforms []string, infraFamilies []string, deploymentOverview map[string]any) []string {
	toolFamilies := mergeStringSets(platforms, infraFamilies)
	for _, row := range mapSliceValue(deploymentOverview, "delivery_family_paths") {
		if toolFamily := StringVal(row, "tool_family"); toolFamily != "" {
			toolFamilies = mergeStringSets(toolFamilies, []string{toolFamily})
		}
	}
	return toolFamilies
}

func mapValueHasRows(value map[string]any, key string) bool {
	if len(value) == 0 {
		return false
	}
	if len(mapSliceValue(value, key)) > 0 {
		return true
	}
	return len(stringSliceMapValue(value, key)) > 0
}

func mergeStringSets(left []string, right []string) []string {
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(left)+len(right))
	for _, item := range append(append([]string{}, left...), right...) {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		merged = append(merged, item)
	}
	return merged
}

func stringSliceMapValue(value map[string]any, key string) []string {
	if len(value) == 0 {
		return nil
	}
	raw, ok := value[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return nonEmptyStrings(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok && text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}
