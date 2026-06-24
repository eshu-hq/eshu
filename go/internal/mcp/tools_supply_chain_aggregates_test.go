// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestSupplyChainImpactCountToolDocumentsProfile(t *testing.T) {
	t.Parallel()

	tool, ok := supplyChainAggregateToolByName("count_supply_chain_impact_findings")
	if !ok {
		t.Fatal("count_supply_chain_impact_findings tool missing")
	}
	inputSchema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("tool input schema = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("tool properties = %T, want map[string]any", inputSchema["properties"])
	}
	profile, ok := properties["profile"].(map[string]any)
	if !ok {
		t.Fatalf("profile property = %T, want map[string]any", properties["profile"])
	}
	if got, want := profile["default"], "precise"; got != want {
		t.Fatalf("profile.default = %#v, want %#v", got, want)
	}
	enum, ok := profile["enum"].([]string)
	if !ok {
		t.Fatalf("profile.enum = %T, want []string", profile["enum"])
	}
	if !stringSetEqual(enum, []string{"precise", "comprehensive"}) {
		t.Fatalf("profile.enum = %#v, want precise/comprehensive", enum)
	}
	if got, ok := profile["description"].(string); !ok || !containsAll(got, "precise", "comprehensive") {
		t.Fatalf("profile.description = %#v, want precise/comprehensive semantics", profile["description"])
	}
}

func TestSupplyChainImpactAggregateToolsDocumentPriorityAndSuppressionFilters(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"count_supply_chain_impact_findings",
		"get_supply_chain_impact_inventory",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tool, ok := supplyChainAggregateToolByName(name)
			if !ok {
				t.Fatalf("%s tool missing", name)
			}
			inputSchema, ok := tool.InputSchema.(map[string]any)
			if !ok {
				t.Fatalf("%s input schema = %T, want map[string]any", name, tool.InputSchema)
			}
			properties, ok := inputSchema["properties"].(map[string]any)
			if !ok {
				t.Fatalf("%s properties = %T, want map[string]any", name, inputSchema["properties"])
			}
			for _, want := range []string{
				"profile",
				"priority_bucket",
				"min_priority_score",
				"suppression_state",
				"include_suppressed",
			} {
				if _, ok := properties[want]; !ok {
					t.Fatalf("%s schema missing %q in %#v", name, want, properties)
				}
			}
		})
	}
}

func supplyChainAggregateToolByName(name string) (ToolDefinition, bool) {
	for _, tool := range supplyChainImpactAggregateTools() {
		if tool.Name == name {
			return tool, true
		}
	}
	return ToolDefinition{}, false
}

func stringSetEqual(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(got))
	for _, value := range got {
		seen[value] = true
	}
	for _, value := range want {
		if !seen[value] {
			return false
		}
	}
	return true
}

func containsAll(text string, wants ...string) bool {
	for _, want := range wants {
		if !contains(text, want) {
			return false
		}
	}
	return true
}

func contains(text string, want string) bool {
	return strings.Contains(text, want)
}
