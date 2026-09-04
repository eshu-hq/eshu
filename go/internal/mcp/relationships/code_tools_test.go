// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshiptools

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/toolcontract"
)

func TestCodeToolsPreserveRegistrationContract(t *testing.T) {
	t.Parallel()

	tools := CodeTools()
	if got, want := len(tools), 2; got != want {
		t.Fatalf("len(CodeTools()) = %d, want %d", got, want)
	}
	if got, want := []string{tools[0].Name, tools[1].Name}, []string{"get_code_relationship_story", "analyze_code_relationships"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CodeTools() names = %#v, want %#v", got, want)
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal code relationship tools: %v", err)
	}
	const wantDefinitionBytes = 6276
	if got := len(encoded); got != wantDefinitionBytes {
		t.Fatalf("serialized code relationship definitions = %d bytes, want %d", got, wantDefinitionBytes)
	}
	const wantDefinitionsHash = "6677735b3339fd3e24d97cebba114d2d7737b4a332c9e8658b6615fb3257e0d0"
	if got := fmt.Sprintf("%x", sha256.Sum256(encoded)); got != wantDefinitionsHash {
		t.Fatalf("code relationship definitions hash = %s, want %s", got, wantDefinitionsHash)
	}

	wantRelationshipTypes := []string{"CALLS", "IMPORTS", "REFERENCES", "INHERITS", "OVERRIDES", "TAINT_FLOWS_TO"}
	for _, property := range []string{"relationship_type", "relationship_types"} {
		if got := codeToolEnum(t, tools[0], property); !reflect.DeepEqual(got, wantRelationshipTypes) {
			t.Fatalf("%s enum = %#v, want %#v", property, got, wantRelationshipTypes)
		}
	}
	if got := codeToolEnum(t, tools[1], "relationship_types"); !reflect.DeepEqual(got, wantRelationshipTypes) {
		t.Fatalf("analysis relationship_types enum = %#v, want %#v", got, wantRelationshipTypes)
	}

	wantQueryTypes := []string{
		"find_callers",
		"find_callees",
		"find_all_callers",
		"find_all_callees",
		"find_cross_repo_callers",
		"find_cross_repo_callees",
		"find_importers",
		"find_cross_repo_importers",
		"who_modifies",
		"class_hierarchy",
		"cross_repo_class_hierarchy",
		"overrides",
		"cross_repo_overrides",
		"dead_code",
		"call_chain",
		"find_cross_repo_call_chain",
		"module_deps",
		"variable_scope",
		"find_complexity",
		"find_functions_by_argument",
		"find_functions_by_decorator",
	}
	if got := codeToolEnum(t, tools[1], "query_type"); !reflect.DeepEqual(got, wantQueryTypes) {
		t.Fatalf("analysis query_type enum = %#v, want %#v", got, wantQueryTypes)
	}
}

func TestCodeToolsReturnDeeplyIndependentDefinitions(t *testing.T) {
	firstRegistry := CodeTools()
	secondRegistry := CodeTools()

	enums := []struct {
		name string
		enum []string
	}{
		{name: "first registry story relationship_type", enum: codeToolEnum(t, firstRegistry[0], "relationship_type")},
		{name: "first registry story relationship_types", enum: codeToolEnum(t, firstRegistry[0], "relationship_types")},
		{name: "first registry analysis relationship_types", enum: codeToolEnum(t, firstRegistry[1], "relationship_types")},
		{name: "second registry story relationship_type", enum: codeToolEnum(t, secondRegistry[0], "relationship_type")},
		{name: "second registry story relationship_types", enum: codeToolEnum(t, secondRegistry[0], "relationship_types")},
		{name: "second registry analysis relationship_types", enum: codeToolEnum(t, secondRegistry[1], "relationship_types")},
	}
	wantFirstValues := make([]string, len(enums))
	for i, enum := range enums {
		wantFirstValues[i] = enum.enum[0]
	}

	for mutatedIndex, mutated := range enums {
		mutatedIndex, mutated := mutatedIndex, mutated
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

	firstSchema := codeToolSchema(t, firstRegistry[1])
	secondSchema := codeToolSchema(t, secondRegistry[1])
	firstRequired := firstSchema["required"].([]string)
	secondRequired := secondSchema["required"].([]string)
	originalRequired := firstRequired[0]
	t.Cleanup(func() {
		firstRequired[0] = originalRequired
	})
	firstRequired[0] = "MUTATED"
	if got, want := secondRequired[0], "query_type"; got != want {
		t.Fatalf("second required field = %q after mutating first registry, want %q", got, want)
	}

	firstProperties := firstSchema["properties"].(map[string]any)
	secondProperties := secondSchema["properties"].(map[string]any)
	originalTarget := firstProperties["target"]
	t.Cleanup(func() {
		firstProperties["target"] = originalTarget
	})
	firstProperties["target"] = "MUTATED"
	if got := secondProperties["target"]; got == "MUTATED" {
		t.Fatal("code relationship constructors share nested property-map storage")
	}
}

func TestAnalyzeCodeRelationshipsSchemaReturnsFreshSchema(t *testing.T) {
	first := AnalyzeCodeRelationshipsSchema()
	second := AnalyzeCodeRelationshipsSchema()
	registered := codeToolSchema(t, CodeTools()[1])
	if !reflect.DeepEqual(first, registered) {
		t.Fatal("analysis schema constructor drifted from the registered tool schema")
	}

	firstProperties := first["properties"].(map[string]any)
	secondProperties := second["properties"].(map[string]any)
	firstProperties["target"] = "MUTATED"
	if got := secondProperties["target"]; got == "MUTATED" {
		t.Fatal("analysis schema constructor shares nested property-map storage")
	}

	firstRequired := first["required"].([]string)
	secondRequired := second["required"].([]string)
	firstRequired[0] = "MUTATED"
	if got, want := secondRequired[0], "query_type"; got != want {
		t.Fatalf("second required field = %q after mutating first schema, want %q", got, want)
	}
}

func codeToolEnum(t *testing.T, tool toolcontract.ToolDefinition, propertyName string) []string {
	t.Helper()

	properties := codeToolSchema(t, tool)["properties"].(map[string]any)
	property, ok := properties[propertyName].(map[string]any)
	if !ok {
		t.Fatalf("%s.%s schema type = %T, want map[string]any", tool.Name, propertyName, properties[propertyName])
	}
	if items, ok := property["items"].(map[string]any); ok {
		property = items
	}
	enum, ok := property["enum"].([]string)
	if !ok || len(enum) == 0 {
		t.Fatalf("%s.%s enum = %#v, want non-empty []string", tool.Name, propertyName, property["enum"])
	}
	return enum
}

func codeToolSchema(t *testing.T, tool toolcontract.ToolDefinition) map[string]any {
	t.Helper()

	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("%s input schema type = %T, want map[string]any", tool.Name, tool.InputSchema)
	}
	return schema
}
