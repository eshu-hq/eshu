// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimagetools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyTools is the exact set of names this package owns.
var familyTools = []string{
	"list_container_image_identities",
	"list_container_image_tag_history",
	"count_container_image_identities",
	"get_container_image_identity_inventory",
}

// queryKeys pins the exact key set each of the four requests sends. The
// listing and the two aggregates share five filters, but their paths, their
// paging keys, and their limit defaults differ, so each set is spelled out
// rather than derived from a shared base.
var queryKeys = map[string][]string{
	"list_container_image_identities": {
		"after_identity_id",
		"digest",
		"image_ref",
		"limit",
		"outcome",
		"repository_id",
		"source_repository_id",
	},
	"list_container_image_tag_history": {
		"limit",
		"offset",
		"repository_id",
		"tag",
	},
	"count_container_image_identities": {
		"digest",
		"image_ref",
		"outcome",
		"repository_id",
		"source_repository_id",
	},
	"get_container_image_identity_inventory": {
		"digest",
		"group_by",
		"image_ref",
		"limit",
		"offset",
		"outcome",
		"repository_id",
		"source_repository_id",
	},
}

// wantPaths pins each tool's path. Tag history is the odd one: it is served
// from /api/v0/images/ rather than the /api/v0/supply-chain/container-images/
// prefix the other three share, so "normalizing" it to the sibling prefix
// would route to a path no handler mounts.
var wantPaths = map[string]string{
	"list_container_image_identities":        "/api/v0/supply-chain/container-images/identities",
	"list_container_image_tag_history":       "/api/v0/images/tag-history",
	"count_container_image_identities":       "/api/v0/supply-chain/container-images/identities/count",
	"get_container_image_identity_inventory": "/api/v0/supply-chain/container-images/identities/inventory",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// a request builder fail the exact comparison below instead of passing on a
// shared value. unused_decoy must never reach a query.
var populatedArguments = routecontract.Arguments{
	"after_identity_id":    "container-image-identity-1",
	"digest":               "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
	"group_by":             "identity_strength",
	"image_ref":            "ghcr.io/eshu-hq/api:1.2.3",
	"limit":                float64(25),
	"offset":               float64(7),
	"outcome":              "exact_digest",
	"repository_id":        "oci-registry://ghcr.io/eshu-hq/api",
	"source_repository_id": "github.com/eshu-hq/eshu",
	"tag":                  "1.2.3",
	"unused_decoy":         "ignored",
}

// wantPopulatedRequests is the request each tool must select from
// populatedArguments.
var wantPopulatedRequests = map[string]routecontract.Request{
	"list_container_image_identities": {Method: "GET", Path: "/api/v0/supply-chain/container-images/identities", Query: map[string]string{
		"after_identity_id":    "container-image-identity-1",
		"digest":               "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"image_ref":            "ghcr.io/eshu-hq/api:1.2.3",
		"limit":                "25",
		"outcome":              "exact_digest",
		"repository_id":        "oci-registry://ghcr.io/eshu-hq/api",
		"source_repository_id": "github.com/eshu-hq/eshu",
	}},
	"list_container_image_tag_history": {Method: "GET", Path: "/api/v0/images/tag-history", Query: map[string]string{
		"limit":         "25",
		"offset":        "7",
		"repository_id": "oci-registry://ghcr.io/eshu-hq/api",
		"tag":           "1.2.3",
	}},
	"count_container_image_identities": {Method: "GET", Path: "/api/v0/supply-chain/container-images/identities/count", Query: map[string]string{
		"digest":               "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"image_ref":            "ghcr.io/eshu-hq/api:1.2.3",
		"outcome":              "exact_digest",
		"repository_id":        "oci-registry://ghcr.io/eshu-hq/api",
		"source_repository_id": "github.com/eshu-hq/eshu",
	}},
	"get_container_image_identity_inventory": {Method: "GET", Path: "/api/v0/supply-chain/container-images/identities/inventory", Query: map[string]string{
		"digest":               "sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00112233445566778899aabbccddeeff0",
		"group_by":             "identity_strength",
		"image_ref":            "ghcr.io/eshu-hq/api:1.2.3",
		"limit":                "25",
		"offset":               "7",
		"outcome":              "exact_digest",
		"repository_id":        "oci-registry://ghcr.io/eshu-hq/api",
		"source_repository_id": "github.com/eshu-hq/eshu",
	}},
}

func TestRouteOwnsExactlyTheContainerImageFamily(t *testing.T) {
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
	// The supply-chain siblings matter most -- two of this family's builders
	// came out of dispatch_supply_chain.go, and the six that stayed there are
	// still answered by the switch.
	for _, toolName := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"get_package_registry_package_inventory",
		"list_ci_cd_run_correlations",
		"list_codeowners_ownership",
		"list_secrets_iam_posture_gaps",
		"list_observability_coverage_correlations",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_advisory_evidence",
		"get_vulnerability_scanner_read_contract",
		"list_supply_chain_impact_findings",
		"count_supply_chain_impact_findings",
		"get_supply_chain_impact_inventory",
		"explain_supply_chain_impact",
		"list_security_alert_reconciliations",
		"list_sbom_attestation_attachments",
		"get_repository_stats",
		"list_container_image_identity",
		"list_container_image_identities_extra",
		"list_container_image_identities ",
		"count_container_image_identity",
		"count_container_image_tag_history",
		"get_container_image_identity_inventories",
		"get_container_image_tag_history",
		"list_container_image_tag_histories",
		"container_image_identities",
		"LIST_CONTAINER_IMAGE_IDENTITIES",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesContainerImageRequestContract(t *testing.T) {
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

// TestRouteKeepsTagHistoryOffTheSupplyChainPrefix pins the one path asymmetry
// in this family. Three tools sit under
// /api/v0/supply-chain/container-images/identities; tag history is mounted at
// GET /api/v0/images/tag-history by TagHistoryHandler.Mount. Folding it into
// the sibling prefix for tidiness would select a path nothing serves.
func TestRouteKeepsTagHistoryOffTheSupplyChainPrefix(t *testing.T) {
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

	request, _ := Route("list_container_image_tag_history", routecontract.Arguments{})
	if request.Path != "/api/v0/images/tag-history" {
		t.Fatalf("tag history path = %q, want /api/v0/images/tag-history", request.Path)
	}
	for _, sibling := range []string{
		"/api/v0/supply-chain/container-images/identities",
		"/api/v0/supply-chain/container-images/tag-history",
		"/api/v0/supply-chain/images/tag-history",
	} {
		if request.Path == sibling {
			t.Fatalf("tag history path was normalized onto %q", sibling)
		}
	}
}

// TestRouteCarriesEveryContainerImageQueryKey pins each key on its own. The
// exact-request comparison already covers the set, but a per-key assertion
// names the dropped key, and the handlers read these by name: the identity
// listing 400s without limit or a scope anchor, tag history 400s without
// repository_id and tag, and a dropped aggregate filter silently widens the
// count or the inventory bucket the caller asked for.
func TestRouteCarriesEveryContainerImageQueryKey(t *testing.T) {
	t.Parallel()

	// Keys that belong to a sibling in this family, not to the tool under
	// test. Leaking one across would send a filter the handler ignores or,
	// worse, a paging key the route does not support.
	foreignKeys := map[string][]string{
		"list_container_image_identities":        {"offset", "group_by", "tag"},
		"list_container_image_tag_history":       {"digest", "image_ref", "outcome", "group_by", "source_repository_id", "after_identity_id"},
		"count_container_image_identities":       {"limit", "offset", "group_by", "tag", "after_identity_id"},
		"get_container_image_identity_inventory": {"tag", "after_identity_id"},
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

// TestRouteAppliesContainerImageLimitAndOffsetDefaults pins the three paging
// defaults. They are not uniform: the identity listing and tag history default
// limit to 50, the inventory to 100, and only tag history and the inventory
// carry an offset, which defaults to 0. The count route has neither key.
func TestRouteAppliesContainerImageLimitAndOffsetDefaults(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		tool       string
		wantLimit  string
		wantOffset string
	}{
		{tool: "list_container_image_identities", wantLimit: "50"},
		{tool: "list_container_image_tag_history", wantLimit: "50", wantOffset: "0"},
		{tool: "get_container_image_identity_inventory", wantLimit: "100", wantOffset: "0"},
	} {
		request, handled := Route(tt.tool, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.tool)
		}
		if got := request.Query["limit"]; got != tt.wantLimit {
			t.Errorf("Route(%s) absent limit -> %q, want %q", tt.tool, got, tt.wantLimit)
		}
		if tt.wantOffset == "" {
			continue
		}
		if got := request.Query["offset"]; got != tt.wantOffset {
			t.Errorf("Route(%s) absent offset -> %q, want %q", tt.tool, got, tt.wantOffset)
		}
	}

	// The count route is a plain scope-anchored aggregate: no paging at all.
	request, _ := Route("count_container_image_identities", routecontract.Arguments{"limit": 25, "offset": 5})
	for _, key := range []string{"limit", "offset"} {
		if value, present := request.Query[key]; present {
			t.Errorf("count route carries %q = %q, want no paging key", key, value)
		}
	}
}

func TestRouteAppliesContainerImageNumericCoercions(t *testing.T) {
	t.Parallel()

	// Numeric coercion follows routecontract.Arguments.IntOr exactly: int,
	// int64, and float64 are accepted, a float64 truncates toward zero, and
	// every other type falls back to the default.
	for _, tt := range []struct {
		value          any
		wantIdentities string
		wantInventory  string
		wantOffset     string
	}{
		{value: 25, wantIdentities: "25", wantInventory: "25", wantOffset: "25"},
		{value: int64(26), wantIdentities: "26", wantInventory: "26", wantOffset: "26"},
		{value: 27.9, wantIdentities: "27", wantInventory: "27", wantOffset: "27"},
		{value: -3.9, wantIdentities: "-3", wantInventory: "-3", wantOffset: "-3"},
		{value: -7, wantIdentities: "-7", wantInventory: "-7", wantOffset: "-7"},
		{value: 0, wantIdentities: "0", wantInventory: "0", wantOffset: "0"},
		{value: "25", wantIdentities: "50", wantInventory: "100", wantOffset: "0"},
		{value: true, wantIdentities: "50", wantInventory: "100", wantOffset: "0"},
		{value: nil, wantIdentities: "50", wantInventory: "100", wantOffset: "0"},
		{value: float32(25), wantIdentities: "50", wantInventory: "100", wantOffset: "0"},
	} {
		identities, _ := Route("list_container_image_identities", routecontract.Arguments{"limit": tt.value})
		if got := identities.Query["limit"]; got != tt.wantIdentities {
			t.Errorf("identities limit=%#v -> %q, want %q", tt.value, got, tt.wantIdentities)
		}
		tagHistory, _ := Route("list_container_image_tag_history", routecontract.Arguments{"limit": tt.value})
		if got := tagHistory.Query["limit"]; got != tt.wantIdentities {
			t.Errorf("tag history limit=%#v -> %q, want %q", tt.value, got, tt.wantIdentities)
		}
		inventory, _ := Route("get_container_image_identity_inventory", routecontract.Arguments{"limit": tt.value})
		if got := inventory.Query["limit"]; got != tt.wantInventory {
			t.Errorf("inventory limit=%#v -> %q, want %q", tt.value, got, tt.wantInventory)
		}
		for _, tool := range []string{"list_container_image_tag_history", "get_container_image_identity_inventory"} {
			request, _ := Route(tool, routecontract.Arguments{"offset": tt.value})
			if got := request.Query["offset"]; got != tt.wantOffset {
				t.Errorf("%s offset=%#v -> %q, want %q", tool, tt.value, got, tt.wantOffset)
			}
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every string key of every tool.
	for _, value := range []any{42, nil, true, []string{"ghcr.io"}, struct{}{}, []byte("sha256:abc")} {
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

// TestRouteFallsBackToOutcomeGroupBy pins the inventory dimension default.
// The dispatcher has always sent group_by=outcome when the caller omitted it;
// query.containerImageIdentityInventory independently applies the same default
// on an empty value and rejects anything outside outcome, identity_strength,
// and repository_id with a 400. So the fallback is not what makes an omitted
// group_by work -- it is what keeps the selected wire value stable, and
// changing it to another dimension would change the answer.
func TestRouteFallsBackToOutcomeGroupBy(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{name: "absent", value: nil, want: "outcome"},
		{name: "empty string", value: "", want: "outcome"},
		{name: "wrong type int", value: 42, want: "outcome"},
		{name: "wrong type bool", value: true, want: "outcome"},
		{name: "wrong type slice", value: []string{"outcome"}, want: "outcome"},
		{name: "explicit outcome", value: "outcome", want: "outcome"},
		{name: "identity strength", value: "identity_strength", want: "identity_strength"},
		{name: "repository id", value: "repository_id", want: "repository_id"},
		// An unsupported dimension is forwarded verbatim so the handler can
		// answer with its own 400 instead of the route silently correcting a
		// caller typo into a different grouping.
		{name: "unsupported dimension", value: "digest", want: "digest"},
	} {
		args := routecontract.Arguments{}
		if tt.value != nil {
			args["group_by"] = tt.value
		}
		request, handled := Route("get_container_image_identity_inventory", args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if got := request.Query["group_by"]; got != tt.want {
			t.Errorf("%s: group_by = %q, want %q", tt.name, got, tt.want)
		}
	}

	// group_by belongs to the inventory alone; the count route groups nothing.
	request, _ := Route("count_container_image_identities", routecontract.Arguments{"group_by": "identity_strength"})
	if value, present := request.Query["group_by"]; present {
		t.Errorf("count route carries group_by = %q, want the key absent", value)
	}
}

func TestRouteHandlesNilAndTypedNilContainerImageArguments(t *testing.T) {
	t.Parallel()

	want := map[string]routecontract.Request{
		"list_container_image_identities": {Method: "GET", Path: "/api/v0/supply-chain/container-images/identities", Query: map[string]string{
			"after_identity_id":    "",
			"digest":               "",
			"image_ref":            "",
			"limit":                "50",
			"outcome":              "",
			"repository_id":        "",
			"source_repository_id": "",
		}},
		"list_container_image_tag_history": {Method: "GET", Path: "/api/v0/images/tag-history", Query: map[string]string{
			"limit":         "50",
			"offset":        "0",
			"repository_id": "",
			"tag":           "",
		}},
		"count_container_image_identities": {Method: "GET", Path: "/api/v0/supply-chain/container-images/identities/count", Query: map[string]string{
			"digest":               "",
			"image_ref":            "",
			"outcome":              "",
			"repository_id":        "",
			"source_repository_id": "",
		}},
		"get_container_image_identity_inventory": {Method: "GET", Path: "/api/v0/supply-chain/container-images/identities/inventory", Query: map[string]string{
			"digest":               "",
			"group_by":             "outcome",
			"image_ref":            "",
			"limit":                "100",
			"offset":               "0",
			"outcome":              "",
			"repository_id":        "",
			"source_repository_id": "",
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

func TestRouteDoesNotAliasCallerContainerImageArguments(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		args := routecontract.Arguments{
			"repository_id": "oci-registry://ghcr.io/eshu-hq/api",
			"tag":           "1.2.3",
			"limit":         float64(25),
		}
		request, handled := Route(toolName, args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		request.Query["repository_id"] = "mutated"
		if got := args["repository_id"]; got != "oci-registry://ghcr.io/eshu-hq/api" {
			t.Fatalf("Route(%s) mutated caller arguments through the returned query: repository_id = %#v", toolName, got)
		}
		if len(args) != 3 {
			t.Fatalf("Route(%s) grew caller arguments to %d keys, want 3", toolName, len(args))
		}

		// Two calls with the same arguments hand back independent query maps.
		first, _ := Route(toolName, args)
		second, _ := Route(toolName, args)
		first.Query["repository_id"] = "mutated"
		if got := second.Query["repository_id"]; got != "oci-registry://ghcr.io/eshu-hq/api" {
			t.Fatalf("Route(%s) shares a query map between calls: repository_id = %q", toolName, got)
		}
	}
}
