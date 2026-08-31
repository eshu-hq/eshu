// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	observabilitycoveragetools "github.com/eshu-hq/eshu/go/internal/mcp/observabilitycoverage"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// observabilityCoverageRouteTools lists every tool the child package owns.
var observabilityCoverageRouteTools = []string{"list_observability_coverage_correlations"}

// observabilityCoverageQueryKeys is the twelve-key query the correlations
// listing must still send through dispatch.
var observabilityCoverageQueryKeys = []string{
	"after_correlation_id",
	"coverage_signal",
	"coverage_status",
	"limit",
	"observability_object_ref",
	"outcome",
	"provider",
	"resource_class",
	"scope_id",
	"source_class",
	"target_service_ref",
	"target_uid",
}

func TestResolveRouteUsesExactObservabilityCoverageChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_correlation_id":     "observability-coverage-1",
			"coverage_signal":          "alarm",
			"coverage_status":          "covered",
			"limit":                    float64(25),
			"observability_object_ref": "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
			"outcome":                  "exact",
			"provider":                 "aws",
			"resource_class":           "dashboard",
			"scope_id":                 "aws-account://111122223333",
			"source_class":             "declared",
			"target_service_ref":       "checkout",
			"target_uid":               "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
		}},
		{name: "scope only", args: map[string]any{"scope_id": "scope-a"}},
		{name: "malformed", args: map[string]any{
			"coverage_signal": 42,
			"limit":           "25",
			"outcome":         nil,
			"provider":        []string{"aws"},
			"scope_id":        struct{}{},
			"target_uid":      true,
		}},
	}

	for _, tool := range observabilityCoverageRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := observabilitycoveragetools.Route(tool, routecontract.Arguments(tt.args))
			if !handled {
				t.Fatalf("child Route(%s) handled = false, want true", tool)
			}
			want := &route{
				method: request.Method,
				path:   request.Path,
				body:   request.Body,
				query:  request.Query,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolveRoute(%s, %s) = %#v, want child request %#v", tool, tt.name, got, want)
			}
		}
	}
}

// TestObservabilityCoverageDispatchKeepsEveryQueryKey proves the twelve
// filters survive the adapter boundary, where the handler actually reads them.
// Dropping one fails two different ways: limit and the scope anchor are
// required, so losing either returns 400, while losing a plain filter returns
// 200 and widens the caller's page to rows they filtered out. Both shapes are
// why each key is asserted by name and the total count is pinned.
func TestObservabilityCoverageDispatchKeepsEveryQueryKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"after_correlation_id":     "observability-coverage-1",
		"coverage_signal":          "alarm",
		"coverage_status":          "covered",
		"limit":                    float64(25),
		"observability_object_ref": "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
		"outcome":                  "exact",
		"provider":                 "aws",
		"resource_class":           "dashboard",
		"scope_id":                 "aws-account://111122223333",
		"source_class":             "declared",
		"target_service_ref":       "checkout",
		"target_uid":               "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
	}
	want := map[string]string{
		"after_correlation_id":     "observability-coverage-1",
		"coverage_signal":          "alarm",
		"coverage_status":          "covered",
		"limit":                    "25",
		"observability_object_ref": "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
		"outcome":                  "exact",
		"provider":                 "aws",
		"resource_class":           "dashboard",
		"scope_id":                 "aws-account://111122223333",
		"source_class":             "declared",
		"target_service_ref":       "checkout",
		"target_uid":               "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
	}

	got, err := resolveRoute("list_observability_coverage_correlations", args)
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got.method != "GET" {
		t.Errorf("method = %q, want GET", got.method)
	}
	if got.path != "/api/v0/observability/coverage/correlations" {
		t.Errorf("path = %q, want the coverage correlations path", got.path)
	}
	if got.body != nil {
		t.Errorf("body = %#v, want nil", got.body)
	}
	if n, wantN := len(got.query), len(observabilityCoverageQueryKeys); n != wantN {
		t.Fatalf("query carries %d keys (%#v), want %d", n, got.query, wantN)
	}
	for _, key := range observabilityCoverageQueryKeys {
		value, present := got.query[key]
		if !present {
			t.Errorf("dispatch dropped %q entirely", key)
			continue
		}
		if value != want[key] {
			t.Errorf("query[%s] = %q, want %q", key, value, want[key])
		}
	}
	for _, key := range []string{"offset", "group_by", "repository_id", "scope"} {
		if value, present := got.query[key]; present {
			t.Errorf("query carries %q = %q, want the key absent", key, value)
		}
	}

	// The limit default reaches the handler unchanged when the caller omits it.
	bare, err := resolveRoute("list_observability_coverage_correlations", map[string]any{"scope_id": "scope-a"})
	if err != nil {
		t.Fatalf("resolveRoute(scope only) error = %v, want nil", err)
	}
	if value := bare.query["limit"]; value != "50" {
		t.Errorf("absent limit -> %q, want the default 50", value)
	}
}

// TestRepositoryRouteStillOwnsItsArmsAfterObservabilityCoverage proves the
// fifth delegation added in front of the repository switch claims only this
// family and leaves every neighbouring arm — including the ones sharing the
// "count_", "get_", and "list_" prefixes — answered as before.
func TestRepositoryRouteStillOwnsItsArmsAfterObservabilityCoverage(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"get_package_registry_package_inventory",
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"get_ci_cd_run_correlation_inventory",
		"list_codeowners_ownership",
		"list_secrets_iam_posture_gaps",
		"count_secrets_iam_posture",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_container_image_identities",
		"count_container_image_identities",
		"get_container_image_identity_inventory",
		"list_advisory_evidence",
		"get_repository_stats",
	} {
		if _, handled := observabilityCoverageRoute(tool, map[string]any{}); handled {
			t.Errorf("observabilityCoverageRoute(%s) handled = true, want false", tool)
		}
		got, ok, err := repositoryRoute(tool, map[string]any{})
		if err != nil {
			t.Errorf("repositoryRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if !ok || got == nil {
			t.Errorf("repositoryRoute(%s) ok = %v, route = %v, want a route", tool, ok, got)
		}
	}

	// An unknown tool still falls through the repository switch untouched.
	if got, ok, err := repositoryRoute("not_a_tool", map[string]any{}); ok || got != nil || err != nil {
		t.Fatalf("repositoryRoute(not_a_tool) = (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestObservabilityCoverageRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the owned name is claimed, and near-miss names
// are not.
func TestObservabilityCoverageRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range observabilityCoverageRouteTools {
		if _, handled := observabilityCoverageRoute(tool, map[string]any{}); !handled {
			t.Errorf("observabilityCoverageRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_observability_coverage",
		"list_observability_coverage_correlation",
		"list_observability_coverage_correlations_extra",
		"list_observability_coverage_gaps",
		"count_observability_coverage_correlations",
		"get_observability_coverage_correlations",
		"get_observability_coverage_inventory",
		"observability_coverage_correlations",
		"LIST_OBSERVABILITY_COVERAGE_CORRELATIONS",
	} {
		if _, handled := observabilityCoverageRoute(tool, map[string]any{}); handled {
			t.Errorf("observabilityCoverageRoute(%q) handled = true, want false", tool)
		}
	}
}
