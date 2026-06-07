package mcp

import "testing"

func TestResolveRouteMapsAdvisoryEvidenceToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_advisory_evidence", map[string]any{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"package_id":         "pkg:npm/example",
		"repository_id":      "repo://example/api",
		"service_id":         "service:api",
		"workload_id":        "workload:api",
		"source":             "osv",
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
	if got, want := route.query["service_id"], "service:api"; got != want {
		t.Fatalf("route.query[service_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["workload_id"], "workload:api"; got != want {
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

func TestAdvisoryEvidenceToolAdvertisesTargetScopes(t *testing.T) {
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
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema = %T, want map[string]any", tool.InputSchema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %T, want map[string]any", schema["properties"])
	}
	for _, want := range []string{"repository_id", "workload_id", "service_id"} {
		if _, ok := properties[want]; !ok {
			t.Fatalf("list_advisory_evidence schema missing %q", want)
		}
	}
}
