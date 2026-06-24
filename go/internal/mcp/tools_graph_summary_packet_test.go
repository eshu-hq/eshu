// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsGraphSummaryPacketToolToBoundedEndpoint(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_graph_summary_packet", map[string]any{
		"repo_id": "repo-1",
		"limit":   float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/ecosystem/graph-summary"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 10; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestGraphSummaryPacketToolSchemaIsBounded(t *testing.T) {
	t.Parallel()

	var tool ToolDefinition
	for _, candidate := range ecosystemTools() {
		if candidate.Name == "get_graph_summary_packet" {
			tool = candidate
			break
		}
	}
	if tool.Name != "get_graph_summary_packet" {
		t.Fatalf("get_graph_summary_packet not present in ecosystemTools()")
	}
	schema := tool.InputSchema.(map[string]any)
	properties := schema["properties"].(map[string]any)
	repoID, ok := properties["repo_id"].(map[string]any)
	if !ok {
		t.Fatalf("repo_id property missing from schema")
	}
	if _, ok := repoID["description"].(string); !ok {
		t.Fatalf("repo_id missing description")
	}
	limit := properties["limit"].(map[string]any)
	if got, want := limit["maximum"], 100; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
	if got, want := limit["minimum"], 1; got != want {
		t.Fatalf("limit minimum = %#v, want %#v", got, want)
	}
}
