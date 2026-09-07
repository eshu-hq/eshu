// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// This file holds the deployment-trace story/fact builders for the
// trace_deployment_chain response surface. They moved here from the query
// root (deployment_trace_story_facts.go) with lane B2 of #6060: the response
// shaper in this package is their only caller, and an impacttrace file
// cannot name root-defined helpers. DeploymentTraceEvidenceControllerFamilies
// stays exported because the impact package calls it from outside this
// package. None of these builders issue a graph Run or RunSingle call, so
// none are tracked by the query-source-coverage gate.

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
			"summary": fmt.Sprintf("Observed controller families: %s", JoinOrNone(platformKinds)),
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
	toolFamilies := DeploymentTraceGitOpsToolFamilies(platformKinds, deploymentSources, deploymentEvidence, controllerEntities)
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

func BuildDeploymentFacts(
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
		for _, platform := range querycontract.PlatformTargets(instance) {
			for _, topologyEdge := range querycontract.MapSliceValue(platform, "topology_edges") {
				fact := deploymentTopologyFact(topologyEdge, platform)
				if fact != nil {
					facts = append(facts, fact)
				}
			}
		}
	}
	for _, platform := range provisionedPlatforms {
		for _, topologyEdge := range querycontract.MapSliceValue(platform, "topology_edges") {
			if fact := deploymentTopologyFact(topologyEdge, platform); fact != nil {
				facts = append(facts, fact)
			}
		}
	}
	for _, source := range deploymentSources {
		fact := map[string]any{
			"type":       querycontract.FirstNonEmptyString(querycontract.SafeStr(source, "relationship_type"), "DEPLOYS_FROM"),
			"target":     querycontract.SafeStr(source, "repo_name"),
			"target_id":  querycontract.FirstNonEmptyString(querycontract.SafeStr(source, "target_id"), querycontract.SafeStr(source, "repo_id")),
			"confidence": querycontract.FloatVal(source, "confidence"),
			"reason":     querycontract.SafeStr(source, "reason"),
		}
		if sourceID := querycontract.SafeStr(source, "source_id"); sourceID != "" {
			fact["source_id"] = sourceID
		}
		facts = append(facts, fact)
	}
	return facts
}

func deploymentTopologyFact(topologyEdge, platform map[string]any) map[string]any {
	relationshipType := querycontract.StringVal(topologyEdge, "relationship_type")
	sourceID := querycontract.StringVal(topologyEdge, "source_id")
	targetID := querycontract.StringVal(topologyEdge, "target_id")
	if relationshipType == "" || sourceID == "" || targetID == "" {
		return nil
	}
	targetName := querycontract.FirstNonEmptyString(
		querycontract.StringVal(topologyEdge, "target_name"),
		querycontract.StringVal(platform, "platform_name"),
		targetID,
	)
	fact := map[string]any{
		"type":       relationshipType,
		"source_id":  sourceID,
		"target_id":  targetID,
		"target":     targetName,
		"confidence": querycontract.FloatVal(topologyEdge, "confidence"),
		"reason":     querycontract.StringVal(topologyEdge, "reason"),
	}
	for _, field := range []string{"source_name", "evidence_source", "source_tool"} {
		if value := querycontract.StringVal(topologyEdge, field); value != "" {
			fact[field] = value
		}
	}
	if targetID == querycontract.StringVal(platform, "platform_id") {
		if kind := querycontract.StringVal(platform, "platform_kind"); kind != "" {
			fact["kind"] = kind
		}
	}
	return fact
}

// firstPositiveFloat returns the first positive candidate, or zero. The
// implementation moved to querycontract for #6060; this wrapper keeps root
// callers unchanged.
func buildControllerDrivenPaths(instances []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(instances))
	paths := make([]map[string]any, 0, len(instances))
	for _, instance := range instances {
		for _, platform := range querycontract.PlatformTargets(instance) {
			platformName := querycontract.StringVal(platform, "platform_name")
			platformKind := querycontract.StringVal(platform, "platform_kind")
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
	SortDeploymentTraceMaps(paths)
	return paths
}

// deploymentTraceGitOpsToolFamilies returns GitOps tool families backed by
// controller entities, platform kinds, or relationship evidence.
func DeploymentTraceGitOpsToolFamilies(
	platformKinds []string,
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) []string {
	families := DeploymentTraceEvidenceControllerFamilies(deploymentSources, deploymentEvidence, controllerEntities)
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
	return querycontract.UniqueSortedStrings(families)
}

// deploymentTraceEvidenceControllerFamilies lifts controller families out of
// provenance evidence so read surfaces do not lose GitOps truth when runtime
// platform kinds are generic values like kubernetes or ecs.
func DeploymentTraceEvidenceControllerFamilies(
	deploymentSources []map[string]any,
	deploymentEvidence map[string]any,
	controllerEntities []map[string]any,
) []string {
	families := make([]string, 0, len(controllerEntities)+len(deploymentSources))
	for _, entity := range controllerEntities {
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(entity, "controller_kind")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(entity, "entity_type")))
	}
	for _, source := range deploymentSources {
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(source, "reason")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(source, "evidence_type")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(source, "evidence_kind")))
	}
	for _, family := range querycontract.StringSliceMapValue(deploymentEvidence, "tool_families") {
		families = append(families, deploymentTraceControllerFamilyFromText(family))
	}
	for _, artifact := range querycontract.MapSliceValue(deploymentEvidence, "artifacts") {
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(artifact, "family")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(artifact, "tool_family")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(artifact, "evidence_type")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(artifact, "evidence_kind")))
		for _, kind := range querycontract.StringSliceVal(artifact, "evidence_kinds") {
			families = append(families, deploymentTraceControllerFamilyFromText(kind))
		}
	}
	for _, path := range querycontract.MapSliceValue(deploymentEvidence, "delivery_paths") {
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(path, "family")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(path, "tool_family")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(path, "kind")))
		families = append(families, deploymentTraceControllerFamilyFromText(querycontract.StringVal(path, "evidence_type")))
	}
	return querycontract.UniqueSortedStrings(families)
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
