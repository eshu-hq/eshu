// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestCodeownersOwnershipToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_codeowners_ownership")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "repository_id" {
		t.Fatalf("required = %#v, want [repository_id]", schema["required"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"repository_id", "limit", "after_order_index", "after_pattern", "after_ref"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

// TestReadOnlyToolsSplicePreservesCodeownersPosition pins the absolute
// registration index of the CODEOWNERS definition owned by the code/owners
// package, with its cicd-aggregate/service-catalog predecessors and
// kubernetes/secrets neighbours. Every name and index is literal here,
// independent of the child's own order, so a reorder of
// codeownerstools.Tools or a move of the whole segment fails this test
// instead of shifting with the code under test.
func TestReadOnlyToolsSplicePreservesCodeownersPosition(t *testing.T) {
	t.Parallel()

	tools := ReadOnlyTools()
	want := map[int]string{
		75: "count_ci_cd_run_correlations",
		76: "get_ci_cd_run_correlation_inventory",
		77: "list_service_catalog_correlations",
		78: "list_codeowners_ownership",
		79: "list_kubernetes_correlations",
		80: "list_secrets_iam_identity_trust_chains",
	}
	for index, name := range want {
		if index >= len(tools) {
			t.Fatalf("ReadOnlyTools() has %d tools, want index [%d] = %q", len(tools), index, name)
		}
		if got := tools[index].Name; got != name {
			t.Fatalf("ReadOnlyTools()[%d] = %q, want %q", index, got, name)
		}
	}
}

func TestResolveRouteMapsCodeownersOwnership(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_codeowners_ownership", map[string]any{
		"repository_id": "repo-1",
		"limit":         float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Method, "GET"; got != want {
		t.Fatalf("route.Method = %q, want %q", got, want)
	}
	if got, want := route.Path, "/api/v0/codeowners/ownership"; got != want {
		t.Fatalf("route.Path = %q, want %q", got, want)
	}
	if got, want := route.Query["repository_id"], "repo-1"; got != want {
		t.Fatalf("route.Query[repository_id] = %q, want %q", got, want)
	}
	if got, want := route.Query["limit"], "25"; got != want {
		t.Fatalf("route.Query[limit] = %q, want %q", got, want)
	}
	if got, want := route.Query["after_order_index"], ""; got != want {
		t.Fatalf("route.Query[after_order_index] = %q, want %q (absent cursor must stay empty, not coerce to 0)", got, want)
	}
}

func TestResolveRouteMapsCodeownersOwnershipCursor(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_codeowners_ownership", map[string]any{
		"repository_id":     "repo-1",
		"after_order_index": float64(3),
		"after_pattern":     "*.go",
		"after_ref":         "@org/team-a",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.Query["after_order_index"], "3"; got != want {
		t.Fatalf("route.Query[after_order_index] = %q, want %q", got, want)
	}
	if got, want := route.Query["after_pattern"], "*.go"; got != want {
		t.Fatalf("route.Query[after_pattern] = %q, want %q", got, want)
	}
	if got, want := route.Query["after_ref"], "@org/team-a"; got != want {
		t.Fatalf("route.Query[after_ref] = %q, want %q", got, want)
	}
}
