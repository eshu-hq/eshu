// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	cicdtools "github.com/eshu-hq/eshu/go/internal/mcp/cicd"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// cicdRouteTools lists every tool the child package owns, in the order the root
// repository switch used to answer them.
var cicdRouteTools = []string{
	"list_ci_cd_run_correlations",
	"count_ci_cd_run_correlations",
	"get_ci_cd_run_correlation_inventory",
}

func TestResolveRouteUsesExactCICDChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_correlation_id": "correlation-1",
			"artifact_digest":      "sha256:abc",
			"commit_sha":           "0f1e2d3c",
			"environment":          "prod",
			"group_by":             "provider",
			"image_ref":            "registry.example/team-api:1.2.3",
			"limit":                float64(25),
			"offset":               float64(5),
			"outcome":              "correlated",
			"provider":             "github_actions",
			"provider_run_id":      "run-9",
			"repository_id":        "repo-web",
			"run_id":               "run-1",
			"scope_id":             "scope-a",
		}},
		{name: "run_id fallback only", args: map[string]any{
			"run_id": "run-1",
		}},
		{name: "malformed", args: map[string]any{
			"environment":     42,
			"group_by":        []string{"provider"},
			"limit":           "25",
			"offset":          true,
			"outcome":         nil,
			"provider_run_id": struct{}{},
			"run_id":          7,
		}},
	}

	for _, tool := range cicdRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := cicdtools.Route(tool, routecontract.Arguments(tt.args))
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

// TestRepositoryRouteStillOwnsItsArmsAfterCICD proves the second delegation
// added in front of the repository switch claims only the CI/CD family and
// leaves every neighbouring arm — including the ones sharing the "count_",
// "get_", and "list_" prefixes — answered as before.
func TestRepositoryRouteStillOwnsItsArmsAfterCICD(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"get_package_registry_package_inventory",
		"list_service_catalog_correlations",
		"list_codeowners_ownership",
		"list_container_image_identities",
		"count_container_image_identities",
		"get_container_image_identity_inventory",
		"list_advisory_evidence",
		"get_repository_stats",
	} {
		if _, handled := cicdRoute(tool, map[string]any{}); handled {
			t.Errorf("cicdRoute(%s) handled = true, want false", tool)
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

// TestCICDRouteRejectsNonFamilyTools mutation-proves the child selector through
// the adapter: every owned name is claimed, and near-miss names are not.
func TestCICDRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range cicdRouteTools {
		if _, handled := cicdRoute(tool, map[string]any{}); !handled {
			t.Errorf("cicdRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_ci_cd_run_correlation", "list_ci_cd_run_correlations_extra",
		"count_ci_cd_run_correlation", "get_ci_cd_run_correlation_inventories",
		"get_ci_cd_run_inventory", "list_ci_cd_runs",
		"LIST_CI_CD_RUN_CORRELATIONS",
	} {
		if _, handled := cicdRoute(tool, map[string]any{}); handled {
			t.Errorf("cicdRoute(%q) handled = true, want false", tool)
		}
	}
}
