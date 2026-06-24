// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

func TestSupplyChainImpactToolDescriptionAdvertisesReachabilityEnvelope(t *testing.T) {
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
	description := strings.ToLower(tool.Description)
	for _, want := range []string{"reachability", "not_called", "does not change impact", "parser/scip"} {
		if !strings.Contains(description, want) {
			t.Fatalf("description missing %q: %s", want, tool.Description)
		}
	}
}
