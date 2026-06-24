// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteListCloudResourceInventoryForwardsBoundedFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_cloud_resource_inventory", map[string]any{
		"provider":          "gcp",
		"project_id":        "project-synthetic",
		"management_origin": "declared",
		"limit":             25,
		"cursor":            "50",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/cloud/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"provider":          "gcp",
		"project_id":        "project-synthetic",
		"management_origin": "declared",
		"limit":             "25",
		"cursor":            "50",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("query[%s] = %q, want %q", key, got, want)
		}
	}
}

func TestCloudInventoryToolAdvertisesProviderAndOriginEnums(t *testing.T) {
	t.Parallel()

	tools := cloudInventoryTools()
	if got, want := len(tools), 1; got != want {
		t.Fatalf("len(cloudInventoryTools()) = %d, want %d", got, want)
	}
	schema, ok := tools[0].InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tools[0].InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, key := range []string{"provider", "management_origin", "scope_id", "limit", "cursor"} {
		if _, present := properties[key]; !present {
			t.Fatalf("schema missing property %q", key)
		}
	}
}
