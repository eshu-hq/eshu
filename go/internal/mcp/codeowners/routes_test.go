// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeownerstools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestRouteOwnsExactlyTheCodeownersFamily(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_codeowners_ownership", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_codeowners_ownership) handled = false, want true")
	}
	if request.Method != "GET" {
		t.Errorf("method = %q, want GET", request.Method)
	}
	if request.Body != nil {
		t.Errorf("body = %#v, want nil", request.Body)
	}

	// Neighbours in the root repository switch, plus near-miss names: this
	// package must claim none of them.
	for _, toolName := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"get_ci_cd_run_correlation_inventory",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_container_image_identities",
		"list_advisory_evidence",
		"get_repository_stats",
		"list_codeowners_ownerships",
		"list_codeowners_ownership_extra",
		"list_codeowners",
		"count_codeowners_ownership",
		"get_codeowners_ownership",
		"get_codeowners_ownership_inventory",
		"LIST_CODEOWNERS_OWNERSHIP",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesCodeownersRequestContract(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{
		"after_order_index": float64(12),
		"after_pattern":     "/services/api/",
		"after_ref":         "@eshu-hq/platform",
		"limit":             float64(25),
		"repository_id":     "repo-web",
		"unused_decoy":      "ignored",
	}
	want := routecontract.Request{Method: "GET", Path: "/api/v0/codeowners/ownership", Query: map[string]string{
		"repository_id":     "repo-web",
		"limit":             "25",
		"after_order_index": "12",
		"after_pattern":     "/services/api/",
		"after_ref":         "@eshu-hq/platform",
	}}

	request, handled := Route("list_codeowners_ownership", args)
	if !handled {
		t.Fatal("Route handled = false, want true")
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("Route = %#v, want %#v", request, want)
	}
}

func TestRouteAppliesCodeownersDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	// An absent limit falls back to the dispatcher's documented default of 50.
	request, handled := Route("list_codeowners_ownership", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route handled = false, want true")
	}
	if got := request.Query["limit"]; got != "50" {
		t.Errorf("absent limit -> %q, want 50", got)
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly, including
	// float truncation toward zero and the fallback for unsupported types.
	for _, tt := range []struct {
		limit any
		want  string
	}{
		{limit: 25, want: "25"},
		{limit: int64(26), want: "26"},
		{limit: 27.9, want: "27"},
		{limit: -3.9, want: "-3"},
		{limit: -7, want: "-7"},
		{limit: 0, want: "0"},
		{limit: "25", want: "50"},
		{limit: true, want: "50"},
		{limit: nil, want: "50"},
	} {
		request, _ := Route("list_codeowners_ownership", routecontract.Arguments{"limit": tt.limit})
		if got := request.Query["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %q, want %q", tt.limit, got, tt.want)
		}
	}

	// Wrong-typed and absent string arguments both read as empty, never as a
	// formatted Go value.
	for _, tt := range []struct {
		key   string
		value any
	}{
		{key: "repository_id", value: 42},
		{key: "after_pattern", value: nil},
		{key: "after_ref", value: []string{"@eshu-hq/platform"}},
	} {
		request, _ := Route("list_codeowners_ownership", routecontract.Arguments{tt.key: tt.value})
		if got := request.Query[tt.key]; got != "" {
			t.Errorf("%s=%#v -> %q, want empty", tt.key, tt.value, got)
		}
	}

	// The route carries exactly the five keys the handler reads, and no more.
	full, _ := Route("list_codeowners_ownership", routecontract.Arguments{"offset": 5, "group_by": "owner", "scope_id": "scope-a"})
	if got, want := len(full.Query), 5; got != want {
		t.Errorf("query carries %d keys (%#v), want %d", got, full.Query, want)
	}
	for _, key := range []string{"offset", "group_by", "scope_id"} {
		if _, present := full.Query[key]; present {
			t.Errorf("query carries %q, want the key absent", key)
		}
	}
}

