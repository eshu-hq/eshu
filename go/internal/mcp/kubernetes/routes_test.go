// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetestools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyTools is the one name this package owns.
var familyTools = []string{"list_kubernetes_correlations"}

// correlationQueryKeys is the exact ten-key query the listing sends. The
// handler requires limit outright and at least one of the six anchors, so
// the count and the spelling of each key are pinned here rather than left to
// the request comparison alone.
var correlationQueryKeys = []string{
	"after_correlation_id",
	"cluster_id",
	"drift_kind",
	"image_ref",
	"limit",
	"namespace",
	"outcome",
	"scope_id",
	"source_digest",
	"workload_object_id",
}

// anchorQueryKeys are the six keys of which the handler requires at least
// one; a request carrying none of them is rejected with 400.
var anchorQueryKeys = []string{
	"cluster_id",
	"image_ref",
	"namespace",
	"scope_id",
	"source_digest",
	"workload_object_id",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// the request builder fail the exact comparison below instead of passing on a
// shared value.
var populatedArguments = routecontract.Arguments{
	"after_correlation_id": "kubernetes-correlation-1",
	"cluster_id":           "cluster-prod",
	"drift_kind":           "in_sync",
	"image_ref":            "registry.example.com/checkout@sha256:abc",
	"limit":                float64(25),
	"namespace":            "payments",
	"outcome":              "exact",
	"scope_id":             "kubernetes-live://cluster-prod",
	"source_digest":        "sha256:abc",
	"workload_object_id":   "deployment/payments/checkout",
	"unused_decoy":         "ignored",
}

// wantPopulatedRequest is the request the ten populated keys must select.
var wantPopulatedRequest = routecontract.Request{Method: "GET", Path: "/api/v0/kubernetes/correlations", Query: map[string]string{
	"after_correlation_id": "kubernetes-correlation-1",
	"cluster_id":           "cluster-prod",
	"drift_kind":           "in_sync",
	"image_ref":            "registry.example.com/checkout@sha256:abc",
	"limit":                "25",
	"namespace":            "payments",
	"outcome":              "exact",
	"scope_id":             "kubernetes-live://cluster-prod",
	"source_digest":        "sha256:abc",
	"workload_object_id":   "deployment/payments/checkout",
}}

func TestRouteOwnsExactlyTheKubernetesCorrelationsFamily(t *testing.T) {
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
	// the sibling correlation listings, and near-miss names: this package
	// must claim none of them.
	for _, toolName := range []string{
		"list_indexed_repositories",
		"get_relationship_evidence",
		"list_service_catalog_correlations",
		"list_admission_decisions",
		"list_advisory_evidence",
		"get_repository_stats",
		"list_package_registry_packages",
		"list_ci_cd_run_correlations",
		"list_codeowners_ownership",
		"list_secrets_iam_posture_gaps",
		"list_observability_coverage_correlations",
		"list_container_image_identities",
		"list_supply_chain_impact_findings",
		"list_security_alert_reconciliations",
		"list_kubernetes_correlation",
		"list_kubernetes_correlations_extra",
		"list_kubernetes",
		"count_kubernetes_correlations",
		"get_kubernetes_correlations",
		"get_kubernetes_correlation_inventory",
		"kubernetes_correlations",
		"LIST_KUBERNETES_CORRELATIONS",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesKubernetesCorrelationsRequestContract(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_kubernetes_correlations", populatedArguments)
	if !handled {
		t.Fatal("Route(list_kubernetes_correlations) handled = false, want true")
	}
	if !reflect.DeepEqual(request, wantPopulatedRequest) {
		t.Fatalf("Route() = %#v, want %#v", request, wantPopulatedRequest)
	}
}

// TestRouteCarriesEveryKubernetesCorrelationsQueryKey pins each of the ten
// keys on its own. The exact-request comparison already covers the set, but a
// per-key assertion names the dropped filter when one goes missing, and the
// keys fail in different ways when lost: limit 400s outright, a lost anchor
// 400s only when it was the caller's sole anchor, and outcome, drift_kind, and
// after_correlation_id widen or restart the page silently.
func TestRouteCarriesEveryKubernetesCorrelationsQueryKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_kubernetes_correlations", populatedArguments)
	if !handled {
		t.Fatal("Route(list_kubernetes_correlations) handled = false, want true")
	}
	if got, want := len(request.Query), len(correlationQueryKeys); got != want {
		t.Fatalf("query carries %d keys (%#v), want %d", got, request.Query, want)
	}
	for _, key := range correlationQueryKeys {
		value, present := request.Query[key]
		if !present {
			t.Errorf("query dropped %q entirely", key)
			continue
		}
		if want := wantPopulatedRequest.Query[key]; value != want {
			t.Errorf("query[%s] = %q, want %q", key, value, want)
		}
	}
	for _, key := range anchorQueryKeys {
		if _, present := request.Query[key]; !present {
			t.Errorf("query dropped anchor %q; the handler 400s a request with no anchor", key)
		}
	}

	// The listing pages by after_correlation_id only and has no aggregate
	// sibling, so these keys must never appear.
	for _, key := range []string{"offset", "group_by", "cursor", "repository_id", "correlation_id", "workload_id"} {
		if value, present := request.Query[key]; present {
			t.Errorf("query carries %q = %q, want the key absent", key, value)
		}
	}
}

func TestRouteAppliesKubernetesCorrelationsDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_kubernetes_correlations", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_kubernetes_correlations) handled = false, want true")
	}
	if got := request.Query["limit"]; got != "50" {
		t.Errorf("absent limit -> %q, want the dispatcher default 50", got)
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly, including
	// float truncation toward zero and the fallback for unsupported types.
	// Out-of-range values are forwarded as-is: the handler, not the selector,
	// owns the bound and rejects anything outside 1..200 with 400.
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
		{limit: 500, want: "500"},
		{limit: "25", want: "50"},
		{limit: true, want: "50"},
		{limit: nil, want: "50"},
		{limit: float32(25), want: "50"},
	} {
		request, _ := Route("list_kubernetes_correlations", routecontract.Arguments{"limit": tt.limit})
		if got := request.Query["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %q, want %q", tt.limit, got, tt.want)
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every one of the nine string keys.
	for _, value := range []any{42, nil, true, []string{"cluster-prod"}, struct{}{}, []byte("payments")} {
		for _, key := range correlationQueryKeys {
			if key == "limit" {
				continue
			}
			request, _ := Route("list_kubernetes_correlations", routecontract.Arguments{key: value})
			if got := request.Query[key]; got != "" {
				t.Errorf("%s=%#v -> %q, want empty", key, value, got)
			}
		}
	}
}

