// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package infrasearchtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyTools is the one name this package owns.
var familyTools = []string{"find_infra_resources"}

// searchBodyKeys is the exact eight-key body the search sends. The handler
// requires at least one of the seven scope keys, so the count and the
// spelling of each key are pinned here rather than left to the request
// comparison alone.
var searchBodyKeys = []string{
	"query",
	"category",
	"kind",
	"provider",
	"environment",
	"resource_service",
	"resource_category",
	"limit",
}

// scopeBodyKeys are the seven keys of which the handler requires at least
// one non-blank value; a request carrying none of them is rejected with 400.
var scopeBodyKeys = []string{
	"query",
	"category",
	"kind",
	"provider",
	"environment",
	"resource_service",
	"resource_category",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// the request builder fail the exact comparison below instead of passing on a
// shared value.
var populatedArguments = routecontract.Arguments{
	"query":             "checkout-bucket",
	"category":          "cloud",
	"kind":              "aws_s3_bucket",
	"provider":          "aws",
	"environment":       "prod",
	"resource_service":  "s3",
	"resource_category": "storage",
	"limit":             float64(25),
	"unused_decoy":      "ignored",
}

// wantPopulatedRequest is the request the eight populated keys must select.
// limit travels as a Go int inside the JSON body, not as a string.
var wantPopulatedRequest = routecontract.Request{Method: "POST", Path: "/api/v0/infra/resources/search", Body: map[string]any{
	"query":             "checkout-bucket",
	"category":          "cloud",
	"kind":              "aws_s3_bucket",
	"provider":          "aws",
	"environment":       "prod",
	"resource_service":  "s3",
	"resource_category": "storage",
	"limit":             25,
}}

func TestRouteOwnsExactlyTheInfraResourceSearchFamily(t *testing.T) {
	t.Parallel()

	for _, toolName := range familyTools {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", toolName)
			continue
		}
		if request.Method != "POST" {
			t.Errorf("Route(%s) method = %q, want POST", toolName, request.Method)
		}
		if request.Query != nil {
			t.Errorf("Route(%s) query = %#v, want nil", toolName, request.Query)
		}
	}

	// Neighbours in the root resolveRoute switch, the sibling infra
	// aggregates, the other extracted families, and near-miss names: this
	// package must claim none of them.
	for _, toolName := range []string{
		"count_infra_resources",
		"get_infra_resource_inventory",
		"investigate_resource",
		"analyze_infra_relationships",
		"find_code",
		"find_symbol",
		"search_file_content",
		"search_entity_content",
		"list_kubernetes_correlations",
		"list_admission_decisions",
		"list_package_registry_packages",
		"list_container_image_identities",
		"find_infra_resource",
		"find_infra_resources_extra",
		"find_infra",
		"search_infra_resources",
		"list_infra_resources",
		"infra_resources",
		"FIND_INFRA_RESOURCES",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesInfraResourceSearchRequestContract(t *testing.T) {
	t.Parallel()

	request, handled := Route("find_infra_resources", populatedArguments)
	if !handled {
		t.Fatal("Route(find_infra_resources) handled = false, want true")
	}
	if !reflect.DeepEqual(request, wantPopulatedRequest) {
		t.Fatalf("Route() = %#v, want %#v", request, wantPopulatedRequest)
	}
}

// TestRouteCarriesEveryInfraResourceSearchBodyKey pins each of the eight keys
// on its own. The exact-request comparison already covers the set, but a
// per-key assertion names the dropped field when one goes missing, and the
// keys fail differently when lost: a dropped scope key 400s only the caller
// whose sole scope it was and silently widens everyone else's page, while a
// dropped limit never fails at all because the handler substitutes 50.
func TestRouteCarriesEveryInfraResourceSearchBodyKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("find_infra_resources", populatedArguments)
	if !handled {
		t.Fatal("Route(find_infra_resources) handled = false, want true")
	}
	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("body type = %T, want map[string]any", request.Body)
	}
	if got, want := len(body), len(searchBodyKeys); got != want {
		t.Fatalf("body carries %d keys (%#v), want %d", got, body, want)
	}
	for _, key := range searchBodyKeys {
		value, present := body[key]
		if !present {
			t.Errorf("body dropped %q entirely", key)
			continue
		}
		if want := wantPopulatedRequest.Body.(map[string]any)[key]; value != want {
			t.Errorf("body[%s] = %#v, want %#v", key, value, want)
		}
	}
	for _, key := range scopeBodyKeys {
		if _, present := body[key]; !present {
			t.Errorf("body dropped scope key %q; the handler 400s a request with no scope", key)
		}
	}
	if _, isInt := body["limit"].(int); !isInt {
		t.Errorf("body[limit] = %#v (%T), want a Go int the handler decodes as its Limit field", body["limit"], body["limit"])
	}

	// The search has no paging cursor, no offset, and no repository scope
	// key, so these must never appear.
	for _, key := range []string{"offset", "cursor", "after_id", "repo_id", "repository_id", "resource_id", "max_depth"} {
		if value, present := body[key]; present {
			t.Errorf("body carries %q = %#v, want the key absent", key, value)
		}
	}
}

func TestRouteAppliesInfraResourceSearchDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	request, handled := Route("find_infra_resources", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(find_infra_resources) handled = false, want true")
	}
	if got := request.Body.(map[string]any)["limit"]; got != 50 {
		t.Errorf("absent limit -> %#v, want the dispatcher default 50", got)
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly, including
	// float truncation toward zero and the fallback for unsupported types.
	// Out-of-range values are forwarded as-is: the handler, not the selector,
	// substitutes 50 for anything nonpositive and clamps anything above 200.
	for _, tt := range []struct {
		limit any
		want  int
	}{
		{limit: 25, want: 25},
		{limit: int64(26), want: 26},
		{limit: 27.9, want: 27},
		{limit: -3.9, want: -3},
		{limit: -7, want: -7},
		{limit: 0, want: 0},
		{limit: 500, want: 500},
		{limit: "25", want: 50},
		{limit: true, want: 50},
		{limit: nil, want: 50},
		{limit: float32(25), want: 50},
	} {
		request, _ := Route("find_infra_resources", routecontract.Arguments{"limit": tt.limit})
		if got := request.Body.(map[string]any)["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %#v, want %d", tt.limit, got, tt.want)
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every one of the seven string keys.
	for _, value := range []any{42, nil, true, []string{"cloud"}, struct{}{}, []byte("prod")} {
		for _, key := range scopeBodyKeys {
			request, _ := Route("find_infra_resources", routecontract.Arguments{key: value})
			if got := request.Body.(map[string]any)[key]; got != "" {
				t.Errorf("%s=%#v -> %#v, want empty", key, value, got)
			}
		}
	}
}

func TestRouteHandlesNilAndTypedNilInfraResourceSearchArguments(t *testing.T) {
	t.Parallel()

	want := routecontract.Request{Method: "POST", Path: "/api/v0/infra/resources/search", Body: map[string]any{
		"query":             "",
		"category":          "",
		"kind":              "",
		"provider":          "",
		"environment":       "",
		"resource_service":  "",
		"resource_category": "",
		"limit":             50,
	}}

	var typedNil map[string]any
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	} {
		request, handled := Route("find_infra_resources", tt.args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route() = %#v, want %#v", tt.name, request, want)
		}
	}
}

func TestRouteDoesNotAliasCallerInfraResourceSearchArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"category": "cloud", "limit": float64(25)}
	request, handled := Route("find_infra_resources", args)
	if !handled {
		t.Fatal("Route(find_infra_resources) handled = false, want true")
	}
	request.Body.(map[string]any)["category"] = "mutated"
	if got := args["category"]; got != "cloud" {
		t.Fatalf("Route mutated caller arguments through the returned body: category = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("Route grew caller arguments to %d keys, want 2", len(args))
	}

	// Two calls with the same arguments hand back independent body maps.
	first, _ := Route("find_infra_resources", args)
	second, _ := Route("find_infra_resources", args)
	first.Body.(map[string]any)["category"] = "mutated"
	if got := second.Body.(map[string]any)["category"]; got != "cloud" {
		t.Fatalf("Route shares a body map between calls: category = %#v", got)
	}
}
