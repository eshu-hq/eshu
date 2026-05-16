package mcp

import "testing"

func TestResolveRouteMapsSupplyChainImpactFindingsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_supply_chain_impact_findings", map[string]any{
		"after_finding_id": "finding-1",
		"cve_id":           "CVE-2026-0001",
		"impact_status":    "affected_exact",
		"limit":            float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/impact/findings"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["cve_id"], "CVE-2026-0001"; got != want {
		t.Fatalf("route.query[cve_id] = %#v, want %#v", got, want)
	}
	if got, want := route.query["impact_status"], "affected_exact"; got != want {
		t.Fatalf("route.query[impact_status] = %#v, want %#v", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %#v, want %#v", got, want)
	}
}
