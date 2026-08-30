// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	packageregistrytools "github.com/eshu-hq/eshu/go/internal/mcp/packageregistry"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// packageRegistryRouteTools lists every tool the child package owns, in the order
// the root repository switch used to answer them.
var packageRegistryRouteTools = []string{
	"list_package_registry_packages",
	"count_package_registry_packages",
	"get_package_registry_package_inventory",
	"list_package_registry_versions",
	"list_package_registry_dependencies",
	"list_package_registry_correlations",
}

func TestResolveRouteUsesExactPackageRegistryChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_correlation_id": "correlation-1",
			"after_dependency_id":  "dependency-1",
			"after_version_id":     "version-1",
			"ecosystem":            "npm",
			"group_by":             "registry",
			"limit":                float64(25),
			"name":                 "team-api",
			"namespace":            "@team",
			"offset":               float64(5),
			"package_id":           "pkg:npm://registry.example/team-api",
			"package_manager":      "npm",
			"registry":             "registry.example",
			"relationship_kind":    "publication",
			"repository_id":        "repo-web",
			"version_id":           "version-2",
			"visibility":           "public",
		}},
		{name: "malformed", args: map[string]any{
			"ecosystem":  42,
			"group_by":   []string{"registry"},
			"limit":      "25",
			"name":       nil,
			"offset":     true,
			"package_id": struct{}{},
		}},
	}

	for _, tool := range packageRegistryRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := packageregistrytools.Route(tool, routecontract.Arguments(tt.args))
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

// TestRepositoryRouteStillOwnsItsRemainingArms proves the delegation added in
// front of the repository switch claims only the package-registry family and
// leaves every neighbouring arm — including the ones sharing the "count_" and
// "list_" prefixes — answered by the root switch exactly as before.
func TestRepositoryRouteStillOwnsItsRemainingArms(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"list_service_catalog_correlations",
		"list_codeowners_ownership",
		"list_container_image_identities",
		"list_advisory_evidence",
		"get_repository_stats",
	} {
		if _, handled := packageRegistryRoute(tool, map[string]any{}); handled {
			t.Errorf("packageRegistryRoute(%s) handled = true, want false", tool)
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

// TestPackageRegistryRouteRejectsNonFamilyTools mutation-proves the child
// selector: every owned name is claimed, and near-miss names are not.
func TestPackageRegistryRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range packageRegistryRouteTools {
		if _, handled := packageRegistryRoute(tool, map[string]any{}); !handled {
			t.Errorf("packageRegistryRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_package_registry", "list_package_registry_package",
		"list_package_registry_packages_extra", "count_package_registry_versions",
		"get_package_registry_inventory", "LIST_PACKAGE_REGISTRY_PACKAGES",
	} {
		if _, handled := packageRegistryRoute(tool, map[string]any{}); handled {
			t.Errorf("packageRegistryRoute(%q) handled = true, want false", tool)
		}
	}
}
