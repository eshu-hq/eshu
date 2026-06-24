// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsObservabilityCoverageCorrelationsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_observability_coverage_correlations", map[string]any{
		"after_correlation_id":     "observability-coverage-1",
		"scope_id":                 "aws-account://111122223333",
		"provider":                 "aws",
		"coverage_signal":          "alarm",
		"observability_object_ref": "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
		"target_uid":               "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
		"target_service_ref":       "checkout",
		"source_class":             "declared",
		"resource_class":           "dashboard",
		"outcome":                  "exact",
		"coverage_status":          "covered",
		"limit":                    float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/observability/coverage/correlations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"after_correlation_id":     "observability-coverage-1",
		"scope_id":                 "aws-account://111122223333",
		"provider":                 "aws",
		"coverage_signal":          "alarm",
		"observability_object_ref": "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
		"target_uid":               "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
		"target_service_ref":       "checkout",
		"source_class":             "declared",
		"resource_class":           "dashboard",
		"outcome":                  "exact",
		"coverage_status":          "covered",
		"limit":                    "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}
