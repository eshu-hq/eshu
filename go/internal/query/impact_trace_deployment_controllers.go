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
) ([]map[string]any, []map[string]any, []string, error) {
	if h == nil || h.Content == nil || len(deploymentSources) == 0 {
		return nil, nil, nil, nil
	}

	repoIDs := uniqueNonEmptyRepoIDs(deploymentSources)
	entities := make([]EntityContent, 0, len(repoIDs)*8)
	for _, repoID := range repoIDs {
		rows, err := h.Content.ListRepoEntities(ctx, repoID, repositorySemanticEntityLimit)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("list deployment source entities for %s: %w", repoID, err)
		}
		entities = append(entities, rows...)
	}

	controllers := selectRelevantDeploymentSourceControllers(serviceName, deploymentSources, entities)
	k8sResources, imageRefs := collectDeploymentSourceK8sResources(controllers, entities)
	return controllers, k8sResources, imageRefs, nil
}

func buildControllerOverview(
	platforms []string,
	platformKinds []string,
	controllerEntities []map[string]any,
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
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
