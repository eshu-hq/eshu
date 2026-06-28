// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestRelationshipEdgesRouteForwardsVerb(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_relationship_edges", map[string]any{
		"verb": "DEPENDS_ON",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_relationship_edges) error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/relationships/edges"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	if got, want := body["verb"], "DEPENDS_ON"; got != want {
		t.Fatalf("body[verb] = %#v, want %#v", got, want)
	}
}

func TestRelationshipEdgesRouteForwardsSourceTool(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_relationship_edges", map[string]any{
		"verb":        "DEPENDS_ON",
		"source_tool": "terraform",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_relationship_edges) error = %v, want nil", err)
	}
	body := requireRouteBody(t, route)
	if got, want := body["source_tool"], "terraform"; got != want {
		t.Fatalf("body[source_tool] = %#v, want %#v", got, want)
	}
}

func TestRelationshipEdgesRouteNoSourceToolWhenEmpty(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_relationship_edges", map[string]any{
		"verb": "DEPENDS_ON",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_relationship_edges) error = %v, want nil", err)
	}
	body := requireRouteBody(t, route)
	if _, ok := body["source_tool"]; ok {
		t.Fatalf("body[source_tool] = %#v, want absent when no filter provided", body["source_tool"])
	}
}

func TestRelationshipEdgesRouteRejectsUnknownSourceTool(t *testing.T) {
	t.Parallel()

	_, err := resolveRoute("list_relationship_edges", map[string]any{
		"verb":        "DEPENDS_ON",
		"source_tool": "not_a_real_tool",
	})
	if err == nil {
		t.Fatal("resolveRoute(list_relationship_edges) error = nil, want error for unknown source_tool")
	}
}

func TestRelationshipEdgesRouteForwardsLimit(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_relationship_edges", map[string]any{
		"verb":  "DEPLOYS_FROM",
		"limit": 100,
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_relationship_edges) error = %v, want nil", err)
	}
	body := requireRouteBody(t, route)
	if got, want := body["limit"], 100; got != want {
		t.Fatalf("body[limit] = %#v, want %d", got, want)
	}
}

func TestRelationshipEdgesRouteLimitDefaults(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_relationship_edges", map[string]any{
		"verb": "DEPENDS_ON",
	})
	if err != nil {
		t.Fatalf("resolveRoute(list_relationship_edges) error = %v, want nil", err)
	}
	body := requireRouteBody(t, route)
	if got, want := body["limit"], 50; got != want {
		t.Fatalf("default body[limit] = %#v, want %d", got, want)
	}
}
