// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestAdmissionDecisionToolIsRegistered(t *testing.T) {
	t.Parallel()

	tool := requireToolDefinition(t, "list_admission_decisions")
	if !strings.Contains(tool.Description, "admission") ||
		!strings.Contains(tool.Description, "canonical graph edges") {
		t.Fatalf("description = %q, want admission decision and canonical edge guidance", tool.Description)
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, required := range []string{"domain", "scope_id", "generation_id", "state", "anchor_kind", "anchor_id"} {
		if _, ok := properties[required]; !ok {
			t.Fatalf("tool properties missing %q", required)
		}
	}
}
