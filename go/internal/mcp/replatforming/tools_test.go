// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replatformingtools

import (
	"strings"
	"testing"
)

func TestToolsOwnsTwoDefinitionsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	tools := Tools()
	want := []string{
		"compose_replatforming_plan",
		"get_replatforming_rollups",
	}
	if len(tools) != len(want) {
		t.Fatalf("Tools() returned %d definitions, want %d", len(tools), len(want))
	}
	for i, name := range want {
		if tools[i].Name != name {
			t.Fatalf("Tools()[%d].Name = %q, want %q", i, tools[i].Name, name)
		}
	}
}

func TestRollupsSchemaDocumentsScope(t *testing.T) {
	t.Parallel()

	tool := rollupsTool()
	schema := tool.InputSchema.(map[string]any)
	if _, ok := schema["anyOf"]; ok {
		t.Fatal("schema must not advertise top-level anyOf")
	}
	if !strings.Contains(tool.Description, "Provide scope_id or account_id") {
		t.Fatalf("tool description = %q, want scope guidance", tool.Description)
	}
	if !strings.Contains(tool.Description, "rejected") {
		t.Fatalf("tool description = %q, want source-state taxonomy guidance", tool.Description)
	}
}
