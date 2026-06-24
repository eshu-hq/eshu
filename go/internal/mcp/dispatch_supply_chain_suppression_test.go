// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestSupplyChainImpactFindingsRouteIncludesSuppressionFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_supply_chain_impact_findings", map[string]any{
		"cve_id":             "CVE-2026-0001",
		"include_suppressed": true,
		"suppression_state":  "not_affected",
		"limit":              float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v", err)
	}
	if got, want := route.query["include_suppressed"], "true"; got != want {
		t.Fatalf("route.query[include_suppressed] = %q, want %q", got, want)
	}
	if got, want := route.query["suppression_state"], "not_affected"; got != want {
		t.Fatalf("route.query[suppression_state] = %q, want %q", got, want)
	}
}

func TestSupplyChainImpactFindingsRouteOmitsUnsetIncludeSuppressed(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_supply_chain_impact_findings", map[string]any{
		"cve_id": "CVE-2026-0001",
		"limit":  float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v", err)
	}
	if _, set := route.query["include_suppressed"]; set {
		t.Fatalf("route.query[include_suppressed] set unexpectedly: %#v", route.query["include_suppressed"])
	}
}

func TestSupplyChainImpactFindingsToolDeclaresSuppressionInputs(t *testing.T) {
	t.Parallel()

	var tool *ToolDefinition
	for _, def := range supplyChainTools() {
		if def.Name == "list_supply_chain_impact_findings" {
			d := def
			tool = &d
			break
		}
	}
	if tool == nil {
		t.Fatal("supply chain tool list_supply_chain_impact_findings missing")
	}
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("InputSchema properties type = %T, want map[string]any", schema["properties"])
	}
	if _, set := props["suppression_state"]; !set {
		t.Fatalf("suppression_state property missing from tool input schema: %#v", props)
	}
	if _, set := props["include_suppressed"]; !set {
		t.Fatalf("include_suppressed property missing from tool input schema: %#v", props)
	}
}
