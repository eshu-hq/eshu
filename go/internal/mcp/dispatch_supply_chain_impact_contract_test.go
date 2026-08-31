// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
	supplychainimpacttools "github.com/eshu-hq/eshu/go/internal/mcp/supplychainimpact"
)

// supplyChainImpactRouteTools lists every tool the child package owns.
var supplyChainImpactRouteTools = []string{
	"list_supply_chain_impact_findings",
	"count_supply_chain_impact_findings",
	"get_supply_chain_impact_inventory",
	"explain_supply_chain_impact",
}

// supplyChainImpactDispatchPaths pins each tool's path as dispatch selects it.
var supplyChainImpactDispatchPaths = map[string]string{
	"list_supply_chain_impact_findings":  "/api/v0/supply-chain/impact/findings",
	"count_supply_chain_impact_findings": "/api/v0/supply-chain/impact/findings/count",
	"get_supply_chain_impact_inventory":  "/api/v0/supply-chain/impact/inventory",
	"explain_supply_chain_impact":        "/api/v0/supply-chain/impact/explain",
}

// supplyChainImpactDispatchQueryKeys is the exact key set each request must
// still carry through dispatch, where the handler reads it.
var supplyChainImpactDispatchQueryKeys = map[string][]string{
	"list_supply_chain_impact_findings": {
		"advisory_id", "after_finding_id", "cve_id", "ecosystem", "environment",
		"ghsa_id", "image_ref", "impact_status", "include_suppressed", "limit",
		"min_priority_score", "osv_id", "package_id", "priority_bucket",
		"profile", "repository_id", "service_id", "severity", "sort",
		"subject_digest", "suppression_state", "workload_id",
	},
	"count_supply_chain_impact_findings": {
		"advisory_id", "cve_id", "ecosystem", "environment", "ghsa_id",
		"image_ref", "impact_status", "include_suppressed", "min_priority_score",
		"osv_id", "package_id", "priority_bucket", "profile", "repository_id",
		"service_id", "severity", "subject_digest", "suppression_state",
		"workload_id",
	},
	"get_supply_chain_impact_inventory": {
		"advisory_id", "cve_id", "ecosystem", "environment", "ghsa_id",
		"group_by", "image_ref", "impact_status", "include_suppressed", "limit",
		"min_priority_score", "offset", "osv_id", "package_id",
		"priority_bucket", "profile", "repository_id", "service_id",
		"severity", "subject_digest", "suppression_state", "workload_id",
	},
	"explain_supply_chain_impact": {
		"advisory_id", "cve_id", "finding_id", "image_ref", "package_id",
		"repository_id", "service_id", "subject_digest", "workload_id",
	},
}

func TestResolveRouteUsesExactSupplyChainImpactChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"advisory_id":        "GHSA-aaaa-bbbb-cccc",
			"after_finding_id":   "finding-1",
			"cve_id":             "CVE-2026-0001",
			"finding_id":         "finding-42",
			"group_by":           "priority_bucket",
			"image_ref":          "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"include_suppressed": true,
			"limit":              float64(25),
			"min_priority_score": float64(75),
			"offset":             float64(10),
			"package_id":         "pkg:maven/example/component@1.0.0",
			"repository_id":      "repo://example/api",
			"service_id":         "service:example-api",
			"severity":           "critical",
			"sort":               "priority",
			"subject_digest":     "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
			"suppression_state":  "accepted_risk",
			"workload_id":        "workload:example-api",
		}},
		{name: "repository only", args: map[string]any{"repository_id": "repo://example/api"}},
		{name: "malformed", args: map[string]any{
			"finding_id":         42,
			"group_by":           []string{"impact_status"},
			"limit":              "25",
			"offset":             true,
			"repository_id":      struct{}{},
			"severity":           nil,
			"include_suppressed": "true",
		}},
	}

	for _, tool := range supplyChainImpactRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := supplychainimpacttools.Route(tool, routecontract.Arguments(tt.args))
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

