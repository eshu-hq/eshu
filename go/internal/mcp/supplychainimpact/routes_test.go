// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychainimpacttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyTools is the exact set of names this package owns.
var familyTools = []string{
	"list_supply_chain_impact_findings",
	"count_supply_chain_impact_findings",
	"get_supply_chain_impact_inventory",
	"explain_supply_chain_impact",
}

// queryKeys pins the exact key set each of the four requests sends. The count
// and the inventory share fourteen filters plus include_suppressed, but their
// paths and their paging keys differ, and the listing and explanation carry
// their own unrelated shapes, so each set is spelled out rather than derived
// from a shared base.
var queryKeys = map[string][]string{
	"list_supply_chain_impact_findings": {
		"advisory_id",
		"after_finding_id",
		"cve_id",
		"ecosystem",
		"environment",
		"ghsa_id",
		"image_ref",
		"impact_status",
		"include_suppressed",
		"limit",
		"min_priority_score",
		"osv_id",
		"package_id",
		"priority_bucket",
		"profile",
		"repository_id",
		"service_id",
		"severity",
		"sort",
		"subject_digest",
		"suppression_state",
		"workload_id",
	},
	"count_supply_chain_impact_findings": {
		"advisory_id",
		"cve_id",
		"ecosystem",
		"environment",
		"ghsa_id",
		"image_ref",
		"impact_status",
		"include_suppressed",
		"min_priority_score",
		"osv_id",
		"package_id",
		"priority_bucket",
		"profile",
		"repository_id",
		"service_id",
		"severity",
		"subject_digest",
		"suppression_state",
		"workload_id",
	},
	"get_supply_chain_impact_inventory": {
		"advisory_id",
		"cve_id",
		"ecosystem",
		"environment",
		"ghsa_id",
		"group_by",
		"image_ref",
		"impact_status",
		"include_suppressed",
		"limit",
		"min_priority_score",
		"offset",
		"osv_id",
		"package_id",
		"priority_bucket",
		"profile",
		"repository_id",
		"service_id",
		"severity",
		"subject_digest",
		"suppression_state",
		"workload_id",
	},
	"explain_supply_chain_impact": {
		"advisory_id",
		"cve_id",
		"finding_id",
		"image_ref",
		"package_id",
		"repository_id",
		"service_id",
		"subject_digest",
		"workload_id",
	},
}

