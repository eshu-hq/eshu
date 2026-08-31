// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	securityalerttools "github.com/eshu-hq/eshu/go/internal/mcp/securityalert"
)

// securityAlertRouteTools lists every tool the child package owns.
var securityAlertRouteTools = []string{
	"list_security_alert_reconciliations",
	"count_security_alert_reconciliations",
	"get_security_alert_reconciliation_inventory",
}

// securityAlertDispatchPaths pins each tool's path as dispatch selects it.
var securityAlertDispatchPaths = map[string]string{
	"list_security_alert_reconciliations":         "/api/v0/supply-chain/security-alerts/reconciliations",
	"count_security_alert_reconciliations":        "/api/v0/supply-chain/security-alerts/reconciliations/count",
	"get_security_alert_reconciliation_inventory": "/api/v0/supply-chain/security-alerts/reconciliations/inventory",
}

// securityAlertDispatchQueryKeys is the exact key set each request must still
// carry through dispatch, where the handler reads it.
var securityAlertDispatchQueryKeys = map[string][]string{
	"list_security_alert_reconciliations": {
		"after_reconciliation_id", "cve_id", "ghsa_id", "limit", "package_id",
		"provider", "provider_state", "reconciliation_status", "repository_id",
	},
	"count_security_alert_reconciliations": {
		"cve_id", "ghsa_id", "package_id", "provider", "provider_state",
		"reconciliation_status", "repository_id",
	},
	"get_security_alert_reconciliation_inventory": {
		"cve_id", "ghsa_id", "group_by", "limit", "offset", "package_id",
		"provider", "provider_state", "reconciliation_status", "repository_id",
	},
}

func TestResolveRouteUsesExactSecurityAlertChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"after_reconciliation_id": "reconciliation-1",
			"cve_id":                  "CVE-2026-0001",
			"ghsa_id":                 "GHSA-aaaa-bbbb-cccc",
			"group_by":                "provider",
			"limit":                   float64(25),
			"offset":                  float64(10),
			"package_id":              "npm://registry.npmjs.org/left-pad",
			"provider":                "github_dependabot",
			"provider_state":          "open",
			"reconciliation_status":   "matched",
			"repository_id":           "repo://github/eshu-hq/eshu",
		}},
		{name: "repository only", args: map[string]any{"repository_id": "repo://github/eshu-hq/eshu"}},
		{name: "malformed", args: map[string]any{
			"group_by":              []string{"provider"},
			"limit":                 "25",
			"offset":                true,
			"repository_id":         struct{}{},
			"reconciliation_status": nil,
		}},
	}

	for _, tool := range securityAlertRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := securityalerttools.Route(tool, routecontract.Arguments(tt.args))
			if !handled {
				t.Fatalf("child Route(%s) handled = false, want true", tool)
			}
			// This comparison proves the adapter transcribes method, path,
			// body, and query faithfully. It cannot prove the child selected
			// the right values -- both sides come from the same selector -- so
			// the literal path and key assertions below carry that claim.
			want := &route{
				method: request.Method,
				path:   request.Path,
				body:   request.Body,
				query:  request.Query,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolveRoute(%s, %s) = %#v, want child request %#v", tool, tt.name, got, want)
			}
		}
	}
}

