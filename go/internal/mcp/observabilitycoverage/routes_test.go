// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package observabilitycoveragetools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyTools is the one name this package owns.
var familyTools = []string{"list_observability_coverage_correlations"}

// coverageQueryKeys is the exact twelve-key query the correlations listing
// sends. The listing has the widest key set in the repository router, so the
// count and the spelling of each key are pinned here rather than left to the
// request comparison alone.
var coverageQueryKeys = []string{
	"after_correlation_id",
	"coverage_signal",
	"coverage_status",
	"limit",
	"observability_object_ref",
	"outcome",
	"provider",
	"resource_class",
	"scope_id",
	"source_class",
	"target_service_ref",
	"target_uid",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// the request builder fail the exact comparison below instead of passing on a
// shared value.
var populatedArguments = routecontract.Arguments{
	"after_correlation_id":     "observability-coverage-1",
	"coverage_signal":          "alarm",
	"coverage_status":          "covered",
	"limit":                    float64(25),
	"observability_object_ref": "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
	"outcome":                  "exact",
	"provider":                 "aws",
	"resource_class":           "dashboard",
	"scope_id":                 "aws-account://111122223333",
	"source_class":             "declared",
	"target_service_ref":       "checkout",
	"target_uid":               "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
	"unused_decoy":             "ignored",
}

// wantPopulatedRequest is the request the twelve populated keys must select.
var wantPopulatedRequest = routecontract.Request{Method: "GET", Path: "/api/v0/observability/coverage/correlations", Query: map[string]string{
	"after_correlation_id":     "observability-coverage-1",
	"coverage_signal":          "alarm",
	"coverage_status":          "covered",
	"limit":                    "25",
	"observability_object_ref": "arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high",
	"outcome":                  "exact",
	"provider":                 "aws",
	"resource_class":           "dashboard",
	"scope_id":                 "aws-account://111122223333",
	"source_class":             "declared",
	"target_service_ref":       "checkout",
	"target_uid":               "arn:aws:ec2:us-east-1:111122223333:instance/i-abc",
}}

func TestRouteOwnsExactlyTheObservabilityCoverageFamily(t *testing.T) {
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

	// Neighbours in the root repository switch, the other extracted families,
	// and near-miss names: this package must claim none of them.
	for _, toolName := range []string{
		"list_indexed_repositories",
		"list_admission_decisions",
		"list_package_registry_packages",
		"count_package_registry_packages",
		"list_ci_cd_run_correlations",
		"count_ci_cd_run_correlations",
		"list_codeowners_ownership",
		"list_secrets_iam_posture_gaps",
		"count_secrets_iam_posture",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
		"list_container_image_identities",
		"list_advisory_evidence",
		"get_repository_stats",
		"list_observability_coverage_correlation",
		"list_observability_coverage_correlations_extra",
		"list_observability_coverage",
		"list_observability_coverage_gaps",
		"count_observability_coverage_correlations",
		"get_observability_coverage_correlations",
		"get_observability_coverage_inventory",
		"observability_coverage_correlations",
		"LIST_OBSERVABILITY_COVERAGE_CORRELATIONS",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesObservabilityCoverageRequestContract(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_observability_coverage_correlations", populatedArguments)
	if !handled {
		t.Fatal("Route(list_observability_coverage_correlations) handled = false, want true")
	}
	if !reflect.DeepEqual(request, wantPopulatedRequest) {
		t.Fatalf("Route() = %#v, want %#v", request, wantPopulatedRequest)
	}
}

// TestRouteCarriesEveryObservabilityCoverageQueryKey pins each of the twelve
// keys on its own. The exact-request comparison already covers the set, but a
// per-key assertion names the dropped filter when one goes missing, and this
// listing carries more filter keys than any other route in the repository
// router.
func TestRouteCarriesEveryObservabilityCoverageQueryKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_observability_coverage_correlations", populatedArguments)
	if !handled {
		t.Fatal("Route(list_observability_coverage_correlations) handled = false, want true")
	}
	if got, want := len(request.Query), len(coverageQueryKeys); got != want {
		t.Fatalf("query carries %d keys (%#v), want %d", got, request.Query, want)
	}
	for _, key := range coverageQueryKeys {
		value, present := request.Query[key]
		if !present {
			t.Errorf("query dropped %q entirely", key)
			continue
		}
		if want := wantPopulatedRequest.Query[key]; value != want {
			t.Errorf("query[%s] = %q, want %q", key, value, want)
		}
	}

	// The listing pages by cursor only, and has no aggregate sibling, so
	// these keys must never appear.
	for _, key := range []string{"offset", "group_by", "repository_id", "scope", "target_ref", "signal"} {
		if value, present := request.Query[key]; present {
			t.Errorf("query carries %q = %q, want the key absent", key, value)
		}
	}
}

func TestRouteAppliesObservabilityCoverageDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_observability_coverage_correlations", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_observability_coverage_correlations) handled = false, want true")
	}
	if got := request.Query["limit"]; got != "50" {
		t.Errorf("absent limit -> %q, want the dispatcher default 50", got)
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
		{limit: float32(25), want: "50"},
	} {
		request, _ := Route("list_observability_coverage_correlations", routecontract.Arguments{"limit": tt.limit})
		if got := request.Query["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %q, want %q", tt.limit, got, tt.want)
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every one of the eleven string keys.
	for _, value := range []any{42, nil, true, []string{"aws"}, struct{}{}, []byte("aws")} {
		for _, key := range coverageQueryKeys {
			if key == "limit" {
				continue
			}
			request, _ := Route("list_observability_coverage_correlations", routecontract.Arguments{key: value})
			if got := request.Query[key]; got != "" {
				t.Errorf("%s=%#v -> %q, want empty", key, value, got)
			}
		}
	}
}

func TestRouteHandlesNilAndTypedNilObservabilityCoverageArguments(t *testing.T) {
	t.Parallel()

	want := routecontract.Request{Method: "GET", Path: "/api/v0/observability/coverage/correlations", Query: map[string]string{
		"after_correlation_id":     "",
		"coverage_signal":          "",
		"coverage_status":          "",
		"limit":                    "50",
		"observability_object_ref": "",
		"outcome":                  "",
		"provider":                 "",
		"resource_class":           "",
		"scope_id":                 "",
		"source_class":             "",
		"target_service_ref":       "",
		"target_uid":               "",
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
		request, handled := Route("list_observability_coverage_correlations", tt.args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route() = %#v, want %#v", tt.name, request, want)
		}
	}
}

func TestRouteDoesNotAliasCallerObservabilityCoverageArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"scope_id": "scope-a", "limit": float64(25)}
	request, handled := Route("list_observability_coverage_correlations", args)
	if !handled {
		t.Fatal("Route(list_observability_coverage_correlations) handled = false, want true")
	}
	request.Query["scope_id"] = "mutated"
	if got := args["scope_id"]; got != "scope-a" {
		t.Fatalf("Route mutated caller arguments through the returned query: scope_id = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("Route grew caller arguments to %d keys, want 2", len(args))
	}

	// Two calls with the same arguments hand back independent query maps.
	first, _ := Route("list_observability_coverage_correlations", args)
	second, _ := Route("list_observability_coverage_correlations", args)
	first.Query["scope_id"] = "mutated"
	if got := second.Query["scope_id"]; got != "scope-a" {
		t.Fatalf("Route shares a query map between calls: scope_id = %q", got)
	}
}
