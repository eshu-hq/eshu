// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinteltools

import (
	"reflect"
	"testing"
)

func TestToolsReturnsEightDefinitionsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	tools := Tools()
	want := []string{
		"find_code",
		"find_symbol",
		"inspect_code_inventory",
		"inspect_call_graph_metrics",
		"trace_route_callers",
		"investigate_code_topic",
		"execute_language_query",
		"find_function_call_chain",
	}
	if len(tools) != len(want) {
		t.Fatalf("len(Tools()) = %d, want %d", len(tools), len(want))
	}
	for i, name := range want {
		if got := tools[i].Name; got != name {
			t.Fatalf("Tools()[%d].Name = %q, want %q", i, got, name)
		}
	}
}

func TestSearchToolSchemasRequireTheirScopeKeys(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"find_code":              {"query"},
		"find_symbol":            {"symbol"},
		"execute_language_query": {"language", "entity_type"},
	}
	byName := make(map[string]int, 8)
	for i, tool := range Tools() {
		byName[tool.Name] = i
	}
	for name, required := range cases {
		tool := Tools()[byName[name]]
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		if got := schema["required"]; !reflect.DeepEqual(got, required) {
			t.Fatalf("%s required = %#v, want %#v", name, got, required)
		}
	}
	chain := Tools()[byName["find_function_call_chain"]]
	schema := chain.InputSchema.(map[string]any)
	if got := schema["required"]; !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("find_function_call_chain required = %#v, want empty", got)
	}
}
