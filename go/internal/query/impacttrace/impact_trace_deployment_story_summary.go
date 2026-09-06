// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/truth"
)

// Deployment-trace fact-summary builders for the trace_deployment_chain
// response surface. Split from impact_trace_deployment_story_facts.go to
// honor the 500-line file cap (#6060 lane B2); the section/fact builders
// stay there. None of these builders issue a graph Run or RunSingle call,
// so none are tracked by the query-source-coverage gate.
func BuildDeploymentFactSummary(
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
	overallConfidence, confidenceReason := DeploymentOverallConfidence(instances, deploymentSources, configEnvironments, hasLiveEvidence)
	// #5638 TIER GUARDRAIL: hasLiveEvidence is the ONLY signal that can
	// promote the deployment truth tier or the confidence reason to a live
	// tier. live_instance_count (attached separately below by the caller,
	// impact_trace_deployment_response.go) is a read-side observation
	// derived from the SAME identity-bound facts, but it is NEVER passed to
	// ClassifyDeploymentTruthTier or DeploymentOverallConfidence -- a count
	// present with hasLiveEvidence=false must still classify config_only
	// with a non-live confidence reason (TestBuildDeploymentFactSummaryTierConfigOnly).
	tier := truth.ClassifyDeploymentTruthTier(hasLiveEvidence, len(instances) > 0, len(deploymentSources) > 0, len(configEnvironments) > 0)
	uncorrelatedCloudResources := querycontract.MapSliceValue(workloadContext, "uncorrelated_cloud_resources")
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
		"has_repository":                 querycontract.SafeStr(workloadContext, "repo_id") != "",
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
		// #5663: truncated is a flat sibling field of live_instance_count,
		// always present (as false or true) whenever live_instance_count
		// itself is present, mirroring the always-present-within-its-parent
		// convention the gitops *_limits "truncated" fields use -- read
		// from workloadContext["_live_instance_count_truncated"] (set by the
		// handler in lockstep with "_live_instance_count",
		// impact_trace_deployment.go) rather than as a builder parameter, to
		// match how live_instance_count itself already arrives here.
		summary["live_instance_count_truncated"], _ = workloadContext["_live_instance_count_truncated"].(bool)
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

func DeploymentOverallConfidence(
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
			confidence := querycontract.FirstPositiveFloat(
				querycontract.FloatVal(instance, "materialization_confidence"),
				querycontract.FloatVal(instance, "platform_confidence"),
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
			confidence := querycontract.FloatVal(source, "confidence")
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
		DeploymentTraceEvidenceControllerFamilies(deploymentSources, deploymentEvidence, controllerEntities),
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
		name := querycontract.StringVal(entity, "entity_name")
		if name == "" {
			name = querycontract.StringVal(entity, "entity_id")
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
		kind := querycontract.StringVal(entity, "controller_kind")
		if kind == "" {
			kind = ControllerEntityTypes[querycontract.StringVal(entity, "entity_type")]
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

// OciDigestMatchStrength is the OCI match strength that counts as a
// canonical image match. It moved here from the impact package with lane B2
// of #6060 with canonicalOCIImageMatchCount. See #6060.
const OciDigestMatchStrength = "canonical_digest"

func canonicalOCIImageMatchCount(rows []map[string]any) int {
	count := 0
	for _, row := range rows {
		if querycontract.StringVal(row, "match_strength") == OciDigestMatchStrength {
			count++
		}
	}
	return count
}
