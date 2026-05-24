package mcp

import "testing"

func TestResolveRouteMapsSecurityAlertReconciliationsToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_security_alert_reconciliations", map[string]any{
		"repository_id":           "repo://github/eshu-hq/eshu",
		"provider":                "github_dependabot",
		"reconciliation_status":   "matched",
		"provider_state":          "open",
		"package_id":              "npm://registry.npmjs.org/left-pad",
		"cve_id":                  "CVE-2026-0001",
		"after_reconciliation_id": "reconciliation-1",
		"limit":                   25,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/supply-chain/security-alerts/reconciliations"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["repository_id"], "repo://github/eshu-hq/eshu"; got != want {
		t.Fatalf("route.query[repository_id] = %q, want %q", got, want)
	}
	if got, want := route.query["reconciliation_status"], "matched"; got != want {
		t.Fatalf("route.query[reconciliation_status] = %q, want %q", got, want)
	}
	if got, want := route.query["limit"], "25"; got != want {
		t.Fatalf("route.query[limit] = %q, want %q", got, want)
	}
}
