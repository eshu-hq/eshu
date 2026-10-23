// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"slices"
	"testing"

	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// TestRouterRequestsValidateAgainstTheirOwnSchemaInAnyOrder reverses the
// definition order and proves find_tool and call_tool still validate against
// the schema whose name matches. It does not call t.Parallel: it swaps the
// package-level definition source.
func TestRouterRequestsValidateAgainstTheirOwnSchemaInAnyOrder(t *testing.T) {
	saved := routerTools
	routerTools = func() []toolcontract.ToolDefinition {
		tools := Tools()
		slices.Reverse(tools)
		return tools
	}
	t.Cleanup(func() { routerTools = saved })

	engine := newFixtureEngine(t, orderRanker{})
	d := &recordingDispatcher{}
	if result := engine.Find(context.Background(), fixtureRequest(d), map[string]any{"intent": "find code"}); result.IsError {
		t.Fatalf("valid find_tool request rejected with definitions reversed: %v", structured(t, result))
	}
	out := engine.Call(context.Background(), fixtureRequest(d), map[string]any{"name": "get_repo_summary", "arguments": map[string]any{}})
	if !out.Dispatched {
		t.Fatalf("valid call_tool request refused with definitions reversed: %v", structured(t, out.Result))
	}
}

func TestToolSchemaLooksUpByName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		prop string
	}{{FindToolName, "intent"}, {CallToolName, "arguments"}} {
		schema, _ := toolSchema(tc.name).(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		if _, ok := props[tc.prop]; !ok {
			t.Fatalf("toolSchema(%s) = %v, want the schema with %q", tc.name, schema, tc.prop)
		}
	}
	if toolSchema("ask") != nil {
		t.Fatalf("toolSchema(ask) != nil, want nil for a non-router tool")
	}
}
