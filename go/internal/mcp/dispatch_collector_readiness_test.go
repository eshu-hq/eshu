// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsCollectorReadinessToStatusRoute(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_collector_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	// API and MCP must agree: the MCP tool maps to the same API route the
	// StatusHandler serves, so both surfaces return identical readiness truth.
	if got, want := route.path, "/api/v0/status/collector-readiness"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestCollectorReadinessToolIsAdvertised(t *testing.T) {
	t.Parallel()

	var tool ToolDefinition
	for _, candidate := range runtimeTools() {
		if candidate.Name == "get_collector_readiness" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("get_collector_readiness tool missing from runtimeTools")
	}
	if tool.Description == "" {
		t.Fatal("get_collector_readiness tool missing description")
	}
}
