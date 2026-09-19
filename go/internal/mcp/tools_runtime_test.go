// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestListCollectorsRuntimeToolRoutesToStatusCollectors(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_collectors", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/collectors"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestListIngestersRuntimeToolRoutesToStatusIngesters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_ingesters", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/ingesters"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestGetIngesterStatusRuntimeToolRoutesToRepositoryStatus(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "default_repository",
			args: map[string]any{},
			want: "/api/v0/status/ingesters/repository",
		},
		{
			name: "explicit_repository",
			args: map[string]any{"ingester": "repository"},
			want: "/api/v0/status/ingesters/repository",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute("get_ingester_status", tc.args)
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			if got, want := route.method, "GET"; got != want {
				t.Fatalf("route.method = %q, want %q", got, want)
			}
			if got := route.path; got != tc.want {
				t.Fatalf("route.path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSemanticCapabilityRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_semantic_capability_status", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/semantic-extraction"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestHostedReadinessRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_hosted_readiness", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/hosted-readiness"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestHostedGovernanceRuntimeToolRoutesToStatus(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_hosted_governance_status", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/status/governance"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestCapabilityCatalogRuntimeToolRoutesToCapabilities(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_capability_catalog", map[string]any{
		"maturity": "gated",
		"owner":    "internal/query",
		"limit":    50,
		"offset":   10,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/capabilities"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["maturity"], "gated"; got != want {
		t.Fatalf("query maturity = %q, want %q", got, want)
	}
	if got, want := route.query["limit"], "50"; got != want {
		t.Fatalf("query limit = %q, want %q", got, want)
	}
}

// TestCapabilityCatalogRuntimeToolSchemaBoundsLimitAndOffset proves
// get_capability_catalog's input schema carries JSON-schema bounds on limit
// and offset, not just a description, so an MCP client (or a validating
// gateway) rejects an out-of-range value before it reaches the handler
// (#6795 review finding).
func TestCapabilityCatalogRuntimeToolSchemaBoundsLimitAndOffset(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	for _, tool := range ReadOnlyTools() {
		if tool.Name == "get_capability_catalog" {
			schema = tool.InputSchema.(map[string]any)
			break
		}
	}
	if schema == nil {
		t.Fatal("get_capability_catalog tool not found")
	}
	properties := schema["properties"].(map[string]any)

	limit := properties["limit"].(map[string]any)
	if got, want := limit["minimum"], 1; got != want {
		t.Fatalf("limit minimum = %#v, want %v", got, want)
	}
	if got, want := limit["maximum"], 500; got != want {
		t.Fatalf("limit maximum = %#v, want %v", got, want)
	}

	offset := properties["offset"].(map[string]any)
	if got, want := offset["minimum"], 0; got != want {
		t.Fatalf("offset minimum = %#v, want %v", got, want)
	}
}

func TestCapabilityCatalogRuntimeToolOmitsEmptyFilters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_capability_catalog", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if _, ok := route.query["maturity"]; ok {
		t.Fatal("empty maturity must not be forwarded")
	}
	if _, ok := route.query["owner"]; ok {
		t.Fatal("empty owner must not be forwarded")
	}
	if got, want := route.query["limit"], "12"; got != want {
		t.Fatalf("default limit = %q, want %q", got, want)
	}
}
