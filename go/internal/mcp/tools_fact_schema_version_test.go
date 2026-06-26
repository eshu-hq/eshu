// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestFactSchemaVersionToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"list_fact_schema_versions", "get_fact_schema_version"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["fact_kind"]; !ok && name == "get_fact_schema_version" {
			t.Fatalf("tool %s properties missing fact_kind", name)
		}
	}
}

func TestResolveRouteMapsListFactSchemaVersions(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_fact_schema_versions", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/fact-schema-versions"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetFactSchemaVersion(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_fact_schema_version", map[string]any{
		"fact_kind": "terraform_state_resource",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/fact-schema-versions/terraform_state_resource"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
