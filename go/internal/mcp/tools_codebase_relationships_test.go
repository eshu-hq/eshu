// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestReadOnlyToolsReturnsIndependentRelationshipTypeEnums(t *testing.T) {
	firstRegistry := ReadOnlyTools()
	secondRegistry := ReadOnlyTools()

	enums := []struct {
		name string
		enum []string
	}{
		{name: "first registry story relationship_type", enum: registeredRelationshipEnum(t, firstRegistry, "get_code_relationship_story", "relationship_type")},
		{name: "first registry story relationship_types", enum: registeredRelationshipEnum(t, firstRegistry, "get_code_relationship_story", "relationship_types")},
		{name: "first registry analysis relationship_types", enum: registeredRelationshipEnum(t, firstRegistry, "analyze_code_relationships", "relationship_types")},
		{name: "second registry story relationship_type", enum: registeredRelationshipEnum(t, secondRegistry, "get_code_relationship_story", "relationship_type")},
		{name: "second registry story relationship_types", enum: registeredRelationshipEnum(t, secondRegistry, "get_code_relationship_story", "relationship_types")},
		{name: "second registry analysis relationship_types", enum: registeredRelationshipEnum(t, secondRegistry, "analyze_code_relationships", "relationship_types")},
	}
	wantFirstValues := make([]string, len(enums))
	for i, enum := range enums {
		wantFirstValues[i] = enum.enum[0]
	}

	for mutatedIndex, mutated := range enums {
		t.Run("mutating "+mutated.name, func(t *testing.T) {
			original := mutated.enum[0]
			t.Cleanup(func() {
				mutated.enum[0] = original
			})
			mutated.enum[0] = "MUTATED"

			for observedIndex, observed := range enums {
				if observedIndex == mutatedIndex {
					continue
				}
				if got, want := observed.enum[0], wantFirstValues[observedIndex]; got != want {
					t.Errorf("%s first enum value = %q after mutating %s, want %q", observed.name, got, mutated.name, want)
				}
			}
		})
	}
}

func registeredRelationshipEnum(t *testing.T, tools []ToolDefinition, toolName, propertyName string) []string {
	t.Helper()

	var inputSchema map[string]any
	for _, tool := range tools {
		if tool.Name == toolName {
			var ok bool
			inputSchema, ok = tool.InputSchema.(map[string]any)
			if !ok {
				t.Fatalf("%s input schema type = %T, want map[string]any", toolName, tool.InputSchema)
			}
			break
		}
	}
	if inputSchema == nil {
		t.Fatalf("ReadOnlyTools missing %q", toolName)
	}

	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s properties type = %T, want map[string]any", toolName, inputSchema["properties"])
	}
	property, ok := properties[propertyName].(map[string]any)
	if !ok {
		t.Fatalf("%s.%s schema type = %T, want map[string]any", toolName, propertyName, properties[propertyName])
	}
	if items, ok := property["items"].(map[string]any); ok {
		property = items
	}
	enum, ok := property["enum"].([]string)
	if !ok || len(enum) == 0 {
		t.Fatalf("%s.%s enum = %#v, want non-empty []string", toolName, propertyName, property["enum"])
	}
	return enum
}

func TestAnalyzeCodeRelationshipsSchemaProperties(t *testing.T) {
	t.Parallel()

	schema := analyzeCodeRelationshipsSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"query_type", "target", "context", "repo_id", "start_entity_id", "end_entity_id", "scope", "max_depth", "limit", "offset", "relationship_types", "token_budget", "min_confidence"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("schema properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsDefault(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_callers",
		"target":     "MyFunc",
		"repo_id":    "repo-1",
		"limit":      float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/relationships/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCallChainQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type":      "call_chain",
		"start_entity_id": "ent-1",
		"end_entity_id":   "ent-2",
		"repo_id":         "repo-1",
		"max_depth":       float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/call-chain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsDeadCode(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "dead_code",
		"repo_id":    "repo-1",
		"limit":      float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/dead-code"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsModuleDeps(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "module_deps",
		"target":     "mypackage",
		"repo_id":    "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/relationships"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
