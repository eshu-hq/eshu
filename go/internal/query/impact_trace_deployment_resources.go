// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"sort"
	"strings"
)

func buildDeploymentFactSummary(
	workloadContext map[string]any,
	instances []map[string]any,
	materializedEnvironments []string,
	configEnvironments []string,
	platforms []string,
	deploymentSources []map[string]any,
	cloudResources []map[string]any,
	k8sResources []map[string]any,
	imageRefs []string,
	deploymentFacts []map[string]any,
	mappingMode string,
) map[string]any {
	overallConfidence, confidenceReason := deploymentOverallConfidence(instances, deploymentSources, configEnvironments)
	uncorrelatedCloudResources := mapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	summary := map[string]any{
		"instance_count":                 len(instances),
		"environment_count":              len(materializedEnvironments),
		"materialized_environment_count": len(materializedEnvironments),
		"config_environment_count":       len(configEnvironments),
		"platform_count":                 len(platforms),
		"deployment_source_count":        len(deploymentSources),
		"cloud_resource_count":           len(cloudResources),
		"k8s_resource_count":             len(k8sResources),
		"image_ref_count":                len(imageRefs),
		"fact_count":                     len(deploymentFacts),
		"has_repository":                 safeStr(workloadContext, "repo_id") != "",
		"mapping_mode":                   mappingMode,
		"overall_confidence":             overallConfidence,
		"overall_confidence_reason":      confidenceReason,
	}
	if len(uncorrelatedCloudResources) > 0 {
		summary["uncorrelated_cloud_resource_count"] = len(uncorrelatedCloudResources)
		summary["missing_evidence"] = []string{"workload_cloud_relationship_missing"}
	}
	if limitations := deploymentFactSummaryLimitations(instances, configEnvironments); len(limitations) > 0 {
		summary["limitations"] = limitations
	}
	return summary
}

func deploymentOverallConfidence(
	instances []map[string]any,
	deploymentSources []map[string]any,
	configEnvironments []string,
) (float64, string) {
	if len(instances) > 0 {
		minConfidence := 1.0
		found := false
		for _, instance := range instances {
			confidence := firstPositiveFloat(
				floatVal(instance, "materialization_confidence"),
				floatVal(instance, "platform_confidence"),
			)
			if confidence <= 0 {
				continue
			}
			found = true
			if confidence < minConfidence {
				minConfidence = confidence
			}
		}
		if found {
			return minConfidence, "materialized_runtime_instances"
		}
		return 0.9, "materialized_runtime_instances"
	}
	if len(deploymentSources) > 0 {
		minConfidence := 1.0
		found := false
		for _, source := range deploymentSources {
			confidence := floatVal(source, "confidence")
			if confidence <= 0 {
				continue
			}
			found = true
			if confidence < minConfidence {
				minConfidence = confidence
			}
		}
		if found {
			return minConfidence, "canonical_deployment_sources"
		}
		return 0.75, "canonical_deployment_sources"
	}
	if len(configEnvironments) > 0 {
		return 0.45, "config_only_evidence"
	}
	return 0, "no_deployment_evidence"
}

func deploymentFactSummaryLimitations(instances []map[string]any, configEnvironments []string) []string {
	if len(instances) == 0 && len(configEnvironments) == 0 {
		return nil
	}
	limitations := []string{}
	if len(instances) == 0 && len(configEnvironments) > 0 {
		limitations = append(limitations, "config_environments_present_without_materialized_runtime_instances")
	}
	return limitations
}

// cloudResourceResult holds the workload's materialized USES cloud
// dependencies plus bounded-collection metadata.
//
// The query below is anchored on the grant-verified workload id, so no
// per-row grant predicate is needed here. The free-text CloudResource
// fallbacks that cannot bind to a repo_id at all (the config-derived and
// uncorrelated candidate scans) are gated by the caller's !access.scoped()
// checks in the handler instead (#5167 W3), so a scoped caller never
// reaches them and this fetch only ever returns the materialized USES
// edges of the caller's own grant-verified workload.
type cloudResourceResult struct {
	rows   []map[string]any
	limits map[string]any
}

