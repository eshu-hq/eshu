// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

func deploymentConfigCoverageBounds(deploymentSourceLimits, k8sResourceLimits map[string]any) (bool, bool) {
	deploymentSourceTruncated, deploymentSourceLowerBound, deploymentSourceAvailable := deploymentConfigBoundState(deploymentSourceLimits, false)
	k8sTruncated, k8sLowerBound, k8sAvailable := deploymentConfigBoundState(k8sResourceLimits, true)
	return deploymentSourceTruncated || k8sTruncated || !deploymentSourceAvailable || !k8sAvailable,
		deploymentSourceLowerBound || k8sLowerBound || !deploymentSourceAvailable || !k8sAvailable
}

func deploymentConfigBoundState(limits map[string]any, requireDeploymentSourceProbe bool) (bool, bool, bool) {
	truncated, truncatedOK := limits["truncated"].(bool)
	lowerBound, lowerBoundOK := limits["observed_count_is_lower_bound"].(bool)
	limit, limitOK := limits["limit"].(int)
	querySentinel, querySentinelOK := limits["query_sentinel_limit"].(int)
	returned, returnedOK := limits["returned_count"].(int)
	observed, observedOK := limits["observed_count"].(int)
	ordering, orderingOK := limits["ordering"].([]string)
	available := truncatedOK && lowerBoundOK && limitOK && querySentinelOK && returnedOK && observedOK && orderingOK &&
		limit > 0 && querySentinel == limit+1 && returned >= 0 && returned <= limit && observed >= returned && len(ordering) > 0 &&
		truncated == (lowerBound || observed > returned)
	if requireDeploymentSourceProbe {
		deploymentSourceSentinel, deploymentSourceSentinelOK := limits["deployment_source_query_sentinel_limit"].(int)
		deploymentSourceCount, deploymentSourceCountOK := limits["deployment_source_observed_count"].(int)
		deploymentSourceLowerBound, deploymentSourceLowerBoundOK := limits["deployment_source_observed_count_is_lower_bound"].(bool)
		contentCount, contentCountOK := limits["content_observed_count"].(int)
		contentLowerBound, contentLowerBoundOK := limits["content_observed_count_is_lower_bound"].(bool)
		available = available && deploymentSourceSentinelOK && deploymentSourceCountOK && deploymentSourceLowerBoundOK && contentCountOK && contentLowerBoundOK &&
			deploymentSourceSentinel > 0 && deploymentSourceCount >= 0 && contentCount >= 0 && lowerBound == (contentLowerBound || deploymentSourceLowerBound)
		return truncated, lowerBound, available
	}
	canonicalCount, canonicalCountOK := limits["canonical_observed_count"].(int)
	repositoryCount, repositoryCountOK := limits["repository_observed_count"].(int)
	available = available && canonicalCountOK && repositoryCountOK && canonicalCount >= 0 && repositoryCount >= 0
	return truncated, lowerBound, available
}

func deploymentConfigLimitations(
	environment string,
	artifacts []map[string]any,
	valuesLayers []map[string]any,
	targets []map[string]any,
	deploymentSourceLimits map[string]any,
	k8sResourceLimits map[string]any,
) []string {
	limitations := []string{}
	deploymentSourceTruncated, deploymentSourceLowerBound, deploymentSourceAvailable := deploymentConfigBoundState(deploymentSourceLimits, false)
	k8sTruncated, k8sLowerBound, k8sAvailable := deploymentConfigBoundState(k8sResourceLimits, true)
	if !deploymentSourceAvailable {
		limitations = append(limitations, "deployment_source_limits_unavailable")
	} else if deploymentSourceTruncated {
		limitations = append(limitations, "deployment_source_evidence_truncated")
	}
	if deploymentSourceLowerBound {
		limitations = append(limitations, "deployment_source_evidence_lower_bound")
	}
	if !k8sAvailable {
		limitations = append(limitations, "k8s_resource_limits_unavailable")
	} else if k8sTruncated {
		limitations = append(limitations, "k8s_resource_evidence_truncated")
	}
	if k8sLowerBound {
		limitations = append(limitations, "k8s_resource_evidence_lower_bound")
	}
	if environment != "" {
		limitations = append(limitations, "Rows without explicit environment are retained because shared Helm or ArgoCD layers can still apply to the requested environment.")
	}
	if len(artifacts) == 0 {
		limitations = append(limitations, "No deployment configuration artifacts were materialized for this service.")
	}
	if len(valuesLayers) == 0 {
		limitations = append(limitations, "No Helm, Kustomize, ArgoCD, or Terraform values layer was found in the indexed evidence.")
	}
	if len(targets) == 0 {
		limitations = append(limitations, "No rendered Kubernetes or controller target was found in the indexed evidence.")
	}
	return limitations
}