// wantPaths pins each tool's path. All four sit under the same
// /api/v0/supply-chain/impact prefix, unlike the container-image family's
// asymmetric tag-history route.
var wantPaths = map[string]string{
	"list_supply_chain_impact_findings":  "/api/v0/supply-chain/impact/findings",
	"count_supply_chain_impact_findings": "/api/v0/supply-chain/impact/findings/count",
	"get_supply_chain_impact_inventory":  "/api/v0/supply-chain/impact/inventory",
	"explain_supply_chain_impact":        "/api/v0/supply-chain/impact/explain",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// a request builder fail the exact comparison below instead of passing on a
// shared value. unused_decoy must never reach a query. ghsa_id and osv_id are
// included even though the handler folds them into advisory_id itself; this
// route forwards the raw wire key, not the handler's fallback.
var populatedArguments = routecontract.Arguments{
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
	"unused_decoy":       "ignored",
}

// wantPopulatedRequests is the request each tool must select from
// populatedArguments.
var wantPopulatedRequests = map[string]routecontract.Request{
	"list_supply_chain_impact_findings": {Method: "GET", Path: "/api/v0/supply-chain/impact/findings", Query: map[string]string{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"after_finding_id":   "finding-1",
		"cve_id":             "CVE-2026-0001",
		"ecosystem":          "maven",
		"environment":        "production",
		"ghsa_id":            "GHSA-dddd-eeee-ffff",
		"image_ref":          "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"impact_status":      "affected_exact",
		"include_suppressed": "true",
		"limit":              "25",
		"min_priority_score": "75",
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
	}},
	"count_supply_chain_impact_findings": {Method: "GET", Path: "/api/v0/supply-chain/impact/findings/count", Query: map[string]string{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"cve_id":             "CVE-2026-0001",
		"ecosystem":          "maven",
		"environment":        "production",
		"ghsa_id":            "GHSA-dddd-eeee-ffff",
		"image_ref":          "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"impact_status":      "affected_exact",
		"include_suppressed": "true",
		"min_priority_score": "75",
		"osv_id":             "OSV-2026-0001",
		"package_id":         "pkg:maven/example/component@1.0.0",
		"priority_bucket":    "high",
		"profile":            "comprehensive",
		"repository_id":      "repo://example/api",
		"service_id":         "service:example-api",
		"severity":           "critical",
		"subject_digest":     "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"suppression_state":  "accepted_risk",
		"workload_id":        "workload:example-api",
	}},
	"get_supply_chain_impact_inventory": {Method: "GET", Path: "/api/v0/supply-chain/impact/inventory", Query: map[string]string{
		"advisory_id":        "GHSA-aaaa-bbbb-cccc",
		"cve_id":             "CVE-2026-0001",
		"ecosystem":          "maven",
		"environment":        "production",
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
		"subject_digest":     "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"suppression_state":  "accepted_risk",
		"workload_id":        "workload:example-api",
	}},
	"explain_supply_chain_impact": {Method: "GET", Path: "/api/v0/supply-chain/impact/explain", Query: map[string]string{
		"advisory_id":    "GHSA-aaaa-bbbb-cccc",
		"cve_id":         "CVE-2026-0001",
		"finding_id":     "finding-42",
		"image_ref":      "registry.example.com/team/api@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"package_id":     "pkg:maven/example/component@1.0.0",
		"repository_id":  "repo://example/api",
		"service_id":     "service:example-api",
		"subject_digest": "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"workload_id":    "workload:example-api",
	}},
}

func TestRouteOwnsExactlyTheSupplyChainImpactFamily(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", toolName)
			continue
		}
		if request.Method != "GET" {
			t.Errorf("Route(%s) method = %q, want GET", toolName, request.Method)
		}
		if request.Body != nil {
			t.Errorf("Route(%s) body = %#v, want nil", toolName, request.Body)
		}
	}

	// Neighbours left in the root repository switch, the other extracted
	// families, and near-miss names: this package must claim none of them.
	// The vulnerability-scanner, advisory-evidence, security-alert, and SBOM
	// siblings matter most -- this family's four builders came out of
	// dispatch_supply_chain.go and dispatch_supply_chain_aggregates.go, and
	// the four that stayed in dispatch_supply_chain.go are still answered by
	// the switch.
	for _, toolName := range []string{
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
		"list_container_image_tag_history",
		"count_container_image_identities",
		"get_container_image_identity_inventory",
		"list_advisory_evidence",
		"get_vulnerability_scanner_read_contract",
		"list_security_alert_reconciliations",
		"count_security_alert_reconciliations",
		"get_security_alert_reconciliation_inventory",
		"list_sbom_attestation_attachments",
		"count_sbom_attestation_attachments",
		"get_sbom_attestation_attachment_inventory",
		"get_repository_stats",
		"list_supply_chain_impact_finding",
		"list_supply_chain_impact_findings_extra",
		"list_supply_chain_impact_findings ",
		"count_supply_chain_impact_finding",
		"get_supply_chain_impact_inventories",
		"explain_supply_chain_impacts",
		"supply_chain_impact_findings",
		"LIST_SUPPLY_CHAIN_IMPACT_FINDINGS",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesSupplyChainImpactRequestContract(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		request, handled := Route(toolName, populatedArguments)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		want := wantPopulatedRequests[toolName]
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("Route(%s) = %#v, want %#v", toolName, request, want)
		}
	}
}

