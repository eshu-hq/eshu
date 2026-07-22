// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"path"
	"slices"
	"sort"
	"strings"
)

func buildDeploymentSourceControllerEntity(entity EntityContent) (map[string]any, bool) {
	controllerKind, ok := controllerEntityTypes[entity.EntityType]
	if !ok {
		return nil, false
	}

	sourceRoots := deploymentTraceSourceRoots(entity.EntityType, entity.Metadata)
	discoveryRoots := deploymentTraceDiscoveryRoots(entity.Metadata)
	controller := map[string]any{
		"entity_id":              entity.EntityID,
		"entity_type":            entity.EntityType,
		"entity_name":            entity.EntityName,
		"controller_kind":        controllerKind,
		"repo_id":                entity.RepoID,
		"relative_path":          entity.RelativePath,
		"source_repo":            metadataNonEmptyStringValue(entity.Metadata, "source_repo"),
		"source_path":            metadataNonEmptyStringValue(entity.Metadata, "source_path"),
		"generator_source_repos": slices.Clone(metadataStringSlice(entity.Metadata, "generator_source_repos")),
		"generator_source_paths": slices.Clone(metadataStringSlice(entity.Metadata, "generator_source_paths")),
		"template_source_repos":  slices.Clone(metadataStringSlice(entity.Metadata, "template_source_repos")),
		"template_source_paths":  slices.Clone(metadataStringSlice(entity.Metadata, "template_source_paths")),
		"dest_server":            metadataNonEmptyStringValue(entity.Metadata, "dest_server"),
		"dest_namespace":         metadataNonEmptyStringValue(entity.Metadata, "dest_namespace"),
	}
	if len(sourceRoots) > 0 {
		controller["source_root"] = sourceRoots[0]
		controller["source_roots"] = sourceRoots
	}
	if len(discoveryRoots) > 0 {
		controller["discovery_roots"] = discoveryRoots
	}
	return controller, true
}

// selectRelevantDeploymentSourceControllers keeps the GitOps controller
// entities (ArgoCD/Flux) that are relevant to the traced service, out of the
// full entity set observed across deploymentSources' repos plus the
// workload's own repo (workloadRepoID).
//
// workloadRepoID is the grant-verified identity of the SPECIFIC traced
// workload (not a heuristic), but that alone does not make every controller
// found there safe to trust: a GitOps config repo commonly hosts MANY
// workloads' controllers in one app-of-apps monorepo (see the repo-helm
// fixture below). Trust is gated on WORKLOAD cardinality, via
// ownRepoWorkloadCount (the number of Workload nodes workloadRepoID DEFINES,
// capped at ownRepoWorkloadCountProbeLimit -- see
// countWorkloadsDefinedByRepo): when workloadRepoID defines exactly one
// workload, that workload IS the one being traced, so every controller found
// in that repo is unambiguously its controller, even when the controller's
// own entity name or path (e.g. an ArgoCD Application named after the app it
// deploys, not after the config repo that hosts it) does not textually
// contain the service name (#5471 defect A).
//
// ownRepoWorkloadCount is deliberately NOT how many controller ENTITIES are
// indexed in workloadRepoID -- an earlier version of this gate counted
// controllers instead of workloads and was itself a reachable leak (#5471
// review round 3 P0): a shared repo can define workloads A and B while only
// B's controller has been indexed so far (ordinary partial discovery, not a
// data error), so a controller-count of 1 does not prove there is only one
// workload the lone indexed controller could belong to. Tracing A against
// that repo must still fall back to the service-name-token match below and
// correctly reject B's controller. When workloadRepoID defines two or more
// workloads, every controller found there -- including ones token-matching
// nothing -- falls back to the same service-name-token match required for
// controllers named only by deploymentSources (a shared multi-service GitOps
// repo), so tracing service A can never pull service B's controller out of a
// shared repo, whether or not A's own controller happens to be indexed yet.
func selectRelevantDeploymentSourceControllers(
	serviceName string,
	workloadRepoID string,
	ownRepoWorkloadCount int,
	deploymentSources []map[string]any,
	entities []EntityContent,
) []map[string]any {
	if serviceName == "" || (len(deploymentSources) == 0 && workloadRepoID == "") || len(entities) == 0 {
		return nil
	}

	repoIDs := make(map[string]struct{}, len(deploymentSources)+1)
	for _, repoID := range uniqueNonEmptyRepoIDs(deploymentSources) {
		repoIDs[repoID] = struct{}{}
	}
	if workloadRepoID != "" {
		repoIDs[workloadRepoID] = struct{}{}
	}

	trustOwnRepo := workloadRepoID != "" && ownRepoWorkloadCount == 1

	serviceToken := normalizedDeploymentTraceMatch(serviceName)
	filtered := make([]map[string]any, 0, len(entities))
	for _, entity := range entities {
		if _, ok := repoIDs[entity.RepoID]; !ok {
			continue
		}
		controller, ok := buildDeploymentSourceControllerEntity(entity)
		if !ok {
			continue
		}
		if (trustOwnRepo && entity.RepoID == workloadRepoID) ||
			deploymentTraceControllerMatchesService(controller, serviceToken) {
			filtered = append(filtered, controller)
		}
	}
	sortDeploymentTraceMaps(filtered)
	return filtered
}

