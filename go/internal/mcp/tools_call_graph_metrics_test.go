// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestCodebaseToolsSplicePreservesIntelPositions pins the absolute
// registration indices of the four code-intelligence definitions owned by
// the code/intel package, with the interleaved import-dependency and
// trailing security neighbors. Every name and index is literal here,
// independent of the child's own order, so a reorder of
// codeinteltools.Tools or a move of the whole segment fails this test
// instead of shifting with the code under test.
func TestCodebaseToolsSplicePreservesIntelPositions(t *testing.T) {
	t.Parallel()

	codebase := codebaseTools()
	want := map[int]string{
		2: "inspect_code_inventory",
		3: "investigate_import_dependencies",
		4: "inspect_call_graph_metrics",
		5: "trace_route_callers",
		6: "investigate_code_topic",
		7: "investigate_hardcoded_secrets",
	}
	for index, name := range want {
		if index >= len(codebase) {
			t.Fatalf("codebaseTools() has %d tools, want index [%d] = %q", len(codebase), index, name)
		}
		if got := codebase[index].Name; got != name {
			t.Fatalf("codebaseTools()[%d] = %q, want %q", index, got, name)
		}
	}
}

// TestCodebaseToolsSplicePreservesIntelLanguagePositions pins the absolute
// registration indices of the language-query and call-chain definitions
// owned by the code/intel package, with their repository-stats predecessor.
// Every name and index is literal here, independent of the child's own
// order, so a reorder of codeinteltools.Tools or a move of the segment
// fails this test instead of shifting with the code under test.
func TestCodebaseToolsSplicePreservesIntelLanguagePositions(t *testing.T) {
	t.Parallel()

	codebase := codebaseTools()
	want := map[int]string{
		30: "get_repository_stats",
		31: "execute_language_query",
		32: "find_function_call_chain",
	}
	for index, name := range want {
		if index >= len(codebase) {
			t.Fatalf("codebaseTools() has %d tools, want index [%d] = %q", len(codebase), index, name)
		}
		if got := codebase[index].Name; got != name {
			t.Fatalf("codebaseTools()[%d] = %q, want %q", index, got, name)
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
