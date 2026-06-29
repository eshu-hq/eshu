// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestFindCrossRepoDeadCodeToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "find_cross_repo_dead_code")
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"repo_id", "language", "limit", "consumer_repo_ids"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsFindCrossRepoDeadCodeToolRoute(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_cross_repo_dead_code", map[string]any{
		"repo_id":           "repo-producer",
		"consumer_repo_ids": []any{"repo-consumer"},
		"language":          "go",
		"limit":             float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/dead-code/cross-repo"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, _ := route.body.(map[string]any)
	if got, want := body["repo_id"], "repo-producer"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	consumerRepoIDs, ok := body["consumer_repo_ids"].([]string)
	if !ok {
		t.Fatalf("body[consumer_repo_ids] type = %T, want []string", body["consumer_repo_ids"])
	}
	if got, want := consumerRepoIDs[0], "repo-consumer"; got != want {
		t.Fatalf("consumer_repo_ids[0] = %#v, want %#v", got, want)
	}
}