func collectDeploymentSourceK8sResources(
	controllerEntities []map[string]any,
	entities []EntityContent,
) ([]map[string]any, []string) {
	if len(controllerEntities) == 0 || len(entities) == 0 {
		return nil, nil
	}

	controllersByRepo := make(map[string][]map[string]any, len(controllerEntities))
	for _, controller := range controllerEntities {
		repoID := StringVal(controller, "repo_id")
		if repoID == "" {
			continue
		}
		controllersByRepo[repoID] = append(controllersByRepo[repoID], controller)
	}

	resources := make([]map[string]any, 0, len(entities))
	imageSet := make(map[string]struct{})
	seen := make(map[string]struct{}, len(entities))
	for _, entity := range entities {
		controller, sourceRoot, ok := matchDeploymentTraceController(entity, controllersByRepo[entity.RepoID])
		if !ok {
			continue
		}
		kind, include := deploymentTraceEntityKind(entity)
		if !include {
			continue
		}
		key := entity.EntityID + "|" + sourceRoot + "|" + StringVal(controller, "entity_id")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		qualifiedName, _ := metadataNonEmptyString(entity.Metadata, "qualified_name")
		containerImages := metadataStringSlice(entity.Metadata, "container_images")
		for _, imageRef := range containerImages {
			imageSet[imageRef] = struct{}{}
		}

		resource := map[string]any{
			"entity_id":            entity.EntityID,
			"entity_type":          entity.EntityType,
			"entity_name":          entity.EntityName,
			"kind":                 kind,
			"qualified_name":       qualifiedName,
			"relative_path":        entity.RelativePath,
			"repo_id":              entity.RepoID,
			"container_images":     containerImages,
			"source_root":          sourceRoot,
			"controller_kind":      StringVal(controller, "controller_kind"),
			"controller_entity_id": StringVal(controller, "entity_id"),
			"controller_path":      StringVal(controller, "relative_path"),
			"namespace":            k8sNamespace(entity.Metadata),
			// api_version is the resource's raw apiVersion string ("apps/v1",
			// "v1", ...), captured from the parsed K8sResource content row
			// (go/internal/parser/yaml/semantics.go:149) so query-time ArgoCD
			// tracking-id derivation (apiVersionGroup,
			// expectedArgoCDTrackingIDs, #5471 codex P1) can compute the
			// resource's API group without re-parsing content.
			"api_version": metadataNonEmptyStringValue(entity.Metadata, "api_version"),
		}
		// selector/pod_template_labels presence carries tri-state meaning
		// for k8sSelectMatch (see content_relationships_k8s_match.go): the
		// key must be omitted entirely, not set to "", when the source
		// content row lacks it.
		if selector, ok := entity.Metadata["selector"].(string); ok {
			resource["selector"] = selector
		}
		if podTemplateLabels, ok := entity.Metadata["pod_template_labels"].(string); ok {
			resource["pod_template_labels"] = podTemplateLabels
		}
		resources = append(resources, resource)
	}

	sortDeploymentTraceMaps(resources)
	imageRefs := make([]string, 0, len(imageSet))
	for imageRef := range imageSet {
		imageRefs = append(imageRefs, imageRef)
	}
	sort.Strings(imageRefs)
	return resources, imageRefs
}

// deploymentTraceSourceRoots derives the deployment-trace path roots for one
// controller entity. Every controller reads metadata["source_path"] and
// template_source_paths as-is (the ArgoCD and Flux Kustomization parsers both
// emit spec.path under "source_path"). FluxHelmRelease is the one exception
// (issue #5483 C2): per the Flux HelmRelease API,
// spec.chart.spec.chart is a chart NAME when source_ref_kind is
// HelmRepository, but a chart PATH when source_ref_kind is GitRepository or
// Bucket -- so "chart" is folded in as an extra root ONLY for that entity type
// and ONLY when source_ref_kind names a path-shaped source.
func deploymentTraceSourceRoots(entityType string, metadata map[string]any) []string {
	values := append([]string{metadataNonEmptyStringValue(metadata, "source_path")}, metadataStringSlice(metadata, "template_source_paths")...)
	if entityType == "FluxHelmRelease" && fluxHelmReleaseChartIsPathRoot(metadata) {
		values = append(values, metadataNonEmptyStringValue(metadata, "chart"))
	}
	return deploymentTraceNormalizedRoots(values)
}