func TestRouteHandlesNilAndTypedNilKubernetesCorrelationsArguments(t *testing.T) {
	t.Parallel()

	want := routecontract.Request{Method: "GET", Path: "/api/v0/kubernetes/correlations", Query: map[string]string{
		"after_correlation_id": "",
		"cluster_id":           "",
		"drift_kind":           "",
		"image_ref":            "",
		"limit":                "50",
		"namespace":            "",
		"outcome":              "",
		"scope_id":             "",
		"source_digest":        "",
		"workload_object_id":   "",
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
		request, handled := Route("list_kubernetes_correlations", tt.args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route() = %#v, want %#v", tt.name, request, want)
		}
	}
}

func TestRouteDoesNotAliasCallerKubernetesCorrelationsArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"cluster_id": "cluster-a", "limit": float64(25)}
	request, handled := Route("list_kubernetes_correlations", args)
	if !handled {
		t.Fatal("Route(list_kubernetes_correlations) handled = false, want true")
	}
	request.Query["cluster_id"] = "mutated"
	if got := args["cluster_id"]; got != "cluster-a" {
		t.Fatalf("Route mutated caller arguments through the returned query: cluster_id = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("Route grew caller arguments to %d keys, want 2", len(args))
	}

	// Two calls with the same arguments hand back independent query maps.
	first, _ := Route("list_kubernetes_correlations", args)
	second, _ := Route("list_kubernetes_correlations", args)
	first.Query["cluster_id"] = "mutated"
	if got := second.Query["cluster_id"]; got != "cluster-a" {
		t.Fatalf("Route shares a query map between calls: cluster_id = %q", got)
	}
}
