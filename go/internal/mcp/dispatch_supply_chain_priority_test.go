// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsSupplyChainImpactPriorityFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_supply_chain_impact_findings", map[string]any{
		"repository_id":      "repo://example/api",
		"priority_bucket":    "high",
		"min_priority_score": float64(60),
		"sort":               "priority_score_desc",
		"limit":              float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.query["priority_bucket"], "high"; got != want {
		t.Fatalf("priority_bucket query = %q, want %q", got, want)
	}
	if got, want := route.query["min_priority_score"], "60"; got != want {
		t.Fatalf("min_priority_score query = %q, want %q", got, want)
	}
	if got, want := route.query["sort"], "priority_score_desc"; got != want {
		t.Fatalf("sort query = %q, want %q", got, want)
	}
}

func TestSupplyChainImpactToolSchemaAdvertisesPriorityFilters(t *testing.T) {
	t.Parallel()

	var tool ToolDefinition
	for _, candidate := range supplyChainTools() {
		if candidate.Name == "list_supply_chain_impact_findings" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("list_supply_chain_impact_findings tool missing")
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	for _, name := range []string{"priority_bucket", "min_priority_score", "sort"} {
		if _, ok := properties[name]; !ok {
			t.Fatalf("tool schema missing %q in %#v", name, properties)
		}
	}
}
