// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsAdvisoryEvidenceToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_advisory_evidence", map[string]any{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"package_id":         "pkg:npm/example",
		"repository_id":      "repo://example/api",
		"service_id":         "service:payments-api",
		"source":             "osv",
		"workload_id":        "workload:payments-api",
		"after_advisory_key": "CVE-2026-0001",
		"limit":              float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/advisories/evidence"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["advisory_id"], "GHSA-aaaa-bbbb-cccc"; got != want {
		t.Fatalf("route.query[advisory_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["package_id"], "pkg:npm/example"; got != want {
		t.Fatalf("route.query[package_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["repository_id"], "repo://example/api"; got != want {
		t.Fatalf("route.query[repository_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["service_id"], "service:payments-api"; got != want {
		t.Fatalf("route.query[service_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["workload_id"], "workload:payments-api"; got != want {
		t.Fatalf("route.query[workload_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["source"], "osv"; got != want {
		t.Fatalf("route.query[source] = %#v, want %#v", got, want)
	}
	if got, want := route.query["after_advisory_key"], "CVE-2026-0001"; got != want {
		t.Fatalf("route.query[after_advisory_key] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}

func TestAdvisoryEvidenceToolSchemaAdvertisesRepositoryScope(t *testing.T) {
	t.Parallel()

	var tool ToolDefinition
	for _, candidate := range supplyChainTools() {
		if candidate.Name == "list_advisory_evidence" {
			tool = candidate
			break
		}
	}
	if tool.Name == "" {
		t.Fatal("list_advisory_evidence tool missing")
	}
	inputSchema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := inputSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", inputSchema["properties"])
	}
	for _, property := range []string{"repository_id", "service_id", "workload_id"} {
		property := property
		schema, ok := properties[property].(map[string]any)
		if !ok {
			t.Fatalf("%s schema = %T, want map[string]any", property, properties[property])
		}
		if got, want := schema["type"], "string"; got != want {
			t.Fatalf("%s.type = %#v, want %#v", property, got, want)
		}
		description, _ := schema["description"].(string)
		if !containsAll(description, "impact", "finding") {
			t.Fatalf("%s.description = %q, want impact-finding semantics", property, description)
		}
	}
}
