// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsRelationshipStoryTokenBudgetAndTypes(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_code_relationship_story", map[string]any{
		"entity_id":          "fn-1",
		"relationship_types": []any{"CALLS", "IMPORTS"},
		"token_budget":       float64(512),
		"limit":              float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	body := requireRouteBody(t, route)
	if got, want := body["token_budget"], 512; got != want {
		t.Fatalf("body[token_budget] = %#v, want %#v", got, want)
	}
	types, ok := body["relationship_types"].([]any)
	if !ok {
		t.Fatalf("body[relationship_types] type = %T, want []any", body["relationship_types"])
	}
	if got, want := len(types), 2; got != want {
		t.Fatalf("len(relationship_types) = %d, want %d (%#v)", got, want, types)
	}
}

func TestResolveRouteMapsAnalyzeCallersTokenBudgetAndTypes(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type":         "find_callers",
		"target":             "helper",
		"relationship_types": []any{"CALLS", "REFERENCES"},
		"token_budget":       float64(256),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships/story" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships/story", route.path)
	}
	body := requireRouteBody(t, route)
	if got, want := body["token_budget"], 256; got != want {
		t.Fatalf("body[token_budget] = %#v, want %#v", got, want)
	}
	types, ok := body["relationship_types"].([]any)
	if !ok {
		t.Fatalf("body[relationship_types] type = %T, want []any", body["relationship_types"])
	}
	if got, want := len(types), 2; got != want {
		t.Fatalf("len(relationship_types) = %d, want %d (%#v)", got, want, types)
	}
}

func TestResolveRouteMapsRelationshipStoryMinConfidence(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_code_relationship_story", map[string]any{
		"entity_id":         "fn-1",
		"min_confidence":    float64(0.75),
		"relationship_type": "CALLS",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	body := requireRouteBody(t, route)
	if got, want := body["min_confidence"], 0.75; got != want {
		t.Fatalf("body[min_confidence] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCallersMinConfidence(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type":     "find_callers",
		"target":         "helper",
		"min_confidence": float64(0.6),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships/story" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships/story", route.path)
	}
	body := requireRouteBody(t, route)
	if got, want := body["min_confidence"], 0.6; got != want {
		t.Fatalf("body[min_confidence] = %#v, want %#v", got, want)
	}
}