func TestRouteUsesTheSharedSupplyChainImpactPathPrefix(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		if got, want := request.Path, wantPaths[toolName]; got != want {
			t.Errorf("Route(%s) path = %q, want %q", toolName, got, want)
		}
	}
}

// TestRouteCarriesEverySupplyChainImpactQueryKey pins each key on its own.
// The exact-request comparison already covers the set, but a per-key
// assertion names the dropped key: the listing 400s without limit or a scope
// anchor, the explanation 400s without finding_id or an advisory/CVE plus a
// scope leg, and a dropped aggregate filter silently widens the count or the
// inventory bucket the caller asked for.
func TestRouteCarriesEverySupplyChainImpactQueryKey(t *testing.T) {
	t.Parallel()

	// Keys that belong to a sibling in this family, not to the tool under
	// test. Leaking one across would send a filter the handler ignores or,
	// worse, a paging key the route does not support.
	foreignKeys := map[string][]string{
		"list_supply_chain_impact_findings":  {"group_by", "offset", "finding_id"},
		"count_supply_chain_impact_findings": {"after_finding_id", "sort", "group_by", "offset", "limit", "finding_id"},
		"get_supply_chain_impact_inventory":  {"after_finding_id", "sort", "finding_id"},
		"explain_supply_chain_impact":        {"after_finding_id", "ecosystem", "environment", "ghsa_id", "impact_status", "limit", "min_priority_score", "osv_id", "priority_bucket", "profile", "severity", "sort", "suppression_state", "include_suppressed", "group_by", "offset"},
	}

	for _, toolName := range familyTools {
		request, handled := Route(toolName, populatedArguments)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		want := wantPopulatedRequests[toolName].Query
		keys := queryKeys[toolName]
		if got, wantN := len(request.Query), len(keys); got != wantN {
			t.Fatalf("Route(%s) query carries %d keys (%#v), want %d", toolName, got, request.Query, wantN)
		}
		for _, key := range keys {
			value, present := request.Query[key]
			if !present {
				t.Errorf("Route(%s) dropped %q entirely", toolName, key)
				continue
			}
			if value != want[key] {
				t.Errorf("Route(%s) query[%s] = %q, want %q", toolName, key, value, want[key])
			}
		}
		for _, key := range foreignKeys[toolName] {
			if value, present := request.Query[key]; present {
				t.Errorf("Route(%s) carries sibling key %q = %q, want it absent", toolName, key, value)
			}
		}
		if _, present := request.Query["unused_decoy"]; present {
			t.Errorf("Route(%s) forwarded an unrelated argument", toolName)
		}
	}
}

// TestRouteAppliesSupplyChainImpactLimitAndOffsetDefaults pins the two paging
// defaults. The listing defaults limit to 50; the inventory defaults limit to
// 100 and offset to 0. The count and the explanation carry neither key.
func TestRouteAppliesSupplyChainImpactLimitAndOffsetDefaults(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_supply_chain_impact_findings", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_supply_chain_impact_findings) handled = false, want true")
	}
	if got, want := request.Query["limit"], "50"; got != want {
		t.Errorf("listing absent limit -> %q, want %q", got, want)
	}

	inventory, handled := Route("get_supply_chain_impact_inventory", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(get_supply_chain_impact_inventory) handled = false, want true")
	}
	if got, want := inventory.Query["limit"], "100"; got != want {
		t.Errorf("inventory absent limit -> %q, want %q", got, want)
	}
	if got, want := inventory.Query["offset"], "0"; got != want {
		t.Errorf("inventory absent offset -> %q, want %q", got, want)
	}

	count, handled := Route("count_supply_chain_impact_findings", routecontract.Arguments{"limit": 25, "offset": 5})
	if !handled {
		t.Fatal("Route(count_supply_chain_impact_findings) handled = false, want true")
	}
	for _, key := range []string{"limit", "offset"} {
		if value, present := count.Query[key]; present {
			t.Errorf("count route carries %q = %q, want no paging key", key, value)
		}
	}

	explanation, handled := Route("explain_supply_chain_impact", routecontract.Arguments{"limit": 25, "offset": 5})
	if !handled {
		t.Fatal("Route(explain_supply_chain_impact) handled = false, want true")
	}
	for _, key := range []string{"limit", "offset"} {
		if value, present := explanation.Query[key]; present {
			t.Errorf("explanation route carries %q = %q, want no paging key", key, value)
		}
	}
}

