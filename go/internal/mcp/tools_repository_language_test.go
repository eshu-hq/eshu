// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestRepositoryLanguageToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_repositories_by_language", "list_repositories_by_language", "get_repository_language_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if name != "get_repository_language_inventory" {
			if _, ok := properties["language"]; !ok {
				t.Fatalf("tool %s properties missing language", name)
			}
		}
	}
}

func TestResolveRouteMapsCountRepositoriesByLanguage(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_repositories_by_language", map[string]any{
		"language": "go",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/repositories/by-language"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsListRepositoriesByLanguage(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_repositories_by_language", map[string]any{
		"language": "python",
		"limit":    float64(25),
		"offset":   float64(0),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/repositories/by-language"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetRepositoryLanguageInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_repository_language_inventory", map[string]any{
		"limit":  float64(50),
		"offset": float64(0),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/repositories/language-inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
