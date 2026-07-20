// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

var controllerEntityTypes = map[string]string{
	"ArgoCDApplication":    "argocd_application",
	"ArgoCDApplicationSet": "argocd_applicationset",
}

func (h *ImpactHandler) fetchControllerEntities(
	ctx context.Context,
	deploymentSources []map[string]any,
) ([]map[string]any, error) {
	if h == nil || h.Content == nil || len(deploymentSources) == 0 {
		return nil, nil
	}

	repoIDs := uniqueNonEmptyRepoIDs(deploymentSources)
	controllers := make([]map[string]any, 0, len(repoIDs))
	for _, repoID := range repoIDs {
		entities, err := h.Content.ListRepoEntities(ctx, repoID, 500)
		if err != nil {
			return nil, fmt.Errorf("list controller entities for %s: %w", repoID, err)
		}
		for _, entity := range entities {
			controller, ok := buildDeploymentSourceControllerEntity(entity)
			if !ok {
				continue
			}
			controllers = append(controllers, controller)
		}
	}
	return controllers, nil
}

func (h *ImpactHandler) fetchDeploymentSourceGitOps(
	ctx context.Context,
	serviceName string,
	deploymentSources []map[string]any,
) ([]map[string]any, []map[string]any, []string, bool, error) {
	result, err := h.fetchDeploymentSourceGitOpsResult(ctx, serviceName, deploymentSources)
	return result.controllers, result.k8sResources, result.imageRefs, result.k8sObservedCountIsLowerBound, err
}

type deploymentSourceGitOpsResult struct {
	controllers                  []map[string]any
	controllerLimits             map[string]any
	imageRefs                    []string
	k8sObservedCountIsLowerBound bool
	k8sResources                 []map[string]any
}

func (h *ImpactHandler) fetchDeploymentSourceGitOpsResult(
	ctx context.Context,
	serviceName string,
	deploymentSources []map[string]any,
) (deploymentSourceGitOpsResult, error) {
	if h == nil || h.Content == nil || len(deploymentSources) == 0 {
		return deploymentSourceGitOpsResult{}, nil
	}

	repoIDs := uniqueNonEmptyRepoIDs(deploymentSources)
	entities := make([]EntityContent, 0, len(repoIDs)*8)
	observedCountIsLowerBound := false
	for _, repoID := range repoIDs {
		rows, err := h.Content.ListRepoEntities(ctx, repoID, repositorySemanticEntityLimit+1)
		if err != nil {
			return deploymentSourceGitOpsResult{}, fmt.Errorf("list deployment source entities for %s: %w", repoID, err)
		}
		if len(rows) >= repositorySemanticEntityLimit+1 {
			observedCountIsLowerBound = true
		}
		if len(rows) > repositorySemanticEntityLimit {
			rows = rows[:repositorySemanticEntityLimit]
		}
		entities = append(entities, rows...)
	}

	observedControllers := selectRelevantDeploymentSourceControllers(serviceName, deploymentSources, entities)
	controllers, controllersTruncated := capMapRows(observedControllers, serviceStoryItemLimit)
	k8sResources, imageRefs := collectDeploymentSourceK8sResources(controllers, entities)
	controllerObservedCountIsLowerBound := observedCountIsLowerBound
	controllerTruncated := controllerObservedCountIsLowerBound || controllersTruncated
	return deploymentSourceGitOpsResult{
		controllers: controllers,
		controllerLimits: map[string]any{
			"limit":                         serviceStoryItemLimit,
			"source_query_sentinel_limit":   repositorySemanticEntityLimit + 1,
			"returned_count":                len(controllers),
			"observed_count":                len(observedControllers),
			"observed_count_is_lower_bound": controllerObservedCountIsLowerBound,
			"truncated":                     controllerTruncated,
			"ordering":                      []string{"repo_id", "relative_path", "entity_id"},
		},
		imageRefs:                    imageRefs,
		k8sObservedCountIsLowerBound: controllerTruncated,
		k8sResources:                 k8sResources,
	}, nil
}

func buildControllerOverview(
	platforms []string,
	platformKinds []string,
	controllerEntities []map[string]any,
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerLimits map[string]any,
) map[string]any {
	controllerNames := controllerEntityNames(controllerEntities)
	controllerKinds := controllerOverviewKinds(controllerEntities, platformKinds)
	controllerKinds = mergeControllerKinds(
		controllerKinds,
		deploymentTraceEvidenceControllerFamilies(deploymentSources, deploymentEvidence, controllerEntities),
	)
	controllerCount := len(controllerNames)
	if controllerCount == 0 {
		controllerCount = len(controllerKinds)
	}
	overview := map[string]any{
		"controller_count": controllerCount,
		"controller_kinds": controllerKinds,
	}
	if len(controllerNames) > 0 {
		overview["controllers"] = controllerNames
	}
	if len(platforms) > 0 {
		overview["observed_targets"] = platforms
	}
	if len(controllerEntities) > 0 {
		overview["entities"] = controllerEntities
	}
	if len(controllerLimits) > 0 {
		overview["entity_limits"] = controllerLimits
	}
	return overview
}

// mergeControllerKinds preserves the runtime/controller kind list while adding
// controller families that only appear in relationship evidence.
func mergeControllerKinds(kinds []string, families []string) []string {
	if len(families) == 0 {
		return kinds
	}
	seen := make(map[string]struct{}, len(kinds)+len(families))
	merged := make([]string, 0, len(kinds)+len(families))
	for _, kind := range append(append([]string{}, kinds...), families...) {
		kind = strings.TrimSpace(kind)
		if kind == "" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		merged = append(merged, kind)
	}
	return merged
}

func controllerEntityNames(controllerEntities []map[string]any) []string {
	names := make([]string, 0, len(controllerEntities))
	seen := make(map[string]struct{}, len(controllerEntities))
	for _, entity := range controllerEntities {
		name := StringVal(entity, "entity_name")
		if name == "" {
			name = StringVal(entity, "entity_id")
		}
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func controllerOverviewKinds(controllerEntities []map[string]any, platformKinds []string) []string {
	kinds := make([]string, 0, len(controllerEntities))
	seen := make(map[string]struct{}, len(controllerEntities))
	for _, entity := range controllerEntities {
		kind := StringVal(entity, "controller_kind")
		if kind == "" {
			kind = controllerEntityTypes[StringVal(entity, "entity_type")]
		}
		if kind == "" {
			continue
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	if len(kinds) > 0 {
		return kinds
	}
	return platformKinds
}

func metadataNonEmptyStringValue(metadata map[string]any, key string) string {
	value, _ := metadataNonEmptyString(metadata, key)
	return value
}

func uniqueNonEmptyRepoIDs(sources []map[string]any) []string {
	if len(sources) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(sources))
	repoIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		repoID := safeStr(source, "repo_id")
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	return repoIDs
}
