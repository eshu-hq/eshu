// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/truth"
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
	hasLiveEvidence bool,
) map[string]any {
	overallConfidence, confidenceReason := deploymentOverallConfidence(instances, deploymentSources, configEnvironments, hasLiveEvidence)
	// #5638 TIER GUARDRAIL: hasLiveEvidence is the ONLY signal that can
	// promote the deployment truth tier or the confidence reason to a live
	// tier. live_instance_count (attached separately below by the caller,
	// impact_trace_deployment_response.go) is a read-side observation
	// derived from the SAME identity-bound facts, but it is NEVER passed to
	// ClassifyDeploymentTruthTier or deploymentOverallConfidence -- a count
	// present with hasLiveEvidence=false must still classify config_only
	// with a non-live confidence reason (TestBuildDeploymentFactSummaryTierConfigOnly).
	tier := truth.ClassifyDeploymentTruthTier(hasLiveEvidence, len(instances) > 0, len(deploymentSources) > 0, len(configEnvironments) > 0)
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
	if tier != "" {
		summary["deployment_truth_tier"] = string(tier)
	}
	// #5638: live_instance_count is attached here (read from workloadContext,
	// set by the handler at ctx["_live_instance_count"]) so every
	// deployment_fact_summary field originates from this one builder,
	// matching how hasLiveEvidence itself arrives via
	// workloadContext["_has_live_evidence"]
	// (impact_trace_deployment_response.go). Conditional like the tier key:
	// emitted only when fetchWorkloadLiveInstanceSummary produced an actual
	// observation (>= 1 matched fact carried a replica count), never a
	// fabricated zero.
	if count, ok := workloadContext["_live_instance_count"].(int); ok {
		summary["live_instance_count"] = count
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
	hasLiveEvidence bool,
) (float64, string) {
	// Live runtime observation is the strongest evidence tier — it means a
	// kubernetes_live correlation (or equivalent live observation) confirmed
	// the workload is running. The confidence is calibrated at 0.95: higher
	// than the materialized-runtime-instances baseline (0.9) because live
	// evidence is a direct observation, not a config-derived inference.
	if hasLiveEvidence {
		return 0.95, "live_runtime_observation"
	}
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

const cloudResourceObservationLimit = serviceStoryItemLimit * serviceStoryItemLimit

func (h *ImpactHandler) fetchCloudResourceResult(
	ctx context.Context,
	repoID string,
	workloadID string,
) (cloudResourceResult, error) {
	queryLimit := serviceStoryItemLimit + 1
	repoID = strings.TrimSpace(repoID)
	workloadID = strings.TrimSpace(workloadID)
	if h == nil || h.Neo4j == nil || repoID == "" || workloadID == "" {
		return boundedCloudResourceResult(nil, queryLimit), nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	// WorkloadInstance and USES relationships are global today and do not
	// carry repository ownership. A repository-scoped token therefore cannot
	// prove that a reachable cloud observation belongs to its grant. Omit the
	// limits with the rows so consumers cannot misread withheld evidence as an
	// exact empty collection.
	if access.scoped() {
		return cloudResourceResult{rows: []map[string]any{}}, nil
	}
	params := access.graphParams(map[string]any{
		"repo_id":                 repoID,
		"workload_id":             workloadID,
		"cloud_observation_limit": cloudResourceObservationLimit + 1,
	})
	rows, err := h.Neo4j.Run(ctx, fmt.Sprintf(`
		MATCH (repo:Repository)-[:DEFINES]->(w:Workload {id: $workload_id})<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:USES]->(c:CloudResource)
		WHERE repo.id = $repo_id%s
		WITH c, i, rel,
		     c.name AS sort_name, c.id AS sort_id,
		     rel.confidence AS sort_confidence,
		     rel.stable_fact_key AS sort_stable_fact_key,
		     rel.source_fact_id AS sort_source_fact_id,
		     rel.source_system AS sort_source_system,
		     rel.source_record_id AS sort_source_record_id,
		     i.environment AS sort_instance_environment
		ORDER BY sort_name, sort_id, sort_confidence DESC,
		         sort_stable_fact_key, sort_source_fact_id,
		         sort_source_system, sort_source_record_id,
		         sort_instance_environment
		LIMIT $cloud_observation_limit
		RETURN c.id as id, c.name as name, c.kind as kind, c.provider as provider,
		       properties(rel) as observation,
		       i.environment as instance_environment,
		       c.environment as resource_environment
	`, access.graphPredicate("repo")), params)
	if err != nil {
		return cloudResourceResult{}, err
	}
	observationCount := 0
	for _, row := range rows {
		if observations := mapSliceValue(row, "observations"); len(observations) > 0 {
			observationCount += IntVal(row, "observation_count")
			continue
		}
		observationCount++
	}
	resources, err := deploymentTraceCloudResourcesFromRows(rows, "")
	if err != nil {
		return cloudResourceResult{}, err
	}
	return boundedCloudResourceResultWithObservationState(
		resources,
		queryLimit,
		observationCount,
		observationCount > cloudResourceObservationLimit ||
			len(rows) > cloudResourceObservationLimit || len(resources) >= queryLimit,
	), nil
}

func boundedCloudResourceResult(rows []map[string]any, queryLimit int) cloudResourceResult {
	return boundedCloudResourceResultWithObservationState(rows, queryLimit, 0, false)
}

func boundedCloudResourceResultWithObservationState(
	rows []map[string]any,
	queryLimit int,
	observationCount int,
	observationsTruncated bool,
) cloudResourceResult {
	returned, truncated := capMapRows(rows, serviceStoryItemLimit)
	truncated = truncated || observationsTruncated
	limits := boundedCollectionMetadata(
		serviceStoryItemLimit, queryLimit, len(returned), len(rows), truncated,
		[]string{"name", "id"},
	)
	limits["observation_limit"] = cloudResourceObservationLimit
	limits["observation_query_sentinel_limit"] = cloudResourceObservationLimit + 1
	limits["observation_count"] = observationCount
	limits["observation_count_is_lower_bound"] = observationsTruncated
	return cloudResourceResult{
		rows:   returned,
		limits: limits,
	}
}

type k8sResourceResult struct {
	rows              []map[string]any
	imageRefs         []string
	limits            map[string]any
	candidates        []map[string]any
	contentLowerBound bool
	// selectCandidatePoolTruncated is true when the directed SELECTS candidate
	// scan (ListRepoK8sSelectCandidates) hit the repositorySemanticEntityLimit
	// ceiling, so some selector-matching Services may be missing from the
	// surfaced pool. It drives the k8s_relationships_complete=false disclosure
	// (see boundedK8sResourceResult) and must be threaded back through any
	// re-merge (the handler and deployment-config-influence re-call
	// boundedK8sResourceResult with deployment-source rows).
	selectCandidatePoolTruncated bool
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
		return boundedK8sResourceResult(nil, false, nil, false, false), nil
	}

	queryLimit := serviceStoryItemLimit + 1
	rows, err := h.Content.SearchEntitiesByName(ctx, repoID, "K8sResource", workloadName, queryLimit)
	if err != nil {
		return k8sResourceResult{}, err
	}

	// Phase 1: the name-anchored surfaced pool. This IS the historical wire
	// response (rows whose entity_name equals the traced workload's name), kept
	// unchanged. While building it, prepare a directed match target for each
	// anchored Deployment so the phase-2 candidate scan parses each workload's
	// pod-template labels exactly once.
	resources := make([]map[string]any, 0, len(rows))
	surfaced := make(map[string]struct{}, len(rows))
	targets := make([]anchoredDeploymentTarget, 0, 1)
	for _, row := range rows {
		if row.EntityName != workloadName {
			continue
		}
		resources = append(resources, k8sResourceWireRow(row))
		surfaced[row.EntityID] = struct{}{}
		if isK8sResourceKind(row, "Deployment") {
			targets = append(targets, anchoredDeploymentTarget{
				entityID: row.EntityID,
				target:   newK8sWorkloadMatchTarget(k8sSelectMatchInputFromEntity(row)),
			})
		}
	}
	contentLowerBound := len(rows) >= queryLimit

	// Phase 2: the directed, matcher-only candidate scan. Only Services that
	// ACTUALLY selector-match an anchored Deployment are hydrated (by ID, wide
	// shape) and joined to the surfaced pool -- a differently-named Service is
	// discovered here (the #5363 under-linking fix) without any unmatched
	// candidate ever touching the wire.
	matchedIDs, candidatePoolTruncated, err := h.fetchK8sSelectMatchedServiceIDs(ctx, repoID, targets, surfaced)
	if err != nil {
		return k8sResourceResult{}, err
	}
	if len(matchedIDs) > 0 {
		hydrated, err := h.Content.ListRepoEntitiesByIDs(ctx, repoID, matchedIDs, len(matchedIDs))
		if err != nil {
			return k8sResourceResult{}, err
		}
		for _, row := range hydrated {
			if _, ok := surfaced[row.EntityID]; ok {
				continue
			}
			resources = append(resources, k8sResourceWireRow(row))
			surfaced[row.EntityID] = struct{}{}
		}
	}

	return boundedK8sResourceResult(resources, contentLowerBound, nil, false, candidatePoolTruncated), nil
}

func boundedK8sResourceResult(
	contentRows []map[string]any,
	contentLowerBound bool,
	deploymentSourceRows []map[string]any,
	deploymentSourceLowerBound bool,
	selectCandidatePoolTruncated bool,
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
	limits := map[string]any{
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
		// Directed SELECTS candidate-scan completeness (#5363). These two keys
		// are always present so operators and clients can read the SELECTS
		// completeness of every response, not only the truncated ones; they are
		// additive (existing keys and their values are unchanged), so a repo
		// that surfaces no new match stays byte-identical on every pre-existing
		// key. When the candidate pool truncated at the ceiling, the
		// machine-readable reason is added and k8s_relationships_complete is
		// false.
		"k8s_select_candidate_sentinel_limit": repositorySemanticEntityLimit + 1,
		"k8s_relationships_complete":          !selectCandidatePoolTruncated,
	}
	if selectCandidatePoolTruncated {
		limits["k8s_relationships_incomplete_reason"] = k8sSelectCandidatePoolTruncationReason
	}
	return k8sResourceResult{
		rows:                         rows,
		imageRefs:                    imageRefs,
		candidates:                   merged,
		contentLowerBound:            contentLowerBound,
		selectCandidatePoolTruncated: selectCandidatePoolTruncated,
		limits:                       limits,
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
