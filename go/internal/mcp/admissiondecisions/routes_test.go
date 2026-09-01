// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admissiondecisionstools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyTools is the one name this package owns.
var familyTools = []string{"list_admission_decisions"}

// decisionQueryKeys is the exact eight-key query the listing sends. Three of
// them are required by the handler and 400 when lost, so the count and the
// spelling of each key are pinned here rather than left to the request
// comparison alone.
var decisionQueryKeys = []string{
	"anchor_id",
	"anchor_kind",
	"domain",
	"generation_id",
	"include_evidence",
	"limit",
	"scope_id",
	"state",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// the request builder fail the exact comparison below instead of passing on a
// shared value.
var populatedArguments = routecontract.Arguments{
	"anchor_id":        "repo://team/api",
	"anchor_kind":      "repository",
	"domain":           "deployable_unit",
	"generation_id":    "generation-1",
	"include_evidence": true,
	"limit":            float64(25),
	"scope_id":         "git-repository-scope:team/api",
	"state":            "missing_evidence",
	"unused_decoy":     "ignored",
}

// wantPopulatedRequest is the request the eight populated keys must select.
var wantPopulatedRequest = routecontract.Request{Method: "GET", Path: "/api/v0/evidence/admission-decisions", Query: map[string]string{
	"anchor_id":        "repo://team/api",
	"anchor_kind":      "repository",
	"domain":           "deployable_unit",
	"generation_id":    "generation-1",
	"include_evidence": "true",
	"limit":            "25",
	"scope_id":         "git-repository-scope:team/api",
	"state":            "missing_evidence",
}}

func TestRouteOwnsExactlyTheAdmissionDecisionsFamily(t *testing.T) {
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
		"get_relationship_evidence",
		"list_service_catalog_correlations",
		"list_kubernetes_correlations",
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
		"list_admission_decision",
		"list_admission_decisions_extra",
		"list_admission",
		"count_admission_decisions",
		"get_admission_decisions",
		"get_admission_decision_inventory",
		"admission_decisions",
		"LIST_ADMISSION_DECISIONS",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesAdmissionDecisionsRequestContract(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_admission_decisions", populatedArguments)
	if !handled {
		t.Fatal("Route(list_admission_decisions) handled = false, want true")
	}
	if !reflect.DeepEqual(request, wantPopulatedRequest) {
		t.Fatalf("Route() = %#v, want %#v", request, wantPopulatedRequest)
	}
}

// TestRouteCarriesEveryAdmissionDecisionsQueryKey pins each of the eight keys
// on its own. The exact-request comparison already covers the set, but a
// per-key assertion names the dropped filter when one goes missing, and the
// keys fail in different ways when lost: domain, scope_id, and generation_id
// 400, a lone anchor half 400s, and state, include_evidence, and limit widen
// or reshape the page silently.
func TestRouteCarriesEveryAdmissionDecisionsQueryKey(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_admission_decisions", populatedArguments)
	if !handled {
		t.Fatal("Route(list_admission_decisions) handled = false, want true")
	}
	if got, want := len(request.Query), len(decisionQueryKeys); got != want {
		t.Fatalf("query carries %d keys (%#v), want %d", got, request.Query, want)
	}
	for _, key := range decisionQueryKeys {
		value, present := request.Query[key]
		if !present {
			t.Errorf("query dropped %q entirely", key)
			continue
		}
		if want := wantPopulatedRequest.Query[key]; value != want {
			t.Errorf("query[%s] = %q, want %q", key, value, want)
		}
	}

	// The listing is limit-bounded with no cursor and no aggregate sibling,
	// so these keys must never appear.
	for _, key := range []string{"offset", "group_by", "after_decision_id", "cursor", "repository_id", "decision_id"} {
		if value, present := request.Query[key]; present {
			t.Errorf("query carries %q = %q, want the key absent", key, value)
		}
	}
}

func TestRouteAppliesAdmissionDecisionsDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_admission_decisions", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_admission_decisions) handled = false, want true")
	}
	if got := request.Query["limit"]; got != "50" {
		t.Errorf("absent limit -> %q, want the dispatcher default 50", got)
	}
	if got := request.Query["include_evidence"]; got != "false" {
		t.Errorf("absent include_evidence -> %q, want an explicit false", got)
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly, including
	// float truncation toward zero and the fallback for unsupported types.
	// Out-of-range values are forwarded as-is: the handler, not the selector,
	// owns the bound (nonpositive becomes the 50-row default; over-200 caps).
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
		request, _ := Route("list_admission_decisions", routecontract.Arguments{"limit": tt.limit})
		if got := request.Query["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %q, want %q", tt.limit, got, tt.want)
		}
	}

	// Boolean coercion matches routecontract.Arguments.BoolOr exactly: only a
	// Go bool is honoured, and every other type -- including the strings
	// "true" and "1" -- falls back to false rather than being parsed.
	for _, tt := range []struct {
		include any
		want    string
	}{
		{include: true, want: "true"},
		{include: false, want: "false"},
		{include: "true", want: "false"},
		{include: "1", want: "false"},
		{include: 1, want: "false"},
		{include: nil, want: "false"},
	} {
		request, _ := Route("list_admission_decisions", routecontract.Arguments{"include_evidence": tt.include})
		if got := request.Query["include_evidence"]; got != tt.want {
			t.Errorf("include_evidence=%#v -> %q, want %q", tt.include, got, tt.want)
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every one of the six string keys.
	for _, value := range []any{42, nil, true, []string{"deployable_unit"}, struct{}{}, []byte("repository")} {
		for _, key := range decisionQueryKeys {
			if key == "limit" || key == "include_evidence" {
				continue
			}
			request, _ := Route("list_admission_decisions", routecontract.Arguments{key: value})
			if got := request.Query[key]; got != "" {
				t.Errorf("%s=%#v -> %q, want empty", key, value, got)
			}
		}
	}
}

func TestRouteHandlesNilAndTypedNilAdmissionDecisionsArguments(t *testing.T) {
	t.Parallel()

	want := routecontract.Request{Method: "GET", Path: "/api/v0/evidence/admission-decisions", Query: map[string]string{
		"anchor_id":        "",
		"anchor_kind":      "",
		"domain":           "",
		"generation_id":    "",
		"include_evidence": "false",
		"limit":            "50",
		"scope_id":         "",
		"state":            "",
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
		request, handled := Route("list_admission_decisions", tt.args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route() = %#v, want %#v", tt.name, request, want)
		}
	}
}

func TestRouteDoesNotAliasCallerAdmissionDecisionsArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"scope_id": "scope-a", "limit": float64(25)}
	request, handled := Route("list_admission_decisions", args)
	if !handled {
		t.Fatal("Route(list_admission_decisions) handled = false, want true")
	}
	request.Query["scope_id"] = "mutated"
	if got := args["scope_id"]; got != "scope-a" {
		t.Fatalf("Route mutated caller arguments through the returned query: scope_id = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("Route grew caller arguments to %d keys, want 2", len(args))
	}

	// Two calls with the same arguments hand back independent query maps.
	first, _ := Route("list_admission_decisions", args)
	second, _ := Route("list_admission_decisions", args)
	first.Query["scope_id"] = "mutated"
	if got := second.Query["scope_id"]; got != "scope-a" {
		t.Fatalf("Route shares a query map between calls: scope_id = %q", got)
	}
}