// TestSecurityAlertDispatchKeepsEveryQueryKey proves each key survives the
// adapter boundary against literal expectations rather than against the
// child selector. The failure shapes differ by tool: the listing 400s
// without limit or a scope anchor, and a dropped aggregate filter returns
// 200 over a wider scope than the caller asked for.
func TestSecurityAlertDispatchKeepsEveryQueryKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"after_reconciliation_id": "reconciliation-1",
		"cve_id":                  "CVE-2026-0001",
		"ghsa_id":                 "GHSA-aaaa-bbbb-cccc",
		"group_by":                "provider",
		"limit":                   float64(25),
		"offset":                  float64(10),
		"package_id":              "npm://registry.npmjs.org/left-pad",
		"provider":                "github_dependabot",
		"provider_state":          "open",
		"reconciliation_status":   "matched",
		"repository_id":           "repo://github/eshu-hq/eshu",
	}
	want := map[string]string{
		"after_reconciliation_id": "reconciliation-1",
		"cve_id":                  "CVE-2026-0001",
		"ghsa_id":                 "GHSA-aaaa-bbbb-cccc",
		"group_by":                "provider",
		"limit":                   "25",
		"offset":                  "10",
		"package_id":              "npm://registry.npmjs.org/left-pad",
		"provider":                "github_dependabot",
		"provider_state":          "open",
		"reconciliation_status":   "matched",
		"repository_id":           "repo://github/eshu-hq/eshu",
	}

	for _, tool := range securityAlertRouteTools {
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if got.method != "GET" {
			t.Errorf("%s: method = %q, want GET", tool, got.method)
		}
		if wantPath := securityAlertDispatchPaths[tool]; got.path != wantPath {
			t.Errorf("%s: path = %q, want %q", tool, got.path, wantPath)
		}
		if got.body != nil {
			t.Errorf("%s: body = %#v, want nil", tool, got.body)
		}
		keys := securityAlertDispatchQueryKeys[tool]
		if n, wantN := len(got.query), len(keys); n != wantN {
			t.Fatalf("%s: query carries %d keys (%#v), want %d", tool, n, got.query, wantN)
		}
		for _, key := range keys {
			value, present := got.query[key]
			if !present {
				t.Errorf("%s: dispatch dropped %q entirely", tool, key)
				continue
			}
			if value != want[key] {
				t.Errorf("%s: query[%s] = %q, want %q", tool, key, value, want[key])
			}
		}
	}

	// Paging defaults are not uniform across the family, and only the
	// inventory falls back to a grouping dimension.
	for _, tt := range []struct {
		tool       string
		wantLimit  string
		wantOffset string
		wantGroup  string
	}{
		{tool: "list_security_alert_reconciliations", wantLimit: "50"},
		{tool: "count_security_alert_reconciliations"},
		{tool: "get_security_alert_reconciliation_inventory", wantLimit: "100", wantOffset: "0", wantGroup: "reconciliation_status"},
	} {
		bare, err := resolveRoute(tt.tool, map[string]any{})
		if err != nil {
			t.Fatalf("resolveRoute(%s, empty) error = %v, want nil", tt.tool, err)
		}
		if got := bare.query["limit"]; got != tt.wantLimit {
			t.Errorf("%s: absent limit -> %q, want %q", tt.tool, got, tt.wantLimit)
		}
		if got := bare.query["offset"]; got != tt.wantOffset {
			t.Errorf("%s: absent offset -> %q, want %q", tt.tool, got, tt.wantOffset)
		}
		if got := bare.query["group_by"]; got != tt.wantGroup {
			t.Errorf("%s: absent group_by -> %q, want %q", tt.tool, got, tt.wantGroup)
		}
	}
}

// TestRepositoryRouteStillOwnsItsArmsAfterSecurityAlert proves the eighth
// delegation added in front of the repository switch claims only this
// family. This family's listing builder came out of dispatch_supply_chain.go,
// so the remaining supply-chain tools whose builders stayed there are listed
// explicitly alongside the neighboring arms and the earlier extracted
// families.
func TestRepositoryRouteStillOwnsItsArmsAfterSecurityAlert(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"list_ci_cd_run_correlations",
		"list_codeowners_ownership",
		"list_secrets_iam_posture_gaps",
		"list_observability_coverage_correlations",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_container_image_identities",
		"count_container_image_identities",
		"get_container_image_identity_inventory",
		"list_advisory_evidence",
		"get_vulnerability_scanner_read_contract",
		"list_supply_chain_impact_findings",
		"count_supply_chain_impact_findings",
		"get_supply_chain_impact_inventory",
		"explain_supply_chain_impact",
		"list_sbom_attestation_attachments",
		"count_sbom_attestation_attachments",
		"get_sbom_attestation_attachment_inventory",
		"get_repository_stats",
	} {
		if _, handled := securityAlertRoute(tool, map[string]any{}); handled {
			t.Errorf("securityAlertRoute(%s) handled = true, want false", tool)
		}
		got, ok, err := repositoryRoute(tool, map[string]any{})
		if err != nil {
			t.Errorf("repositoryRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if !ok || got == nil {
			t.Errorf("repositoryRoute(%s) ok = %v, route = %v, want a route", tool, ok, got)
		}
	}

	// An unknown tool still falls through the repository switch untouched.
	if got, ok, err := repositoryRoute("not_a_tool", map[string]any{}); ok || got != nil || err != nil {
		t.Fatalf("repositoryRoute(not_a_tool) = (%v, %v, %v), want (nil, false, nil)", got, ok, err)
	}
	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestSecurityAlertRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the three owned names are claimed, and
// near-miss names are not.
func TestSecurityAlertRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range securityAlertRouteTools {
		if _, handled := securityAlertRoute(tool, map[string]any{}); !handled {
			t.Errorf("securityAlertRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
		"list_security_alert_reconciliation",
		"list_security_alert_reconciliations_extra",
		"count_security_alert_reconciliation",
		"get_security_alert_reconciliation_inventories",
		"security_alert_reconciliations",
		"LIST_SECURITY_ALERT_RECONCILIATIONS",
		"not_a_tool",
	} {
		if _, handled := securityAlertRoute(tool, map[string]any{}); handled {
			t.Errorf("securityAlertRoute(%q) handled = true, want false", tool)
		}
	}
}