func TestRouteAppliesSupplyChainImpactNumericCoercions(t *testing.T) {
	t.Parallel()

	// Numeric coercion follows routecontract.Arguments.IntOr exactly: int,
	// int64, and float64 are accepted, a float64 truncates toward zero, and
	// every other type falls back to the default.
	for _, tt := range []struct {
		value         any
		wantFindings  string
		wantInventory string
		wantOffset    string
	}{
		{value: 25, wantFindings: "25", wantInventory: "25", wantOffset: "25"},
		{value: int64(26), wantFindings: "26", wantInventory: "26", wantOffset: "26"},
		{value: 27.9, wantFindings: "27", wantInventory: "27", wantOffset: "27"},
		{value: -3.9, wantFindings: "-3", wantInventory: "-3", wantOffset: "-3"},
		{value: -7, wantFindings: "-7", wantInventory: "-7", wantOffset: "-7"},
		{value: 0, wantFindings: "0", wantInventory: "0", wantOffset: "0"},
		{value: "25", wantFindings: "50", wantInventory: "100", wantOffset: "0"},
		{value: true, wantFindings: "50", wantInventory: "100", wantOffset: "0"},
		{value: nil, wantFindings: "50", wantInventory: "100", wantOffset: "0"},
		{value: float32(25), wantFindings: "50", wantInventory: "100", wantOffset: "0"},
	} {
		findings, _ := Route("list_supply_chain_impact_findings", routecontract.Arguments{"limit": tt.value})
		if got := findings.Query["limit"]; got != tt.wantFindings {
			t.Errorf("findings limit=%#v -> %q, want %q", tt.value, got, tt.wantFindings)
		}
		inventory, _ := Route("get_supply_chain_impact_inventory", routecontract.Arguments{"limit": tt.value})
		if got := inventory.Query["limit"]; got != tt.wantInventory {
			t.Errorf("inventory limit=%#v -> %q, want %q", tt.value, got, tt.wantInventory)
		}
		offsetRequest, _ := Route("get_supply_chain_impact_inventory", routecontract.Arguments{"offset": tt.value})
		if got := offsetRequest.Query["offset"]; got != tt.wantOffset {
			t.Errorf("inventory offset=%#v -> %q, want %q", tt.value, got, tt.wantOffset)
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every string key of every tool.
	for _, value := range []any{42, nil, true, []string{"CVE-2026-0001"}, struct{}{}, []byte("CVE-2026-0001")} {
		for _, toolName := range familyTools {
			for _, key := range queryKeys[toolName] {
				if key == "limit" || key == "offset" || key == "group_by" || key == "min_priority_score" || key == "include_suppressed" {
					continue
				}
				request, _ := Route(toolName, routecontract.Arguments{key: value})
				if got := request.Query[key]; got != "" {
					t.Errorf("Route(%s) %s=%#v -> %q, want empty", toolName, key, value, got)
				}
			}
		}
	}
}

// TestRouteFallsBackToImpactStatusGroupBy pins the inventory dimension
// default. The dispatcher has always sent group_by=impact_status when the
// caller omitted it; query.impactInventory independently applies the same
// default on an empty value and rejects anything outside impact_status,
// priority_bucket, severity, repository_id, and ecosystem with a 400. So the
// fallback is not what makes an omitted group_by work -- it is what keeps the
// selected wire value stable, and changing it to another dimension would
// change the answer.
func TestRouteFallsBackToImpactStatusGroupBy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "absent", value: nil, want: "impact_status"},
		{name: "empty string", value: "", want: "impact_status"},
		{name: "wrong type int", value: 42, want: "impact_status"},
		{name: "wrong type bool", value: true, want: "impact_status"},
		{name: "wrong type slice", value: []string{"impact_status"}, want: "impact_status"},
		{name: "explicit impact_status", value: "impact_status", want: "impact_status"},
		{name: "priority bucket", value: "priority_bucket", want: "priority_bucket"},
		{name: "severity", value: "severity", want: "severity"},
		{name: "repository id", value: "repository_id", want: "repository_id"},
		{name: "ecosystem", value: "ecosystem", want: "ecosystem"},
		// An unsupported dimension is forwarded verbatim so the handler can
		// answer with its own 400 instead of the route silently correcting a
		// caller typo into a different grouping.
		{name: "unsupported dimension", value: "subject_digest", want: "subject_digest"},
	} {
		args := routecontract.Arguments{}
		if tt.value != nil {
			args["group_by"] = tt.value
		}
		request, handled := Route("get_supply_chain_impact_inventory", args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if got := request.Query["group_by"]; got != tt.want {
			t.Errorf("%s: group_by = %q, want %q", tt.name, got, tt.want)
		}
	}

	// group_by belongs to the inventory alone; the count route groups nothing.
	request, _ := Route("count_supply_chain_impact_findings", routecontract.Arguments{"group_by": "priority_bucket"})
	if value, present := request.Query["group_by"]; present {
		t.Errorf("count route carries group_by = %q, want the key absent", value)
	}
}

