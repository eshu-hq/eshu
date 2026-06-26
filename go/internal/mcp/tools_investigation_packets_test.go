// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestInvestigationPacketToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"export_supply_chain_impact_packet", "export_deployable_unit_packet", "export_cloud_runtime_drift_packet"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		if _, ok := properties["max_source_facts"]; !ok {
			t.Fatalf("tool %s properties missing max_source_facts", name)
		}
	}
}

func TestResolveRouteMapsExportSupplyChainImpactPacket(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("export_supply_chain_impact_packet", map[string]any{
		"finding_id":       "finding-1",
		"max_source_facts": float64(20),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/investigations/supply-chain/impact/packet"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsExportDeployableUnitPacket(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("export_deployable_unit_packet", map[string]any{
		"scope_id":      "scope-1",
		"generation_id": "gen-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/investigations/deployable-unit/packet"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsExportCloudRuntimeDriftPacket(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("export_cloud_runtime_drift_packet", map[string]any{
		"scope_id": "aws:123:us-east-1",
		"provider": "aws",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/investigations/drift/packet"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
