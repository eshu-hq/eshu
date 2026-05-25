package mcp

import "testing"

func TestResolveRouteMapsAdvisoryEvidenceToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_advisory_evidence", map[string]any{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"package_id":         "pkg:npm/example",
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
