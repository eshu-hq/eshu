// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iacmanagementtools

import (
	"strings"
	"testing"
)

func TestToolsOwnsFiveDefinitionsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	tools := Tools()
	want := []string{
		"get_iac_management_status",
		"explain_iac_management_status",
		"propose_terraform_import_plan",
		"list_terraform_config_state_drift_findings",
		"find_unmanaged_resource_owners",
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

func TestConfigStateDriftFindingsSchemaRequiresScopeID(t *testing.T) {
	t.Parallel()

	tool := configStateDriftFindingsTool()
	schema := tool.InputSchema.(map[string]any)
	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "scope_id" {
		t.Fatalf("schema[required] = %#v, want [scope_id]", schema["required"])
	}
	if !strings.Contains(tool.Description, "Provide scope_id") {
		t.Fatalf("tool description = %q, want scope_id guidance", tool.Description)
	}
}

func TestOwnershipSchemaDocumentsScopeAndSafety(t *testing.T) {
	t.Parallel()

	tool := ownershipTool()
	schema := tool.InputSchema.(map[string]any)
	if _, ok := schema["anyOf"]; ok {
		t.Fatal("schema must not advertise top-level anyOf")
	}
	if !strings.Contains(tool.Description, "Provide scope_id or account_id") {
		t.Fatalf("tool description = %q, want scope guidance", tool.Description)
	}
	if !strings.Contains(tool.Description, "candidate") {
		t.Fatalf("tool description = %q, want candidate (not fabricated owner) guidance", tool.Description)
	}
	if !strings.Contains(tool.Description, "provenance-only") {
		t.Fatalf("tool description = %q, want raw-tag provenance guidance", tool.Description)
	}
}
