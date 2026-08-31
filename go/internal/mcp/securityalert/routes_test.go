// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyTools is the exact set of names this package owns.
var familyTools = []string{
	"list_security_alert_reconciliations",
	"count_security_alert_reconciliations",
	"get_security_alert_reconciliation_inventory",
}

// queryKeys pins the exact key set each of the three requests sends. The
// count and the inventory share seven filters, but their paths and their
// paging keys differ, and the listing carries its own after_reconciliation_id
// cursor and limit, so each set is spelled out rather than derived from a
// shared base.
var queryKeys = map[string][]string{
	"list_security_alert_reconciliations": {
		"after_reconciliation_id",
		"cve_id",
		"ghsa_id",
		"limit",
		"package_id",
		"provider",
		"provider_state",
		"reconciliation_status",
		"repository_id",
	},
	"count_security_alert_reconciliations": {
		"cve_id",
		"ghsa_id",
		"package_id",
		"provider",
		"provider_state",
		"reconciliation_status",
		"repository_id",
	},
	"get_security_alert_reconciliation_inventory": {
		"cve_id",
		"ghsa_id",
		"group_by",
		"limit",
		"offset",
		"package_id",
		"provider",
		"provider_state",
		"reconciliation_status",
		"repository_id",
	},
}

// wantPaths pins each tool's path.
var wantPaths = map[string]string{
	"list_security_alert_reconciliations":         "/api/v0/supply-chain/security-alerts/reconciliations",
	"count_security_alert_reconciliations":        "/api/v0/supply-chain/security-alerts/reconciliations/count",
	"get_security_alert_reconciliation_inventory": "/api/v0/supply-chain/security-alerts/reconciliations/inventory",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// a request builder fail the exact comparison below instead of passing on a
// shared value. unused_decoy must never reach a query.
var populatedArguments = routecontract.Arguments{
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
	"unused_decoy":            "ignored",
}

// wantPopulatedRequests is the request each tool must select from
// populatedArguments.
var wantPopulatedRequests = map[string]routecontract.Request{
	"list_security_alert_reconciliations": {Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations", Query: map[string]string{
		"after_reconciliation_id": "reconciliation-1",
		"cve_id":                  "CVE-2026-0001",
		"ghsa_id":                 "GHSA-aaaa-bbbb-cccc",
		"limit":                   "25",
		"package_id":              "npm://registry.npmjs.org/left-pad",
		"provider":                "github_dependabot",
		"provider_state":          "open",
		"reconciliation_status":   "matched",
		"repository_id":           "repo://github/eshu-hq/eshu",
	}},
	"count_security_alert_reconciliations": {Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations/count", Query: map[string]string{
		"cve_id":                "CVE-2026-0001",
		"ghsa_id":               "GHSA-aaaa-bbbb-cccc",
		"package_id":            "npm://registry.npmjs.org/left-pad",
		"provider":              "github_dependabot",
		"provider_state":        "open",
		"reconciliation_status": "matched",
		"repository_id":         "repo://github/eshu-hq/eshu",
	}},
	"get_security_alert_reconciliation_inventory": {Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations/inventory", Query: map[string]string{
		"cve_id":                "CVE-2026-0001",
		"ghsa_id":               "GHSA-aaaa-bbbb-cccc",
		"group_by":              "provider",
		"limit":                 "25",
		"offset":                "10",
		"package_id":            "npm://registry.npmjs.org/left-pad",
		"provider":              "github_dependabot",
		"provider_state":        "open",
		"reconciliation_status": "matched",
		"repository_id":         "repo://github/eshu-hq/eshu",
	}},
}

