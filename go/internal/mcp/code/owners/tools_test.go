// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeownerstools

import "testing"

func TestCodeownersOwnershipToolSchemaRequiresRepositoryScope(t *testing.T) {
	t.Parallel()

	tools := Tools()
	if len(tools) != 1 {
		t.Fatalf("len(Tools()) = %d, want 1", len(tools))
	}
	tool := tools[0]
	if got, want := tool.Name, "list_codeowners_ownership"; got != want {
		t.Fatalf("tool.Name = %q, want %q", got, want)
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "repository_id" {
		t.Fatalf("required = %#v, want [repository_id]", schema["required"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, field := range []string{"repository_id", "limit", "after_order_index", "after_pattern", "after_ref"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("tool properties missing %q", field)
		}
	}
	limit, ok := properties["limit"].(map[string]any)
	if !ok {
		t.Fatalf("limit type = %T, want map[string]any", properties["limit"])
	}
	if got, want := limit["default"], 50; got != want {
		t.Fatalf("limit default = %#v, want %#v", got, want)
	}
	if got, want := limit["maximum"], 200; got != want {
		t.Fatalf("limit maximum = %#v, want %#v", got, want)
	}
}
