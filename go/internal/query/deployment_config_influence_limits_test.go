// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "testing"

func deploymentConfigExactDeploymentSourceLimits() map[string]any {
	return map[string]any{
		"limit":                         contextStoryItemLimit,
		"query_sentinel_limit":          contextStoryItemLimit + 1,
		"returned_count":                1,
		"observed_count":                1,
		"observed_count_is_lower_bound": false,
		"canonical_observed_count":      1,
		"repository_observed_count":     0,
		"truncated":                     false,
		"ordering":                      []string{"relationship_type_priority"},
	}
}

func deploymentConfigExactK8sResourceLimits() map[string]any {
	return map[string]any{
		"limit":                                           serviceStoryItemLimit,
		"query_sentinel_limit":                            serviceStoryItemLimit + 1,
		"deployment_source_query_sentinel_limit":          repositorySemanticEntityLimit + 1,
		"returned_count":                                  1,
		"observed_count":                                  1,
		"observed_count_is_lower_bound":                   false,
		"content_observed_count":                          1,
		"content_observed_count_is_lower_bound":           false,
		"deployment_source_observed_count":                0,
		"deployment_source_observed_count_is_lower_bound": false,
		"truncated":                                       false,
		"ordering":                                        []string{"repo_id"},
	}
}

func TestBuildDeploymentConfigInfluenceResponseFailsClosedWithoutStructuredLimits(t *testing.T) {
	t.Parallel()

	response := buildDeploymentConfigInfluenceResponse(deploymentConfigInfluenceRequest{ServiceName: "payments-api"}, map[string]any{
		"id":   "workload:payments-api",
		"name": "payments-api",
	})
	coverage := mapValue(response, "coverage")
	if !BoolVal(coverage, "truncated") || !BoolVal(coverage, "observed_count_is_lower_bound") {
		t.Fatalf("coverage = %#v, want fail-closed truncation and lower-bound disclosure", coverage)
	}
	limitations := StringSliceVal(response, "limitations")
	for _, limitation := range []string{"deployment_source_limits_unavailable", "k8s_resource_limits_unavailable"} {
		if !containsString(limitations, limitation) {
			t.Fatalf("limitations = %#v, want %q", limitations, limitation)
		}
	}
}

func TestBuildDeploymentConfigInfluenceResponseFailsClosedWithMalformedStructuredLimits(t *testing.T) {
	t.Parallel()

	response := buildDeploymentConfigInfluenceResponse(deploymentConfigInfluenceRequest{ServiceName: "payments-api"}, map[string]any{
		"id":                       "workload:payments-api",
		"name":                     "payments-api",
		"deployment_source_limits": deploymentConfigExactDeploymentSourceLimits(),
		"k8s_resource_limits": map[string]any{
			"truncated":                     false,
			"observed_count_is_lower_bound": true,
		},
	})
	coverage := mapValue(response, "coverage")
	if !BoolVal(coverage, "truncated") || !BoolVal(coverage, "observed_count_is_lower_bound") {
		t.Fatalf("coverage = %#v, want malformed metadata to fail closed", coverage)
	}
	if limitations := StringSliceVal(response, "limitations"); !containsString(limitations, "k8s_resource_limits_unavailable") {
		t.Fatalf("limitations = %#v, want k8s_resource_limits_unavailable", limitations)
	}
}

func TestBuildDeploymentConfigInfluenceResponseFailsClosedWithContradictoryStructuredLimits(t *testing.T) {
	t.Parallel()

	k8sLimits := deploymentConfigExactK8sResourceLimits()
	k8sLimits["deployment_source_observed_count_is_lower_bound"] = true
	response := buildDeploymentConfigInfluenceResponse(deploymentConfigInfluenceRequest{ServiceName: "payments-api"}, map[string]any{
		"id":                       "workload:payments-api",
		"name":                     "payments-api",
		"deployment_source_limits": deploymentConfigExactDeploymentSourceLimits(),
		"k8s_resource_limits":      k8sLimits,
	})
	coverage := mapValue(response, "coverage")
	if !BoolVal(coverage, "truncated") || !BoolVal(coverage, "observed_count_is_lower_bound") {
		t.Fatalf("coverage = %#v, want contradictory metadata to fail closed", coverage)
	}
	if limitations := StringSliceVal(response, "limitations"); !containsString(limitations, "k8s_resource_limits_unavailable") {
		t.Fatalf("limitations = %#v, want k8s_resource_limits_unavailable", limitations)
	}
}

func TestDeploymentConfigBoundStateRejectsInconsistentFamilyCountsAndSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                         string
		limits                       map[string]any
		requireDeploymentSourceProbe bool
	}{
		{
			name: "deployment source observed count exceeds constituent counts",
			limits: func() map[string]any {
				limits := deploymentConfigExactDeploymentSourceLimits()
				limits["observed_count"] = 2
				return limits
			}(),
		},
		{
			name: "kubernetes observed count exceeds constituent counts",
			limits: func() map[string]any {
				limits := deploymentConfigExactK8sResourceLimits()
				limits["observed_count"] = 2
				return limits
			}(),
			requireDeploymentSourceProbe: true,
		},
		{
			name: "kubernetes deployment source sentinel differs from producer contract",
			limits: func() map[string]any {
				limits := deploymentConfigExactK8sResourceLimits()
				limits["deployment_source_query_sentinel_limit"] = repositorySemanticEntityLimit + 2
				return limits
			}(),
			requireDeploymentSourceProbe: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, _, available := deploymentConfigBoundState(test.limits, test.requireDeploymentSourceProbe)
			if available {
				t.Fatalf("deploymentConfigBoundState(%#v) available = true, want fail-closed false", test.limits)
			}
		})
	}
}
