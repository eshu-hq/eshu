// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
)

// This file holds the deployment-trace story/fact builders split out of
// deployment_trace_support_helpers.go to keep that file under the repo's
// 500-line cap (#5720). None of these builders issue a graph Run or
// RunSingle call, so none are tracked by the query-source-coverage gate.

func buildStorySections(platforms, platformKinds, environments []string) []map[string]any {
	sections := []map[string]any{
		{
			"title":   "deployment",
			"summary": fmt.Sprintf("%d platform target(s) across %d environment(s)", len(platforms), len(environments)),
		},
	}
	if len(platformKinds) > 0 {
		sections = append(sections, map[string]any{
			"title":   "controllers",
			"summary": fmt.Sprintf("Observed controller families: %s", joinOrNone(platformKinds)),
		})
	}
	return sections
}

func buildGitOpsOverview(
	platforms []string,
	platformKinds []string,
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) map[string]any {
	toolFamilies := deploymentTraceGitOpsToolFamilies(platformKinds, deploymentSources, deploymentEvidence, controllerEntities)
	enabled := len(toolFamilies) > 0
	if len(toolFamilies) == 0 {
		toolFamilies = platformKinds
	}
	return map[string]any{
		"enabled":          enabled,
		"tool_families":    toolFamilies,
		"observed_targets": platforms,
	}
}

func buildRuntimeOverview(environments []string) map[string]any {
	return map[string]any{
		"environment_count": len(environments),
		"environments":      environments,
	}
}

func buildDeploymentFacts(
	instances []map[string]any,
	topologyEdges []map[string]any,
	provisionedPlatforms []map[string]any,
	deploymentSources []map[string]any,
) []map[string]any {
	facts := make([]map[string]any, 0, len(instances)*2+len(topologyEdges)+len(provisionedPlatforms)*2+len(deploymentSources))
	for _, topologyEdge := range topologyEdges {
		if fact := deploymentTopologyFact(topologyEdge, nil); fact != nil {
			facts = append(facts, fact)
		}
	}
	for _, instance := range instances {
		for _, platform := range platformTargets(instance) {
			for _, topologyEdge := range mapSliceValue(platform, "topology_edges") {
				fact := deploymentTopologyFact(topologyEdge, platform)
				if fact != nil {
					facts = append(facts, fact)
				}
			}
		}
	}
	for _, platform := range provisionedPlatforms {
		for _, topologyEdge := range mapSliceValue(platform, "topology_edges") {
			if fact := deploymentTopologyFact(topologyEdge, platform); fact != nil {
				facts = append(facts, fact)
			}
		}
	}
	for _, source := range deploymentSources {
		fact := map[string]any{
			"type":       firstNonEmptyString(safeStr(source, "relationship_type"), "DEPLOYS_FROM"),
			"target":     safeStr(source, "repo_name"),
			"target_id":  firstNonEmptyString(safeStr(source, "target_id"), safeStr(source, "repo_id")),
			"confidence": floatVal(source, "confidence"),
			"reason":     safeStr(source, "reason"),
		}
		if sourceID := safeStr(source, "source_id"); sourceID != "" {
			fact["source_id"] = sourceID
		}
		facts = append(facts, fact)
	}
	return facts
}

func deploymentTopologyFact(topologyEdge, platform map[string]any) map[string]any {
	relationshipType := StringVal(topologyEdge, "relationship_type")
	sourceID := StringVal(topologyEdge, "source_id")
	targetID := StringVal(topologyEdge, "target_id")
	if relationshipType == "" || sourceID == "" || targetID == "" {
		return nil
	}
	targetName := firstNonEmptyString(
		StringVal(topologyEdge, "target_name"),
		StringVal(platform, "platform_name"),
		targetID,
	)
	fact := map[string]any{
		"type":       relationshipType,
		"source_id":  sourceID,
		"target_id":  targetID,
		"target":     targetName,
		"confidence": floatVal(topologyEdge, "confidence"),
		"reason":     StringVal(topologyEdge, "reason"),
	}
	for _, field := range []string{"source_name", "evidence_source", "source_tool"} {
		if value := StringVal(topologyEdge, field); value != "" {
			fact[field] = value
		}
	}
	if targetID == StringVal(platform, "platform_id") {
		if kind := StringVal(platform, "platform_kind"); kind != "" {
			fact["kind"] = kind
		}
	}
	return fact
}

