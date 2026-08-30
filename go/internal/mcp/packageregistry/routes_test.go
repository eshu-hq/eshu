// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageregistrytools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestRouteOwnsExactlyThePackageRegistryFamily(t *testing.T) {
	t.Parallel()

	owned := []string{
		"list_package_registry_packages",
		"count_package_registry_packages",
		"get_package_registry_package_inventory",
		"list_package_registry_versions",
		"list_package_registry_dependencies",
		"list_package_registry_correlations",
	}
	for _, toolName := range owned {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", toolName)
			continue
		}
		if request.Method != "GET" {
			t.Errorf("Route(%s) method = %q, want GET", toolName, request.Method)
		}
		if request.Body != nil {
			t.Errorf("Route(%s) body = %#v, want nil", toolName, request.Body)
		}
	}

	// Neighbours in the root repository switch, plus a nonexistent name: this
	// package must claim none of them.
	for _, toolName := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_ci_cd_run_correlations",
		"list_service_catalog_correlations",
		"list_codeowners_ownership",
		"list_container_image_identities",
		"list_advisory_evidence",
		"get_repo_summary",
		"list_package_registry",
		"list_package_registry_packages_extra",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesPackageRegistryRequestContracts(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{
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
	}

	for _, tt := range []struct {
		toolName string
		want     routecontract.Request
	}{
		{
			toolName: "list_package_registry_packages",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/packages", Query: map[string]string{
				"ecosystem":  "npm",
				"limit":      "25",
				"name":       "team-api",
				"package_id": "pkg:npm://registry.example/team-api",
			}},
		},
		{
			toolName: "count_package_registry_packages",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/packages/count", Query: map[string]string{
				"ecosystem":       "npm",
				"registry":        "registry.example",
				"namespace":       "@team",
				"package_manager": "npm",
				"visibility":      "public",
			}},
		},
		{
			toolName: "get_package_registry_package_inventory",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/packages/inventory", Query: map[string]string{
				"group_by":        "registry",
				"ecosystem":       "npm",
				"registry":        "registry.example",
				"namespace":       "@team",
				"package_manager": "npm",
				"visibility":      "public",
				"limit":           "25",
				"offset":          "5",
			}},
		},
		{
			toolName: "list_package_registry_versions",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/versions", Query: map[string]string{
				"limit":      "25",
				"package_id": "pkg:npm://registry.example/team-api",
			}},
		},
		{
			toolName: "list_package_registry_dependencies",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/dependencies", Query: map[string]string{
				"after_dependency_id": "dependency-1",
				"after_version_id":    "version-1",
				"limit":               "25",
				"package_id":          "pkg:npm://registry.example/team-api",
				"version_id":          "version-2",
			}},
		},
		{
			toolName: "list_package_registry_correlations",
			want: routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/correlations", Query: map[string]string{
				"after_correlation_id": "correlation-1",
				"limit":                "25",
				"package_id":           "pkg:npm://registry.example/team-api",
				"relationship_kind":    "publication",
				"repository_id":        "repo-web",
			}},
		},
	} {
		request, handled := Route(tt.toolName, args)
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", tt.toolName)
			continue
		}
		if !reflect.DeepEqual(request, tt.want) {
			t.Errorf("Route(%s) = %#v, want %#v", tt.toolName, request, tt.want)
		}
	}
}

func TestRouteAppliesPackageRegistryDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	// Absent limit/offset fall back to the dispatcher's documented defaults,
	// and an absent group_by falls back to "ecosystem".
	for _, tt := range []struct {
		toolName string
		key      string
		want     string
	}{
		{toolName: "list_package_registry_packages", key: "limit", want: "50"},
		{toolName: "list_package_registry_versions", key: "limit", want: "50"},
		{toolName: "list_package_registry_dependencies", key: "limit", want: "50"},
		{toolName: "list_package_registry_correlations", key: "limit", want: "50"},
		{toolName: "get_package_registry_package_inventory", key: "limit", want: "100"},
		{toolName: "get_package_registry_package_inventory", key: "offset", want: "0"},
		{toolName: "get_package_registry_package_inventory", key: "group_by", want: "ecosystem"},
	} {
		request, handled := Route(tt.toolName, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.toolName)
		}
		if got := request.Query[tt.key]; got != tt.want {
			t.Errorf("Route(%s).Query[%s] = %q, want %q", tt.toolName, tt.key, got, tt.want)
		}
	}

	// An explicitly empty group_by still falls back; a non-empty one wins.
	for _, tt := range []struct {
		groupBy any
		want    string
	}{
		{groupBy: "", want: "ecosystem"},
		{groupBy: "registry", want: "registry"},
		{groupBy: 7, want: "ecosystem"}, // wrong type reads as absent
	} {
		request, _ := Route("get_package_registry_package_inventory", routecontract.Arguments{"group_by": tt.groupBy})
		if got := request.Query["group_by"]; got != tt.want {
			t.Errorf("group_by=%#v -> %q, want %q", tt.groupBy, got, tt.want)
		}
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly, including
	// float truncation toward zero and the fallback for unsupported types.
	for _, tt := range []struct {
		limit any
		want  string
	}{
		{limit: 25, want: "25"},
		{limit: int64(26), want: "26"},
		{limit: 27.9, want: "27"},
		{limit: -3.9, want: "-3"},
		{limit: 0, want: "0"},
		{limit: "25", want: "50"},
		{limit: true, want: "50"},
		{limit: nil, want: "50"},
	} {
		request, _ := Route("list_package_registry_packages", routecontract.Arguments{"limit": tt.limit})
		if got := request.Query["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %q, want %q", tt.limit, got, tt.want)
		}
	}

	// Wrong-typed and absent string arguments both read as empty, never as a
	// formatted Go value.
	request, _ := Route("list_package_registry_packages", routecontract.Arguments{"ecosystem": 42, "name": nil})
	if got := request.Query["ecosystem"]; got != "" {
		t.Errorf("ecosystem=42 -> %q, want empty", got)
	}
	if got := request.Query["name"]; got != "" {
		t.Errorf("name=nil -> %q, want empty", got)
	}
}

func TestRouteHandlesNilAndTypedNilPackageRegistryArguments(t *testing.T) {
	t.Parallel()

	var typedNil map[string]any
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	} {
		request, handled := Route("list_package_registry_packages", tt.args)
		if !handled {
			t.Fatalf("%s: Route handled = false, want true", tt.name)
		}
		want := routecontract.Request{Method: "GET", Path: "/api/v0/package-registry/packages", Query: map[string]string{
			"ecosystem":  "",
			"limit":      "50",
			"name":       "",
			"package_id": "",
		}}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route = %#v, want %#v", tt.name, request, want)
		}
	}
}

func TestRouteDoesNotAliasCallerPackageRegistryArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"ecosystem": "npm", "limit": float64(25)}
	request, handled := Route("list_package_registry_packages", args)
	if !handled {
		t.Fatal("Route handled = false, want true")
	}
	request.Query["ecosystem"] = "mutated"
	if got := args["ecosystem"]; got != "npm" {
		t.Fatalf("caller arguments mutated through the returned query: ecosystem = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("caller arguments grew to %d keys, want 2", len(args))
	}
}
