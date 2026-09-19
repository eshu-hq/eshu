// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestCodebaseToolsSplicePreservesIntelPositions pins the long-standing
// registration positions of the four code-intelligence definitions owned by
// the code/intel package. The names are literal here, independent of the
// child's own order, so a reorder of codeinteltools.Tools that would
// silently move a splice fails this test rather than shifting both sides of
// the comparison. The import-dependency and security helpers keep their
// interleaved positions between the splices.
func TestCodebaseToolsSplicePreservesIntelPositions(t *testing.T) {
	t.Parallel()

	codebase := codebaseTools()
	at := map[string]int{}
	for i, tool := range codebase {
		at[tool.Name] = i
	}
	inventory, ok := at["inspect_code_inventory"]
	if !ok {
		t.Fatal("inspect_code_inventory not found in codebaseTools()")
	}
	for name, want := range map[string]int{
		"inspect_call_graph_metrics": inventory + 2,
		"trace_route_callers":        inventory + 3,
		"investigate_code_topic":     inventory + 4,
	} {
		got, ok := at[name]
		if !ok {
			t.Fatalf("%q not found in codebaseTools()", name)
		}
		if got != want {
			t.Fatalf("%q at codebaseTools()[%d], want [%d]", name, got, want)
		}
	}
}

func TestResolveRouteMapsCallGraphMetricsToolToBoundedEndpoint(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("inspect_call_graph_metrics", map[string]any{
		"metric_type": "hub_functions",
		"repo_id":     "repo-1",
		"language":    "go",
		"limit":       float64(10),
		"offset":      float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/call-graph/metrics"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["metric_type"], "hub_functions"; got != want {
		t.Fatalf("body[metric_type] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 10; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 5; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
}