// TestRouteEncodesIncludeSuppressedOnlyWhenSet pins the three-state
// include_suppressed contract: absent when the caller never set it (so the
// handler's documented false default applies), "true" or "false" when the
// caller set an explicit bool, and absent again for a wrong-typed value
// rather than a formatted Go value.
func TestRouteEncodesIncludeSuppressedOnlyWhenSet(t *testing.T) {
	t.Parallel()

	for _, toolName := range []string{
		"list_supply_chain_impact_findings",
		"count_supply_chain_impact_findings",
		"get_supply_chain_impact_inventory",
	} {
		for _, tt := range []struct {
			name  string
			value any
			want  string
		}{
			{name: "absent", value: nil, want: ""},
			{name: "true", value: true, want: "true"},
			{name: "false", value: false, want: "false"},
			{name: "wrong type string", value: "true", want: ""},
			{name: "wrong type int", value: 1, want: ""},
		} {
			args := routecontract.Arguments{}
			if tt.value != nil {
				args["include_suppressed"] = tt.value
			}
			request, handled := Route(toolName, args)
			if !handled {
				t.Fatalf("%s/%s: handled = false, want true", toolName, tt.name)
			}
			got, present := request.Query["include_suppressed"]
			if tt.want == "" {
				if present {
					t.Errorf("%s/%s: include_suppressed = %q, want absent", toolName, tt.name, got)
				}
				continue
			}
			if !present || got != tt.want {
				t.Errorf("%s/%s: include_suppressed = %q, present=%v, want %q", toolName, tt.name, got, present, tt.want)
			}
		}
	}

	// explain_supply_chain_impact does not carry include_suppressed at all.
	request, handled := Route("explain_supply_chain_impact", routecontract.Arguments{"include_suppressed": true})
	if !handled {
		t.Fatal("Route(explain_supply_chain_impact) handled = false, want true")
	}
	if value, present := request.Query["include_suppressed"]; present {
		t.Errorf("explanation route carries include_suppressed = %q, want the key absent", value)
	}
}

