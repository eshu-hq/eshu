// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestSemanticSearchToolRoutesToBoundedHTTPRead(t *testing.T) {
	t.Parallel()

	registered := map[string]ToolDefinition{}
	for _, tool := range ReadOnlyTools() {
		registered[tool.Name] = tool
	}
	tool, ok := registered["search_semantic_context"]
	if !ok {
		t.Fatal("ReadOnlyTools missing search_semantic_context")
	}
	schema := tool.InputSchema.(map[string]any)
	required := schema["required"].([]any)
	for _, want := range []string{"repo_id", "query", "mode", "limit", "timeout_ms"} {
		if !containsRequired(required, want) {
			t.Fatalf("search_semantic_context required fields = %#v, want %q", required, want)
		}
	}

	route, err := resolveRoute("search_semantic_context", map[string]any{
		"repo_id":      "repo-payments",
		"query":        "payment runbook",
		"mode":         "keyword",
		"limit":        5,
		"timeout_ms":   250,
		"service_id":   "svc-payments",
		"source_kinds": []any{"runtime_summary"},
	})
	if err != nil {
		t.Fatalf("resolveRoute(search_semantic_context) error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/search/semantic"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
		"service_id": "svc-payments",
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%q] = %#v, want %#v", key, got, want)
		}
	}
	sourceKinds := body["source_kinds"].([]any)
	if got, want := sourceKinds[0], "runtime_summary"; got != want {
		t.Fatalf("source_kinds[0] = %#v, want %#v", got, want)
	}
}

func TestSemanticSearchToolPassesRerankFlag(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("search_semantic_context", map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "hybrid",
		"limit":      5,
		"timeout_ms": 250,
		"rerank":     true,
	})
	if err != nil {
		t.Fatalf("resolveRoute error = %v, want nil", err)
	}
	if got, want := route.body.(map[string]any)["rerank"], true; got != want {
		t.Fatalf("body[rerank] = %#v, want %#v", got, want)
	}

	// rerank defaults to false when the caller omits it.
	defaultRoute, err := resolveRoute("search_semantic_context", map[string]any{
		"repo_id":    "repo-payments",
		"query":      "payment runbook",
		"mode":       "keyword",
		"limit":      5,
		"timeout_ms": 250,
	})
	if err != nil {
		t.Fatalf("resolveRoute(default) error = %v, want nil", err)
	}
	if got, want := defaultRoute.body.(map[string]any)["rerank"], false; got != want {
		t.Fatalf("default body[rerank] = %#v, want %#v", got, want)
	}
}

func containsRequired(fields []any, want string) bool {
	for _, field := range fields {
		if field == want {
			return true
		}
	}
	return false
}
