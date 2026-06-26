// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestContentToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"get_file_content", "get_file_lines", "get_entity_content", "build_evidence_citation_packet", "search_file_content", "search_entity_content"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if name == "get_file_content" || name == "get_file_lines" {
			if _, ok := properties["repo_id"]; !ok {
				t.Fatalf("tool %s properties missing repo_id", name)
			}
		}
	}
}

func TestResolveRouteMapsGetFileContent(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_file_content", map[string]any{
		"repo_id":       "repo-1",
		"relative_path": "src/main.go",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/content/files/read"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, _ := route.body.(map[string]any)
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsSearchFileContent(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("search_file_content", map[string]any{
		"pattern":  "logging",
		"repo_ids": []any{"repo-1"},
		"limit":    float64(20),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/content/files/search"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsBuildEvidenceCitationPacket(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("build_evidence_citation_packet", map[string]any{
		"subject":  map[string]any{"kind": "repo"},
		"question": "what is the auth flow?",
		"handles":  []any{map[string]any{"kind": "file", "repo_id": "repo-1", "relative_path": "auth.go"}},
		"limit":    float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/evidence/citations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