func firstPositiveFloat(candidates ...float64) float64 {
	for _, candidate := range candidates {
		if candidate > 0 {
			return candidate
		}
	}
	return 0
}

func buildControllerDrivenPaths(instances []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(instances))
	paths := make([]map[string]any, 0, len(instances))
	for _, instance := range instances {
		for _, platform := range platformTargets(instance) {
			platformName := StringVal(platform, "platform_name")
			platformKind := StringVal(platform, "platform_kind")
			if platformName == "" && platformKind == "" {
				continue
			}
			key := platformName + "|" + platformKind
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			path := map[string]any{}
			if platformKind != "" {
				path["controller_kind"] = platformKind
			}
			if platformName != "" {
				path["observed_target"] = platformName
			}
			paths = append(paths, path)
		}
	}
	sortDeploymentTraceMaps(paths)
	return paths
}

// deploymentTraceGitOpsToolFamilies returns GitOps tool families backed by
// controller entities, platform kinds, or relationship evidence.
func deploymentTraceGitOpsToolFamilies(
	platformKinds []string,
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) []string {
	families := deploymentTraceEvidenceControllerFamilies(deploymentSources, deploymentEvidence, controllerEntities)
	// "flux", "flux_kustomization", "flux_helmrelease" are deliberately not
	// matched below: no parser or collector emits those as a platform-kind
	// value today, so that branch was dead (issue #5342). The Flux
	// Kustomization parse path captures typed evidence only and is not
	// wired to any platform-kind classification until Flux modeling lands a
	// real emitter (#5360).
	for _, kind := range platformKinds {
		normalized := strings.TrimSpace(strings.ToLower(kind))
		if normalized == "argocd" || normalized == "argocd_application" || normalized == "argocd_applicationset" {
			families = append(families, "argocd")
		}
	}
	return uniqueSortedStrings(families)
}

// deploymentTraceEvidenceControllerFamilies lifts controller families out of
// provenance evidence so read surfaces do not lose GitOps truth when runtime
// platform kinds are generic values like kubernetes or ecs.
func deploymentTraceEvidenceControllerFamilies(
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) []string {
	families := make([]string, 0, len(controllerEntities)+len(deploymentSources))
	for _, entity := range controllerEntities {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(entity, "controller_kind")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(entity, "entity_type")))
	}
	for _, source := range deploymentSources {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(source, "reason")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(source, "evidence_type")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(source, "evidence_kind")))
	}
	for _, family := range stringSliceMapValue(deploymentEvidence, "tool_families") {
		families = append(families, deploymentTraceControllerFamilyFromText(family))
	}
	for _, artifact := range mapSliceValue(deploymentEvidence, "artifacts") {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "tool_family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "evidence_type")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(artifact, "evidence_kind")))
		for _, kind := range StringSliceVal(artifact, "evidence_kinds") {
			families = append(families, deploymentTraceControllerFamilyFromText(kind))
		}
	}
	for _, path := range mapSliceValue(deploymentEvidence, "delivery_paths") {
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "tool_family")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "kind")))
		families = append(families, deploymentTraceControllerFamilyFromText(StringVal(path, "evidence_type")))
	}
	return uniqueSortedStrings(families)
}

func deploymentTraceControllerFamilyFromText(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch {
	case normalized == "":
		return ""
	case strings.Contains(normalized, "argocd"):
		return "argocd"
	case strings.Contains(normalized, "flux"):
		return "flux"
	default:
		return ""
	}
}
