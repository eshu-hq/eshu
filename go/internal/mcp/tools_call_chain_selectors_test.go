// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestFindFunctionCallChainSchemaExposesExactSelectors(t *testing.T) {
	t.Parallel()

	properties, required := callChainToolSchema(t, "find_function_call_chain")
	for _, field := range []string{"repo_id", "start_entity_id", "end_entity_id"} {
		property, ok := properties[field].(map[string]any)
		if !ok {
			t.Fatalf("%s schema property type = %T, want map[string]any", field, properties[field])
		}
		description, _ := property["description"].(string)
		if !strings.Contains(description, "Optional") {
			t.Fatalf("%s description = %q, want optional selector guidance", field, description)
		}
	}
	for _, field := range []string{"repo_id", "start_entity_id", "end_entity_id"} {
		if stringSliceContains(required, field) {
			t.Fatalf("required = %#v, want %s optional", required, field)
		}
	}
}

func TestAnalyzeCodeRelationshipsSchemaExposesCallChainExactSelectors(t *testing.T) {
	t.Parallel()

	properties, required := callChainToolSchema(t, "analyze_code_relationships")
	for _, field := range []string{"start_entity_id", "end_entity_id"} {
		property, ok := properties[field].(map[string]any)
		if !ok {
			t.Fatalf("%s schema property type = %T, want map[string]any", field, properties[field])
		}
		description, _ := property["description"].(string)
		if !strings.Contains(description, "call_chain") {
			t.Fatalf("%s description = %q, want call_chain guidance", field, description)
		}
	}
	for _, field := range []string{"start_entity_id", "end_entity_id"} {
		if stringSliceContains(required, field) {
			t.Fatalf("required = %#v, want %s optional", required, field)
		}
	}
}

func callChainToolSchema(t *testing.T, name string) (map[string]any, []string) {
	t.Helper()

	tool := requireMCPTool(t, name)
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("%s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s properties type = %T, want map[string]any", name, schema["properties"])
	}
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("%s required type = %T, want []string", name, schema["required"])
	}
	return properties, required
}
