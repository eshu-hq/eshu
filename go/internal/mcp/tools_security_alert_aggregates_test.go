// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestSecurityAlertReconciliationAggregateToolsAreRegistered(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"count_security_alert_reconciliations", "get_security_alert_reconciliation_inventory"} {
		tool := requireToolDefinition(t, name)
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s InputSchema type = %T, want map[string]any", name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties type = %T, want map[string]any", name, schema["properties"])
		}
		for _, field := range []string{"repository_id", "provider", "package_id", "cve_id", "ghsa_id", "provider_state", "reconciliation_status"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("tool %s properties missing %q", name, field)
			}
		}
	}
}

func TestResolveRouteMapsCountSecurityAlertReconciliations(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("count_security_alert_reconciliations", map[string]any{
		"repository_id":         "repo-1",
		"reconciliation_status": "resolved",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/security-alerts/reconciliations/count"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestResolveRouteMapsGetSecurityAlertReconciliationInventory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_security_alert_reconciliation_inventory", map[string]any{
		"repository_id": "repo-1",
		"group_by":      "provider",
		"limit":         float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/security-alerts/reconciliations/inventory"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}