func (h *ImpactHandler) fetchCloudResourceResult(ctx context.Context, workloadID string) (cloudResourceResult, error) {
	queryLimit := serviceStoryItemLimit + 1
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:USES]->(c:CloudResource)
		WITH c, collect({
		     environment: coalesce(rel.environment, c.environment, i.environment, ''),
		     confidence: coalesce(rel.confidence, 0.0),
		     reason: coalesce(rel.reason, ''),
		     relationship_basis: coalesce(rel.relationship_basis, ''),
		     resolution_mode: coalesce(rel.resolution_mode, ''),
		     evidence_source: coalesce(rel.evidence_source, ''),
		     service_anchor_source: coalesce(rel.service_anchor_source, ''),
		     service_anchor_reason: coalesce(rel.service_anchor_reason, ''),
		     source_fact_id: coalesce(rel.source_fact_id, ''),
		     stable_fact_key: coalesce(rel.stable_fact_key, ''),
		     source_system: coalesce(rel.source_system, ''),
		     source_record_id: coalesce(rel.source_record_id, ''),
		     collector_kind: coalesce(rel.collector_kind, '')
		}) as observations
		RETURN c.id as id, c.name as name, c.kind as kind, c.provider as provider, observations
		ORDER BY name, id
		LIMIT $cloud_resource_limit
	`, map[string]any{"workload_id": workloadID, "cloud_resource_limit": queryLimit})
	if err != nil {
		return cloudResourceResult{}, err
	}
	resources, err := deploymentTraceCloudResourcesFromRows(rows, "")
	if err != nil {
		return cloudResourceResult{}, err
	}
	return boundedCloudResourceResult(resources, queryLimit), nil
}

func boundedCloudResourceResult(rows []map[string]any, queryLimit int) cloudResourceResult {
	returned, truncated := capMapRows(rows, serviceStoryItemLimit)
	return cloudResourceResult{
		rows: returned,
		limits: boundedCollectionMetadata(
			serviceStoryItemLimit, queryLimit, len(returned), len(rows), truncated,
			[]string{"name", "id"},
		),
	}
}

func deploymentTraceCloudResourcesFromRows(rows []map[string]any, defaultRelationshipBasis string) ([]map[string]any, error) {
	resources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		row = cloudResourceRowWithSelectedObservation(row)
		resources = append(resources, map[string]any{
			"id":                    StringVal(row, "id"),
			"name":                  StringVal(row, "name"),
			"kind":                  StringVal(row, "kind"),
			"resource_type":         StringVal(row, "resource_type"),
			"provider":              StringVal(row, "provider"),
			"environment":           StringVal(row, "environment"),
			"confidence":            floatVal(row, "confidence"),
			"reason":                StringVal(row, "reason"),
			"relationship_basis":    firstNonEmptyString(StringVal(row, "relationship_basis"), defaultRelationshipBasis),
			"resolution_mode":       StringVal(row, "resolution_mode"),
			"evidence_source":       StringVal(row, "evidence_source"),
			"service_anchor_source": StringVal(row, "service_anchor_source"),
			"service_anchor_reason": StringVal(row, "service_anchor_reason"),
			"source_fact_id":        StringVal(row, "source_fact_id"),
			"stable_fact_key":       StringVal(row, "stable_fact_key"),
			"source_system":         StringVal(row, "source_system"),
			"source_record_id":      StringVal(row, "source_record_id"),
			"collector_kind":        StringVal(row, "collector_kind"),
		})
	}
	return resources, nil
}

func cloudResourceRowWithSelectedObservation(row map[string]any) map[string]any {
	observations := mapSliceValue(row, "observations")
	if len(observations) == 0 {
		return row
	}
	selected := append([]map[string]any(nil), observations...)
	sort.SliceStable(selected, func(left, right int) bool {
		leftConfidence := floatVal(selected[left], "confidence")
		rightConfidence := floatVal(selected[right], "confidence")
		if leftConfidence != rightConfidence {
			return leftConfidence > rightConfidence
		}
		return cloudResourceObservationKey(selected[left]) < cloudResourceObservationKey(selected[right])
	})
	merged := make(map[string]any, len(row)+len(selected[0]))
	for key, value := range row {
		if key != "observations" {
			merged[key] = value
		}
	}
	for key, value := range selected[0] {
		merged[key] = value
	}
	return merged
}

func cloudResourceObservationKey(observation map[string]any) string {
	return strings.Join([]string{
		StringVal(observation, "stable_fact_key"),
		StringVal(observation, "source_fact_id"),
		StringVal(observation, "source_system"),
		StringVal(observation, "source_record_id"),
		StringVal(observation, "relationship_basis"),
		StringVal(observation, "resolution_mode"),
		StringVal(observation, "evidence_source"),
		StringVal(observation, "service_anchor_source"),
		StringVal(observation, "service_anchor_reason"),
		StringVal(observation, "collector_kind"),
		StringVal(observation, "environment"),
		StringVal(observation, "reason"),
	}, "\x00")
}

func deploymentTraceCloudCandidates(rows []map[string]any) []map[string]any {
	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		candidate := make(map[string]any, len(row)+3)
		for key, value := range row {
			candidate[key] = value
		}
		candidate["candidate_status"] = "uncorrelated"
		candidate["match_basis"] = firstNonEmptyString(
			StringVal(row, "relationship_basis"),
			"deployment_config_read_evidence",
		)
		candidate["missing_relationship"] = "workload_cloud_relationship"
		candidates = append(candidates, candidate)
	}
	return candidates
}

type k8sResourceResult struct {
	rows              []map[string]any
	imageRefs         []string
	limits            map[string]any
	candidates        []map[string]any
	contentLowerBound bool
}

func (h *ImpactHandler) fetchK8sResources(
	ctx context.Context,
	repoID string,
	workloadName string,
) ([]map[string]any, []string, error) {
	result, err := h.fetchK8sResourceResult(ctx, repoID, workloadName)
	return result.rows, result.imageRefs, err
}

func (h *ImpactHandler) fetchK8sResourceResult(
	ctx context.Context,
	repoID string,
	workloadName string,
) (k8sResourceResult, error) {
	if h == nil || h.Content == nil || repoID == "" || workloadName == "" {
		return boundedK8sResourceResult(nil, false, nil, false), nil
	}

	queryLimit := serviceStoryItemLimit + 1
	rows, err := h.Content.SearchEntitiesByName(ctx, repoID, "K8sResource", workloadName, queryLimit)
	if err != nil {
		return k8sResourceResult{}, err
	}

	resources := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if row.EntityName != workloadName {
			continue
		}
		kind, _ := metadataNonEmptyString(row.Metadata, "kind")
		qualifiedName, _ := metadataNonEmptyString(row.Metadata, "qualified_name")
		images := metadataStringSlice(row.Metadata, "container_images")
		resource := map[string]any{
			"entity_id":        row.EntityID,
			"repo_id":          row.RepoID,
			"entity_name":      row.EntityName,
			"kind":             kind,
			"qualified_name":   qualifiedName,
			"relative_path":    row.RelativePath,
			"container_images": images,
			"namespace":        k8sNamespace(row.Metadata),
		}
		// selector/pod_template_labels presence carries tri-state meaning
		// for k8sSelectMatch (see content_relationships_k8s_match.go): the
		// key must be omitted entirely, not set to "", when the source
		// content row lacks it.
		if selector, ok := row.Metadata["selector"].(string); ok {
			resource["selector"] = selector
		}
		if podTemplateLabels, ok := row.Metadata["pod_template_labels"].(string); ok {
			resource["pod_template_labels"] = podTemplateLabels
		}
		resources = append(resources, resource)
	}
	return boundedK8sResourceResult(resources, len(rows) >= queryLimit, nil, false), nil
}

func boundedK8sResourceResult(
	contentRows []map[string]any,
	contentLowerBound bool,
	deploymentSourceRows []map[string]any,
	deploymentSourceLowerBound bool,
) k8sResourceResult {
	merged := mergeDeploymentTraceRows(contentRows, deploymentSourceRows)
	sortDeploymentTraceMaps(merged)
	observedCount := len(merged)
	rows, mergedTruncated := capMapRows(merged, serviceStoryItemLimit)

	imageSet := make(map[string]struct{})
	for _, row := range rows {
		for _, image := range StringSliceVal(row, "container_images") {
			imageSet[image] = struct{}{}
		}
	}
	imageRefs := make([]string, 0, len(imageSet))
	for image := range imageSet {
		imageRefs = append(imageRefs, image)
	}
	sort.Strings(imageRefs)
	observedCountIsLowerBound := contentLowerBound || deploymentSourceLowerBound
	return k8sResourceResult{
		rows:              rows,
		imageRefs:         imageRefs,
		candidates:        merged,
		contentLowerBound: contentLowerBound,
		limits: map[string]any{
			"limit":                                           serviceStoryItemLimit,
			"query_sentinel_limit":                            serviceStoryItemLimit + 1,
			"deployment_source_query_sentinel_limit":          repositorySemanticEntityLimit + 1,
			"returned_count":                                  len(rows),
			"observed_count":                                  observedCount,
			"observed_count_is_lower_bound":                   observedCountIsLowerBound,
			"content_observed_count":                          len(contentRows),
			"content_observed_count_is_lower_bound":           contentLowerBound,
			"deployment_source_observed_count":                len(deploymentSourceRows),
			"deployment_source_observed_count_is_lower_bound": deploymentSourceLowerBound,
			"truncated":                                       observedCountIsLowerBound || mergedTruncated,
			"ordering":                                        []string{"repo_id", "relative_path", "entity_id"},
		},
	}
}

func distinctSortedInstanceField(instances []map[string]any, key string) []string {
	values := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if key == "platform_name" || key == "platform_kind" {
			for _, platform := range platformTargets(instance) {
				value := safeStr(platform, key)
				if value == "" {
					continue
				}
				values[value] = struct{}{}
			}
			continue
		}
		value := safeStr(instance, key)
		if value == "" {
			continue
		}
		values[value] = struct{}{}
	}

	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func mergeDeploymentTraceRows(left []map[string]any, right []map[string]any) []map[string]any {
	if len(left) == 0 {
		return right
	}
	if len(right) == 0 {
		return left
	}
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]map[string]any, 0, len(left)+len(right))
	for _, row := range append(append([]map[string]any{}, left...), right...) {
		key := StringVal(row, "entity_id")
		if key == "" {
			key = StringVal(row, "qualified_name") + "|" + StringVal(row, "relative_path")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, row)
	}
	sortDeploymentTraceMaps(merged)
	return merged
}