// TestSupplyChainImpactDispatchKeepsEveryQueryKey proves each key survives
// the adapter boundary against literal expectations rather than against the
// child selector. The failure shapes differ by tool: the listing 400s
// without limit and without a scope anchor, the explanation 400s without
// finding_id or an advisory/CVE anchor plus a scope leg, and a dropped
// aggregate filter returns 200 over a wider scope than the caller asked for.
func TestSupplyChainImpactDispatchKeepsEveryQueryKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"after_finding_id":   "finding-1",
		"cve_id":             "CVE-2026-0001",
		"ecosystem":          "maven",
		"environment":        "production",
		"finding_id":         "finding-42",
		"ghsa_id":            "GHSA-dddd-eeee-ffff",
		"group_by":           "priority_bucket",
		"image_ref":          "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"impact_status":      "affected_exact",
		"include_suppressed": true,
		"limit":              float64(25),
		"min_priority_score": float64(75),
		"offset":             float64(10),
		"osv_id":             "OSV-2026-0001",
		"package_id":         "pkg:maven/example/component@1.0.0",
		"priority_bucket":    "high",
		"profile":            "comprehensive",
		"repository_id":      "repo://example/api",
		"service_id":         "service:example-api",
		"severity":           "critical",
		"sort":               "priority",
		"subject_digest":     "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"suppression_state":  "accepted_risk",
		"workload_id":        "workload:example-api",
	}
	want := map[string]string{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"after_finding_id":   "finding-1",
		"cve_id":             "CVE-2026-0001",
		"ecosystem":          "maven",
		"environment":        "production",
		"finding_id":         "finding-42",
		"ghsa_id":            "GHSA-dddd-eeee-ffff",
		"group_by":           "priority_bucket",
		"image_ref":          "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"impact_status":      "affected_exact",
		"include_suppressed": "true",
		"limit":              "25",
		"min_priority_score": "75",
		"offset":             "10",
		"osv_id":             "OSV-2026-0001",
		"package_id":         "pkg:maven/example/component@1.0.0",
		"priority_bucket":    "high",
		"profile":            "comprehensive",
		"repository_id":      "repo://example/api",
		"service_id":         "service:example-api",
		"severity":           "critical",
		"sort":               "priority",
		"subject_digest":     "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"suppression_state":  "accepted_risk",
		"workload_id":        "workload:example-api",
	}

	for _, tool := range supplyChainImpactRouteTools {
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if got.method != "GET" {
			t.Errorf("%s: method = %q, want GET", tool, got.method)
		}
		if wantPath := supplyChainImpactDispatchPaths[tool]; got.path != wantPath {
			t.Errorf("%s: path = %q, want %q", tool, got.path, wantPath)
		}
		if got.body != nil {
			t.Errorf("%s: body = %#v, want nil", tool, got.body)
		}
		keys := supplyChainImpactDispatchQueryKeys[tool]
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
		{tool: "list_supply_chain_impact_findings", wantLimit: "50"},
		{tool: "count_supply_chain_impact_findings"},
		{tool: "get_supply_chain_impact_inventory", wantLimit: "100", wantOffset: "0", wantGroup: "impact_status"},
		{tool: "explain_supply_chain_impact"},
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

// TestRepositoryRouteStillOwnsItsArmsAfterSupplyChainImpact proves the
// seventh delegation added in front of the repository switch claims only
// this family. Two of this family's builders came out of
// dispatch_supply_chain.go, so the four supply-chain tools whose builders
// stayed there are listed explicitly alongside the neighboring arms and the
// earlier extracted families.
func TestRepositoryRouteStillOwnsItsArmsAfterSupplyChainImpact(t *testing.T) {
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
		"list_advisory_evidence",
		"get_vulnerability_scanner_read_contract",
		"list_security_alert_reconciliations",
		"count_security_alert_reconciliations",
		"list_sbom_attestation_attachments",
		"count_sbom_attestation_attachments",
		"get_repository_stats",
	} {
		if _, handled := supplyChainImpactRoute(tool, map[string]any{}); handled {
			t.Errorf("supplyChainImpactRoute(%s) handled = true, want false", tool)
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

// TestSupplyChainImpactRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the four owned names are claimed, and
// near-miss names are not.
func TestSupplyChainImpactRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range supplyChainImpactRouteTools {
		if _, handled := supplyChainImpactRoute(tool, map[string]any{}); !handled {
			t.Errorf("supplyChainImpactRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
		"list_supply_chain_impact_finding",
		"list_supply_chain_impact_findings_extra",
		"count_supply_chain_impact_finding",
		"get_supply_chain_impact_inventories",
		"explain_supply_chain_impacts",
		"supply_chain_impact_findings",
		"LIST_SUPPLY_CHAIN_IMPACT_FINDINGS",
		"not_a_tool",
	} {
		if _, handled := supplyChainImpactRoute(tool, map[string]any{}); handled {
			t.Errorf("supplyChainImpactRoute(%q) handled = true, want false", tool)
		}
	}
}
