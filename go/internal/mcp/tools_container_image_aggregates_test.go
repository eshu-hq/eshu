// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestContainerImageIdentityAggregateToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_container_image_identities", "get_container_image_identity_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		for _, field := range []string{"digest", "image_ref", "repository_id", "source_repository_id", "outcome"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s properties missing %q", name, field)
			}
		}
	}
}

func TestResolveRouteMapsCountContainerImageIdentities(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_container_image_identities", map[string]any{
		"source_repository_id": "repo-1",
		"outcome":              "exact_digest",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/container-images/identities/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["source_repository_id"], "repo-1"; got != want {
		t.Fatalf("route.query[source_repository_id] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsGetContainerImageIdentityInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_container_image_identity_inventory", map[string]any{
		"source_repository_id": "repo-1",
		"group_by":             "outcome",
		"limit":                float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/container-images/identities/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["group_by"], "outcome"; got != want {
		t.Fatalf("route.query[group_by] = %#v, want %#v", got, want)
	}
}