func TestRouteOwnsExactlyTheSecurityAlertFamily(t *testing.T) {
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
		"list_security_alert_reconciliation",
		"list_security_alert_reconciliations_extra",
		"list_security_alert_reconciliations ",
		"count_security_alert_reconciliation",
		"get_security_alert_reconciliation_inventories",
		"security_alert_reconciliations",
		"LIST_SECURITY_ALERT_RECONCILIATIONS",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesSecurityAlertRequestContract(t *testing.T) {
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

func TestRouteUsesTheSharedSecurityAlertPathPrefix(t *testing.T) {
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

// TestRouteCarriesEverySecurityAlertQueryKey pins each key on its own. The
// exact-request comparison already covers the set, but a per-key assertion
// names the dropped key: the listing 400s without limit or a scope anchor,
// and a dropped aggregate filter silently widens the count or the inventory
// bucket the caller asked for.
func TestRouteCarriesEverySecurityAlertQueryKey(t *testing.T) {
	t.Parallel()

	// Keys that belong to a sibling in this family, not to the tool under
	// test. Leaking one across would send a filter the handler ignores or,
	// worse, a paging key the route does not support.
	foreignKeys := map[string][]string{
		"list_security_alert_reconciliations":         {"group_by", "offset"},
		"count_security_alert_reconciliations":        {"after_reconciliation_id", "group_by", "offset", "limit"},
		"get_security_alert_reconciliation_inventory": {"after_reconciliation_id"},
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

// TestRouteAppliesSecurityAlertLimitAndOffsetDefaults pins the two paging
// defaults. The listing defaults limit to 50; the inventory defaults limit to
// 100 and offset to 0. The count carries neither key.
func TestRouteAppliesSecurityAlertLimitAndOffsetDefaults(t *testing.T) {
	t.Parallel()

	listing, handled := Route("list_security_alert_reconciliations", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_security_alert_reconciliations) handled = false, want true")
	}
	if got, want := listing.Query["limit"], "50"; got != want {
		t.Errorf("listing absent limit -> %q, want %q", got, want)
	}

	inventory, handled := Route("get_security_alert_reconciliation_inventory", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(get_security_alert_reconciliation_inventory) handled = false, want true")
	}
	if got, want := inventory.Query["limit"], "100"; got != want {
		t.Errorf("inventory absent limit -> %q, want %q", got, want)
	}
	if got, want := inventory.Query["offset"], "0"; got != want {
		t.Errorf("inventory absent offset -> %q, want %q", got, want)
	}

	count, handled := Route("count_security_alert_reconciliations", routecontract.Arguments{"limit": 25, "offset": 5})
	if !handled {
		t.Fatal("Route(count_security_alert_reconciliations) handled = false, want true")
	}
	for _, key := range []string{"limit", "offset"} {
		if value, present := count.Query[key]; present {
			t.Errorf("count route carries %q = %q, want no paging key", key, value)
		}
	}
}

func TestRouteAppliesSecurityAlertNumericCoercions(t *testing.T) {
	t.Parallel()

	// Numeric coercion follows routecontract.Arguments.IntOr exactly: int,
	// int64, and float64 are accepted, a float64 truncates toward zero, and
	// every other type falls back to the default.
	for _, tt := range []struct {
		value         any
		wantListing   string
		wantInventory string
		wantOffset    string
	}{
		{value: 25, wantListing: "25", wantInventory: "25", wantOffset: "25"},
		{value: int64(26), wantListing: "26", wantInventory: "26", wantOffset: "26"},
		{value: 27.9, wantListing: "27", wantInventory: "27", wantOffset: "27"},
		{value: -3.9, wantListing: "-3", wantInventory: "-3", wantOffset: "-3"},
		{value: -7, wantListing: "-7", wantInventory: "-7", wantOffset: "-7"},
		{value: 0, wantListing: "0", wantInventory: "0", wantOffset: "0"},
		{value: "25", wantListing: "50", wantInventory: "100", wantOffset: "0"},
		{value: true, wantListing: "50", wantInventory: "100", wantOffset: "0"},
		{value: nil, wantListing: "50", wantInventory: "100", wantOffset: "0"},
		{value: float32(25), wantListing: "50", wantInventory: "100", wantOffset: "0"},
	} {
		listing, _ := Route("list_security_alert_reconciliations", routecontract.Arguments{"limit": tt.value})
		if got := listing.Query["limit"]; got != tt.wantListing {
			t.Errorf("listing limit=%#v -> %q, want %q", tt.value, got, tt.wantListing)
		}
		inventory, _ := Route("get_security_alert_reconciliation_inventory", routecontract.Arguments{"limit": tt.value})
		if got := inventory.Query["limit"]; got != tt.wantInventory {
			t.Errorf("inventory limit=%#v -> %q, want %q", tt.value, got, tt.wantInventory)
		}
		offsetRequest, _ := Route("get_security_alert_reconciliation_inventory", routecontract.Arguments{"offset": tt.value})
		if got := offsetRequest.Query["offset"]; got != tt.wantOffset {
			t.Errorf("inventory offset=%#v -> %q, want %q", tt.value, got, tt.wantOffset)
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every string key of every tool.
	for _, value := range []any{42, nil, true, []string{"CVE-2026-0001"}, struct{}{}, []byte("CVE-2026-0001")} {
		for _, toolName := range familyTools {
			for _, key := range queryKeys[toolName] {
				if key == "limit" || key == "offset" || key == "group_by" {
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

// TestRouteFallsBackToReconciliationStatusGroupBy pins the inventory
// dimension default. The dispatcher has always sent
// group_by=reconciliation_status when the caller omitted it; the handler
// independently applies the same default on an empty value, so the fallback
// is not what makes an omitted group_by work -- it is what keeps the
// selected wire value stable.
func TestRouteFallsBackToReconciliationStatusGroupBy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "absent", value: nil, want: "reconciliation_status"},
		{name: "empty string", value: "", want: "reconciliation_status"},
		{name: "wrong type int", value: 42, want: "reconciliation_status"},
		{name: "wrong type bool", value: true, want: "reconciliation_status"},
		{name: "wrong type slice", value: []string{"reconciliation_status"}, want: "reconciliation_status"},
		{name: "explicit reconciliation_status", value: "reconciliation_status", want: "reconciliation_status"},
		{name: "provider", value: "provider", want: "provider"},
		{name: "provider_state", value: "provider_state", want: "provider_state"},
		{name: "repository_id", value: "repository_id", want: "repository_id"},
		{name: "package_id", value: "package_id", want: "package_id"},
		// An unsupported dimension is forwarded verbatim so the handler can
		// answer with its own 400 instead of the route silently correcting a
		// caller typo into a different grouping.
		{name: "unsupported dimension", value: "cve_id", want: "cve_id"},
	} {
		args := routecontract.Arguments{}
		if tt.value != nil {
			args["group_by"] = tt.value
		}
		request, handled := Route("get_security_alert_reconciliation_inventory", args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if got := request.Query["group_by"]; got != tt.want {
			t.Errorf("%s: group_by = %q, want %q", tt.name, got, tt.want)
		}
	}

	// group_by belongs to the inventory alone; the count route groups
	// nothing.
	request, _ := Route("count_security_alert_reconciliations", routecontract.Arguments{"group_by": "provider"})
	if value, present := request.Query["group_by"]; present {
		t.Errorf("count route carries group_by = %q, want the key absent", value)
	}
}

func TestRouteHandlesNilAndTypedNilSecurityAlertArguments(t *testing.T) {
	t.Parallel()

	want := map[string]routecontract.Request{
		"list_security_alert_reconciliations": {Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations", Query: map[string]string{
			"after_reconciliation_id": "",
			"cve_id":                  "",
			"ghsa_id":                 "",
			"limit":                   "50",
			"package_id":              "",
			"provider":                "",
			"provider_state":          "",
			"reconciliation_status":   "",
			"repository_id":           "",
		}},
		"count_security_alert_reconciliations": {Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations/count", Query: map[string]string{
			"cve_id":                "",
			"ghsa_id":               "",
			"package_id":            "",
			"provider":              "",
			"provider_state":        "",
			"reconciliation_status": "",
			"repository_id":         "",
		}},
		"get_security_alert_reconciliation_inventory": {Method: "GET", Path: "/api/v0/supply-chain/security-alerts/reconciliations/inventory", Query: map[string]string{
			"cve_id":                "",
			"ghsa_id":               "",
			"group_by":              "reconciliation_status",
			"limit":                 "100",
			"offset":                "0",
			"package_id":            "",
			"provider":              "",
			"provider_state":        "",
			"reconciliation_status": "",
			"repository_id":         "",
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

func TestRouteDoesNotAliasCallerSecurityAlertArguments(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		args := routecontract.Arguments{
			"repository_id": "repo://github/eshu-hq/eshu",
			"cve_id":        "CVE-2026-0001",
			"limit":         float64(25),
		}
		request, handled := Route(toolName, args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		request.Query["repository_id"] = "mutated"
		if got := args["repository_id"]; got != "repo://github/eshu-hq/eshu" {
			t.Fatalf("Route(%s) mutated caller arguments through the returned query: repository_id = %#v", toolName, got)
		}
		if len(args) != 3 {
			t.Fatalf("Route(%s) grew caller arguments to %d keys, want 3", toolName, len(args))
		}

		// Two calls with the same arguments hand back independent query maps.
		first, _ := Route(toolName, args)
		second, _ := Route(toolName, args)
		first.Query["repository_id"] = "mutated"
		if got := second.Query["repository_id"]; got != "repo://github/eshu-hq/eshu" {
			t.Fatalf("Route(%s) shares a query map between calls: repository_id = %q", toolName, got)
		}
	}
}
