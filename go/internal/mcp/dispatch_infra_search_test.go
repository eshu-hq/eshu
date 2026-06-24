// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteFindInfraResourcesPreservesStructuredFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_infra_resources", map[string]any{
		"category":          "cloud",
		"kind":              "aws_instance",
		"provider":          "aws",
		"environment":       "prod",
		"resource_service":  "ec2",
		"resource_category": "compute",
		"limit":             7,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/infra/resources/search"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"category":          "cloud",
		"kind":              "aws_instance",
		"provider":          "aws",
		"environment":       "prod",
		"resource_service":  "ec2",
		"resource_category": "compute",
		"limit":             7,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestFindInfraResourcesToolSchemaAllowsStructuredFiltersWithoutQuery(t *testing.T) {
	t.Parallel()

	var searchTool ToolDefinition
	for _, tool := range ecosystemTools() {
		if tool.Name == "find_infra_resources" {
			searchTool = tool
			break
		}
	}
	if searchTool.Name == "" {
		t.Fatal("find_infra_resources tool not found")
	}
	schema, ok := searchTool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", searchTool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{
		"query",
		"category",
		"kind",
		"provider",
		"environment",
		"resource_service",
		"resource_category",
		"limit",
	} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("InputSchema missing %s", field)
		}
	}
	required, _ := schema["required"].([]string)
	if stringSliceContains(required, "query") {
		t.Fatalf("required = %#v, want query omitted for structured filter searches", required)
	}
}