func TestRouteHandlesNilAndTypedNilSupplyChainImpactArguments(t *testing.T) {
	t.Parallel()

	want := map[string]routecontract.Request{
		"list_supply_chain_impact_findings": {Method: "GET", Path: "/api/v0/supply-chain/impact/findings", Query: map[string]string{
			"advisory_id":        "",
			"after_finding_id":   "",
			"cve_id":             "",
			"ecosystem":          "",
			"environment":        "",
			"ghsa_id":            "",
			"image_ref":          "",
			"impact_status":      "",
			"limit":              "50",
			"min_priority_score": "0",
			"osv_id":             "",
			"package_id":         "",
			"priority_bucket":    "",
			"profile":            "",
			"repository_id":      "",
			"service_id":         "",
			"severity":           "",
			"sort":               "",
			"subject_digest":     "",
			"suppression_state":  "",
			"workload_id":        "",
		}},
		"count_supply_chain_impact_findings": {Method: "GET", Path: "/api/v0/supply-chain/impact/findings/count", Query: map[string]string{
			"advisory_id":        "",
			"cve_id":             "",
			"ecosystem":          "",
			"environment":        "",
			"ghsa_id":            "",
			"image_ref":          "",
			"impact_status":      "",
			"min_priority_score": "0",
			"osv_id":             "",
			"package_id":         "",
			"priority_bucket":    "",
			"profile":            "",
			"repository_id":      "",
			"service_id":         "",
			"severity":           "",
			"subject_digest":     "",
			"suppression_state":  "",
			"workload_id":        "",
		}},
		"get_supply_chain_impact_inventory": {Method: "GET", Path: "/api/v0/supply-chain/impact/inventory", Query: map[string]string{
			"advisory_id":        "",
			"cve_id":             "",
			"ecosystem":          "",
			"environment":        "",
			"ghsa_id":            "",
			"group_by":           "impact_status",
			"image_ref":          "",
			"impact_status":      "",
			"limit":              "100",
			"min_priority_score": "0",
			"offset":             "0",
			"osv_id":             "",
			"package_id":         "",
			"priority_bucket":    "",
			"profile":            "",
			"repository_id":      "",
			"service_id":         "",
			"severity":           "",
			"subject_digest":     "",
			"suppression_state":  "",
			"workload_id":        "",
		}},
		"explain_supply_chain_impact": {Method: "GET", Path: "/api/v0/supply-chain/impact/explain", Query: map[string]string{
			"advisory_id":    "",
			"cve_id":         "",
			"finding_id":     "",
			"image_ref":      "",
			"package_id":     "",
			"repository_id":  "",
			"service_id":     "",
			"subject_digest": "",
			"workload_id":    "",
		}},
	}

	var typedNil map[string]any
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	} {
		for _, toolName := range familyTools {
			request, handled := Route(toolName, tt.args)
			if !handled {
				t.Fatalf("%s: Route(%s) handled = false, want true", tt.name, toolName)
			}
			if !reflect.DeepEqual(request, want[toolName]) {
				t.Fatalf("%s: Route(%s) = %#v, want %#v", tt.name, toolName, request, want[toolName])
			}
		}
	}
}

func TestRouteDoesNotAliasCallerSupplyChainImpactArguments(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		args := routecontract.Arguments{
			"repository_id": "repo://example/api",
			"cve_id":        "CVE-2026-0001",
			"limit":         float64(25),
		}
		request, handled := Route(toolName, args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		request.Query["repository_id"] = "mutated"
		if got := args["repository_id"]; got != "repo://example/api" {
			t.Fatalf("Route(%s) mutated caller arguments through the returned query: repository_id = %#v", toolName, got)
		}
		if len(args) != 3 {
			t.Fatalf("Route(%s) grew caller arguments to %d keys, want 3", toolName, len(args))
		}

		// Two calls with the same arguments hand back independent query maps.
		first, _ := Route(toolName, args)
		second, _ := Route(toolName, args)
		first.Query["repository_id"] = "mutated"
		if got := second.Query["repository_id"]; got != "repo://example/api" {
			t.Fatalf("Route(%s) shares a query map between calls: repository_id = %q", toolName, got)
		}
	}
}
