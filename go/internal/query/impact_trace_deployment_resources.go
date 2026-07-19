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

type cloudResourceResult struct {
	rows   []map[string]any
	limits map[string]any
}

func (h *ImpactHandler) fetchCloudResourceResult(ctx context.Context, workloadID string) (cloudResourceResult, error) {
	queryLimit := serviceStoryItemLimit + 1
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:USES]->(c:CloudResource)
		WITH c.id as id, c.name as name, c.kind as kind, c.provider as provider,
		     min(coalesce(rel.environment, c.environment, i.environment, '')) as environment,
		     max(coalesce(rel.confidence, 0.0)) as confidence,
		     min(coalesce(rel.reason, '')) as reason,
		     min(coalesce(rel.relationship_basis, '')) as relationship_basis,
		     min(coalesce(rel.resolution_mode, '')) as resolution_mode,
		     min(coalesce(rel.evidence_source, '')) as evidence_source,
		     min(coalesce(rel.service_anchor_source, '')) as service_anchor_source,
		     min(coalesce(rel.service_anchor_reason, '')) as service_anchor_reason,
		     min(coalesce(rel.source_fact_id, '')) as source_fact_id,
		     min(coalesce(rel.stable_fact_key, '')) as stable_fact_key,
		     min(coalesce(rel.source_system, '')) as source_system,
		     min(coalesce(rel.source_record_id, '')) as source_record_id,
		     min(coalesce(rel.collector_kind, '')) as collector_kind
		RETURN id, name, kind, provider, environment, confidence, reason,
		       relationship_basis, resolution_mode, evidence_source, service_anchor_source,
		       service_anchor_reason, source_fact_id, stable_fact_key, source_system,
		       source_record_id, collector_kind
		ORDER BY name, id
		LIMIT $cloud_resource_limit
	`, map[string]any{"workload_id": workloadID, "cloud_resource_limit": queryLimit})
	if err != nil {
		return cloudResourceResult{}, err
	}
	if len(rows) == 0 {
		rows, err = h.fetchConfigDerivedCloudResourceRows(ctx, workloadID, queryLimit)
		if err != nil {
			return cloudResourceResult{}, err
		}
		resources, err := deploymentTraceCloudResourcesFromRows(rows, "deployment_config_read_evidence")
		if err != nil {
			return cloudResourceResult{}, err
		}
		return boundedCloudResourceResult(resources, queryLimit), nil
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

func (h *ImpactHandler) fetchConfigDerivedCloudResourceRows(ctx context.Context, workloadID string, limit int) ([]map[string]any, error) {
	serviceName := strings.TrimPrefix(strings.TrimSpace(workloadID), "workload:")
	if h == nil || h.Neo4j == nil || serviceName == "" {
		return nil, nil
	}
	rows, err := h.Neo4j.Run(ctx, `
		MATCH (c:CloudResource)
		WHERE coalesce(c.name, '') CONTAINS $service_name
		   OR coalesce(c.id, '') CONTAINS $service_name
		   OR coalesce(c.resource_id, '') CONTAINS $service_name
		   OR coalesce(c.arn, '') CONTAINS $service_name
		   OR coalesce(c.config_path, '') CONTAINS $service_name
		RETURN DISTINCT coalesce(c.id, c.uid, c.resource_id, c.arn, c.name) as id,
		       coalesce(c.name, '') as name,
		       coalesce(c.kind, c.resource_type, c.data_type, '') as kind,
		       coalesce(c.resource_type, c.data_type, c.kind, '') as resource_type,
		       coalesce(c.provider, c.source_system, '') as provider,
		       coalesce(c.environment, '') as environment,
		       coalesce(c.resource_id, '') as resource_id,
		       coalesce(c.arn, '') as arn,
		       coalesce(c.account_id, '') as account_id,
		       coalesce(c.region, '') as region
		ORDER BY name, id
		LIMIT $limit
	`, map[string]any{
		"service_name": serviceName,
		"limit":        limit,
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
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
		return boundedK8sResourceResult(nil, false, nil), nil
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
	return boundedK8sResourceResult(resources, len(rows) >= queryLimit, nil), nil
}

func boundedK8sResourceResult(
	contentRows []map[string]any,
	contentLowerBound bool,
	deploymentSourceRows []map[string]any,
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
	return k8sResourceResult{
		rows:              rows,
		imageRefs:         imageRefs,
		candidates:        merged,
		contentLowerBound: contentLowerBound,
		limits: map[string]any{
			"limit":                            serviceStoryItemLimit,
			"query_sentinel_limit":             serviceStoryItemLimit + 1,
			"returned_count":                   len(rows),
			"observed_count":                   observedCount,
			"observed_count_is_lower_bound":    contentLowerBound,
			"content_observed_count":           len(contentRows),
			"deployment_source_observed_count": len(deploymentSourceRows),
			"truncated":                        contentLowerBound || mergedTruncated,
			"ordering":                         []string{"repo_id", "relative_path", "entity_id"},
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