// fluxHelmReleaseChartIsPathRoot reports whether a FluxHelmRelease's
// spec.chart.spec.chart names a path (GitRepository/Bucket source) rather
// than a chart name (HelmRepository source, or an unresolved/absent
// source_ref_kind). See the Flux HelmRelease API
// (helm.toolkit.fluxcd.io): chart is a NAME only for a HelmRepository
// source; every other source kind treats it as a repository-relative path.
func fluxHelmReleaseChartIsPathRoot(metadata map[string]any) bool {
	switch metadataNonEmptyStringValue(metadata, "source_ref_kind") {
	case "GitRepository", "Bucket":
		return true
	default:
		return false
	}
}

func deploymentTraceDiscoveryRoots(metadata map[string]any) []string {
	return deploymentTraceNormalizedRoots(metadataStringSlice(metadata, "generator_source_paths"))
}

func deploymentTraceNormalizedRoots(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	roots := make([]string, 0, len(values))
	for _, value := range values {
		root := normalizeDeploymentTraceRoot(value)
		if root == "" {
			continue
		}
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

func normalizeDeploymentTraceRoot(raw string) string {
	trimmed := strings.TrimSpace(strings.Trim(raw, `"'`))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	if wildcard := strings.Index(trimmed, "*"); wildcard >= 0 {
		trimmed = strings.TrimSuffix(trimmed[:wildcard], "/")
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	if ext := path.Ext(cleaned); ext != "" {
		cleaned = path.Dir(cleaned)
	}
	cleaned = strings.TrimPrefix(cleaned, "./")
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return strings.TrimSuffix(cleaned, "/")
}

func deploymentTraceControllerMatchesService(controller map[string]any, normalizedService string) bool {
	if normalizedService == "" {
		return false
	}
	identityCandidates := []string{
		StringVal(controller, "entity_id"),
		StringVal(controller, "entity_name"),
		StringVal(controller, "source_repo"),
	}
	for _, candidate := range identityCandidates {
		if normalizedDeploymentTraceMatch(candidate) == normalizedService {
			return true
		}
	}

	pathCandidates := []string{
		StringVal(controller, "relative_path"),
		StringVal(controller, "source_path"),
		StringVal(controller, "source_root"),
	}
	pathCandidates = append(pathCandidates, stringSliceMapValue(controller, "source_roots")...)
	pathCandidates = append(pathCandidates, stringSliceMapValue(controller, "discovery_roots")...)
	for _, candidate := range pathCandidates {
		if deploymentTracePathHasServiceSegment(candidate, normalizedService) {
			return true
		}
	}
	return false
}

func deploymentTracePathHasServiceSegment(candidate string, normalizedService string) bool {
	for _, segment := range strings.FieldsFunc(candidate, func(separator rune) bool {
		return separator == '/' || separator == '\\'
	}) {
		if normalizedDeploymentTraceMatch(segment) == normalizedService {
			return true
		}
	}
	return false
}

func normalizedDeploymentTraceMatch(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", "-", "/", "-", ".", "-", ":", "-", " ", "-")
	return replacer.Replace(lower)
}

func matchDeploymentTraceController(entity EntityContent, controllers []map[string]any) (map[string]any, string, bool) {
	if len(controllers) == 0 {
		return nil, "", false
	}
	normalizedPath := normalizeDeploymentTraceRoot(entity.RelativePath)
	bestIndex := -1
	bestRoot := ""
	for index, controller := range controllers {
		for _, root := range stringSliceMapValue(controller, "source_roots") {
			if !deploymentTracePathWithinRoot(normalizedPath, root) {
				continue
			}
			if len(root) > len(bestRoot) {
				bestIndex = index
				bestRoot = root
			}
		}
	}
	if bestIndex < 0 {
		return nil, "", false
	}
	return controllers[bestIndex], bestRoot, true
}

func deploymentTracePathWithinRoot(relativePath string, root string) bool {
	normalizedRoot := normalizeDeploymentTraceRoot(root)
	if relativePath == "" || normalizedRoot == "" {
		return false
	}
	return relativePath == normalizedRoot || strings.HasPrefix(relativePath, normalizedRoot+"/")
}

func deploymentTraceEntityKind(entity EntityContent) (string, bool) {
	if entity.EntityType != "K8sResource" {
		return "", false
	}
	return metadataNonEmptyStringValue(entity.Metadata, "kind"), true
}

func sortDeploymentTraceMaps(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool {
		leftRepoID := StringVal(rows[i], "repo_id")
		rightRepoID := StringVal(rows[j], "repo_id")
		if leftRepoID != rightRepoID {
			return leftRepoID < rightRepoID
		}
		leftPath := StringVal(rows[i], "relative_path")
		rightPath := StringVal(rows[j], "relative_path")
		if leftPath != rightPath {
			return leftPath < rightPath
		}
		return StringVal(rows[i], "entity_id") < StringVal(rows[j], "entity_id")
	})
}
