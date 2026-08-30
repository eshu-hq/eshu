// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	codeownerstools "github.com/eshu-hq/eshu/go/internal/mcp/codeowners"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// codeownersRouteTools lists every tool the child package owns, in the order
// the root repository switch used to answer them.
var codeownersRouteTools = []string{
	"list_codeowners_ownership",
}

func TestResolveRouteUsesExactCodeownersChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_order_index": float64(12),
			"after_pattern":     "/services/api/",
			"after_ref":         "@eshu-hq/platform",
			"limit":             float64(25),
			"repository_id":     "repo-web",
		}},
		{name: "cursor legs without the order index", args: map[string]any{
			"after_pattern": "/services/api/",
			"after_ref":     "@eshu-hq/platform",
		}},
		{name: "explicitly nil order index", args: map[string]any{
			"after_order_index": nil,
		}},
		{name: "malformed", args: map[string]any{
			"after_order_index": "12",
			"after_pattern":     42,
			"after_ref":         []string{"@eshu-hq/platform"},
			"limit":             true,
			"repository_id":     struct{}{},
		}},
	}

	for _, tool := range codeownersRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := codeownerstools.Route(tool, routecontract.Arguments(tt.args))
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

// TestRepositoryRouteStillOwnsItsArmsAfterCodeowners proves the third
// delegation added in front of the repository switch claims only the
// CODEOWNERS family and leaves every neighbouring arm — including the ones
// sharing the "count_", "get_", and "list_" prefixes — answered as before.
func TestRepositoryRouteStillOwnsItsArmsAfterCodeowners(t *testing.T) {
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
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_container_image_identities",
		"count_container_image_identities",
		"get_container_image_identity_inventory",
		"list_advisory_evidence",
		"get_repository_stats",
	} {
		if _, handled := codeownersRoute(tool, map[string]any{}); handled {
			t.Errorf("codeownersRoute(%s) handled = true, want false", tool)
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

// TestCodeownersRouteRejectsNonFamilyTools mutation-proves the child selector
// through the adapter: every owned name is claimed, and near-miss names are not.
func TestCodeownersRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range codeownersRouteTools {
		if _, handled := codeownersRoute(tool, map[string]any{}); !handled {
			t.Errorf("codeownersRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"", "list_codeowners_ownerships", "list_codeowners_ownership_extra",
		"list_codeowners", "count_codeowners_ownership", "get_codeowners_ownership",
		"get_codeowners_ownership_inventory", "LIST_CODEOWNERS_OWNERSHIP",
	} {
		if _, handled := codeownersRoute(tool, map[string]any{}); handled {
			t.Errorf("codeownersRoute(%q) handled = true, want false", tool)
		}
	}
}

// TestCodeownersRouteKeepsTheAbsentCursorLegEmptyThroughDispatch carries the
// child's cursor rule across the adapter boundary, where the handler actually
// sees it: an absent after_order_index reaches the query layer as an empty
// value, not as a seek from order index zero.
func TestCodeownersRouteKeepsTheAbsentCursorLegEmptyThroughDispatch(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "absent", args: map[string]any{"repository_id": "repo-web"}, want: ""},
		{name: "absent with the other two legs", args: map[string]any{"after_pattern": "/services/", "after_ref": "@team"}, want: ""},
		{name: "nil arguments", args: nil, want: ""},
		{name: "present as int", args: map[string]any{"after_order_index": 12}, want: "12"},
		{name: "present as float64", args: map[string]any{"after_order_index": 14.9}, want: "14"},
		{name: "present as explicit nil", args: map[string]any{"after_order_index": nil}, want: "0"},
		{name: "present as a string", args: map[string]any{"after_order_index": "12"}, want: "0"},
	} {
		got, err := resolveRoute("list_codeowners_ownership", tt.args)
		if err != nil {
			t.Fatalf("%s: resolveRoute error = %v, want nil", tt.name, err)
		}
		if value, present := got.query["after_order_index"]; !present || value != tt.want {
			t.Errorf("%s: after_order_index = (%q, present=%v), want (%q, present=true)", tt.name, value, present, tt.want)
		}
	}
}