// TestRouteKeepsAnAbsentOrderIndexCursorEmpty pins the one coercion this
// family does not share with the rest of the dispatcher. after_order_index is
// the numeric leg of a three-part keyset cursor, and the handler accepts the
// cursor only when all three legs arrive together. An absent leg must stay the
// empty string; coercing it to "0" — what a plain IntOr default would do —
// turns a caller's first page into a half-supplied cursor the handler rejects
// or, worse, silently reads as a seek past order index zero.
func TestRouteKeepsAnAbsentOrderIndexCursorEmpty(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		args routecontract.Arguments
		want string
	}{
		{name: "absent", args: routecontract.Arguments{}, want: ""},
		{name: "absent alongside the other two legs", args: routecontract.Arguments{"after_pattern": "/services/", "after_ref": "@team"}, want: ""},
		{name: "nil arguments", args: nil, want: ""},
		{name: "present as int", args: routecontract.Arguments{"after_order_index": 12}, want: "12"},
		{name: "present as int64", args: routecontract.Arguments{"after_order_index": int64(13)}, want: "13"},
		{name: "present as float64", args: routecontract.Arguments{"after_order_index": 14.9}, want: "14"},
		{name: "present as a negative float64", args: routecontract.Arguments{"after_order_index": -14.9}, want: "-14"},
		{name: "present as zero", args: routecontract.Arguments{"after_order_index": 0}, want: "0"},
		{name: "present as explicit nil", args: routecontract.Arguments{"after_order_index": nil}, want: "0"},
		{name: "present as a string", args: routecontract.Arguments{"after_order_index": "12"}, want: "0"},
		{name: "present as a bool", args: routecontract.Arguments{"after_order_index": true}, want: "0"},
		{name: "present as an empty string", args: routecontract.Arguments{"after_order_index": ""}, want: "0"},
		{name: "present as a typed nil slice", args: routecontract.Arguments{"after_order_index": []string(nil)}, want: "0"},
	} {
		request, handled := Route("list_codeowners_ownership", tt.args)
		if !handled {
			t.Fatalf("%s: Route handled = false, want true", tt.name)
		}
		if got := request.Query["after_order_index"]; got != tt.want {
			t.Errorf("%s: after_order_index = %q, want %q", tt.name, got, tt.want)
		}
		if _, present := request.Query["after_order_index"]; !present {
			t.Errorf("%s: after_order_index key is missing entirely, want it present", tt.name)
		}
	}

	// The presence rule is specific to the cursor leg: limit still takes its
	// default when absent rather than reading as empty.
	request, _ := Route("list_codeowners_ownership", routecontract.Arguments{})
	if got := request.Query["limit"]; got == "" {
		t.Error("absent limit read as empty, want the default 50")
	}
}

func TestRouteHandlesNilAndTypedNilCodeownersArguments(t *testing.T) {
	t.Parallel()

	var typedNil map[string]any
	want := routecontract.Request{Method: "GET", Path: "/api/v0/codeowners/ownership", Query: map[string]string{
		"repository_id":     "",
		"limit":             "50",
		"after_order_index": "",
		"after_pattern":     "",
		"after_ref":         "",
	}}
	for _, tt := range []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	} {
		request, handled := Route("list_codeowners_ownership", tt.args)
		if !handled {
			t.Fatalf("%s: Route handled = false, want true", tt.name)
		}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route = %#v, want %#v", tt.name, request, want)
		}
	}
}

func TestRouteDoesNotAliasCallerCodeownersArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"repository_id": "repo-web", "limit": float64(25)}
	request, handled := Route("list_codeowners_ownership", args)
	if !handled {
		t.Fatal("Route handled = false, want true")
	}
	request.Query["repository_id"] = "mutated"
	if got := args["repository_id"]; got != "repo-web" {
		t.Fatalf("caller arguments mutated through the returned query: repository_id = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("caller arguments grew to %d keys, want 2", len(args))
	}

	// Two calls with the same arguments hand back independent query maps.
	first, _ := Route("list_codeowners_ownership", args)
	second, _ := Route("list_codeowners_ownership", args)
	first.Query["after_ref"] = "mutated"
	if got := second.Query["after_ref"]; got != "" {
		t.Fatalf("a later request shares the earlier query map: after_ref = %q", got)
	}
}
