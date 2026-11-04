// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

// TestSupplyChainImpactInventoryToolDocumentsGroupedDimensions pins the #9873
// tool schema: group_by lists all eight dimensions, the description names the
// grouped semantics an agent needs to read the answer correctly, and the
// profile default stays precise.
func TestSupplyChainImpactInventoryToolDocumentsGroupedDimensions(t *testing.T) {
	t.Parallel()

	tool, ok := supplyChainAggregateToolByName("get_supply_chain_impact_inventory")
	if !ok {
		t.Fatal("get_supply_chain_impact_inventory tool missing")
	}
	inputSchema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("input schema = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T, want map[string]any", inputSchema["properties"])
	}
	groupBy, ok := properties["group_by"].(map[string]any)
	if !ok {
		t.Fatalf("group_by = %T, want map[string]any", properties["group_by"])
	}
	enum, ok := groupBy["enum"].([]string)
	if !ok {
		t.Fatalf("group_by.enum = %T, want []string", groupBy["enum"])
	}
	want := []string{
		"impact_status", "priority_bucket", "severity", "repository_id", "ecosystem",
		"service_id", "workload_id", "environment",
	}
	if !stringSetEqual(enum, want) {
		t.Fatalf("group_by.enum = %#v, want %#v", enum, want)
	}
	description, _ := groupBy["description"].(string)
	if !containsAll(description, "service_id", "workload_id", "environment", "unattributed", "completeness") {
		t.Fatalf("group_by.description = %q, want the grouped semantics named", description)
	}
	if !containsAll(tool.Description, "service_id", "workload_id", "environment", "distinct_findings") {
		t.Fatalf("tool description = %q, want the three dimensions and distinct_findings named", tool.Description)
	}
	profile, ok := properties["profile"].(map[string]any)
	if !ok || profile["default"] != "precise" {
		t.Fatalf("profile = %#v, want default precise", properties["profile"])
	}
}
