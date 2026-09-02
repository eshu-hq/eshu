// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// populatedRouteCases gives every body key of every family tool a distinct
// populated value, so two keys swapped in a request builder fail the exact
// comparison instead of passing on a shared value. Numbers travel as float64
// in the arguments, the type JSON decoding hands MCP dispatch, and as Go int
// in the selected body.
var populatedRouteCases = []struct {
	tool     string
	path     string
	args     routecontract.Arguments
	wantBody map[string]any
}{
	{
		tool: "trace_deployment_chain",
		path: "/api/v0/impact/trace-deployment-chain",
		args: routecontract.Arguments{
			"service_name":                 "checkout",
			"direct_only":                  false,
			"max_depth":                    float64(3),
			"include_related_module_usage": true,
			"unused_decoy":                 "ignored",
		},
		wantBody: map[string]any{
			"service_name":                 "checkout",
			"direct_only":                  false,
			"max_depth":                    3,
			"include_related_module_usage": true,
		},
	},
	{
		tool: "investigate_deployment_config",
		path: "/api/v0/impact/deployment-config-influence",
		args: routecontract.Arguments{
			"service_name": "checkout",
			"workload_id":  "workload:checkout",
			"environment":  "prod",
			"limit":        float64(7),
			"unused_decoy": "ignored",
		},
		wantBody: map[string]any{
			"service_name": "checkout",
			"workload_id":  "workload:checkout",
			"environment":  "prod",
			"limit":        7,
		},
	},
	{
		tool: "find_blast_radius",
		path: "/api/v0/impact/blast-radius",
		args: routecontract.Arguments{
			"target":       "payments-db",
			"target_type":  "resource",
			"limit":        float64(9),
			"unused_decoy": "ignored",
		},
		wantBody: map[string]any{
			"target":      "payments-db",
			"target_type": "resource",
			"limit":       9,
		},
	},
	{
		tool: "find_change_surface",
		path: "/api/v0/impact/change-surface",
		args: routecontract.Arguments{
			"target":       "payments-db",
			"environment":  "staging",
			"limit":        float64(11),
			"unused_decoy": "ignored",
		},
		wantBody: map[string]any{
			"target":      "payments-db",
			"environment": "staging",
			"limit":       11,
		},
	},
	{
		tool: "investigate_contract_impact",
		path: "/api/v0/impact/contracts",
		args: routecontract.Arguments{
			"family":           "http",
			"provider_repo_id": "repo-provider",
			"consumer_repo_id": "repo-consumer",
			"repo_id":          "repo-any",
			"route":            "/v1/charge",
			"topic":            "charges",
			"service_name":     "billing",
			"method":           "POST",
			"limit":            float64(13),
			"unused_decoy":     "ignored",
		},
		wantBody: map[string]any{
			"family":           "http",
			"provider_repo_id": "repo-provider",
			"consumer_repo_id": "repo-consumer",
			"repo_id":          "repo-any",
			"route":            "/v1/charge",
			"topic":            "charges",
			"service_name":     "billing",
			"method":           "POST",
			"limit":            13,
		},
	},
	{
		tool: "investigate_change_surface",
		path: "/api/v0/impact/change-surface/investigate",
		args: routecontract.Arguments{
			"target":        "payments-db",
			"target_type":   "resource",
			"service_name":  "billing",
			"workload_id":   "workload:billing",
			"resource_id":   "res-42",
			"module_id":     "mod-7",
			"topic":         "charges",
			"repo_id":       "repo-any",
			"changed_paths": []any{"a.tf", "b.tf"},
			"environment":   "prod",
			"max_depth":     float64(2),
			"limit":         float64(15),
			"offset":        float64(5),
			"unused_decoy":  "ignored",
		},
		wantBody: map[string]any{
			"target":        "payments-db",
			"target_type":   "resource",
			"service_name":  "billing",
			"workload_id":   "workload:billing",
			"resource_id":   "res-42",
			"module_id":     "mod-7",
			"topic":         "charges",
			"repo_id":       "repo-any",
			"changed_paths": []any{"a.tf", "b.tf"},
			"environment":   "prod",
			"max_depth":     2,
			"limit":         15,
			"offset":        5,
		},
	},
	{
		tool: "trace_resource_to_code",
		path: "/api/v0/impact/trace-resource-to-code",
		args: routecontract.Arguments{
			"start":        "arn:aws:s3:::checkout-bucket",
			"environment":  "prod",
			"max_depth":    float64(6),
			"limit":        float64(17),
			"unused_decoy": "ignored",
		},
		wantBody: map[string]any{
			"start":       "arn:aws:s3:::checkout-bucket",
			"environment": "prod",
			"max_depth":   6,
			"limit":       17,
		},
	},
	{
		tool: "trace_exposure_path",
		path: "/api/v0/impact/trace-exposure-path",
		args: routecontract.Arguments{
			"source":           "ingress-gateway",
			"source_entity_id": "entity-9",
			"repo_id":          "repo-any",
			"max_depth":        float64(4),
			"unused_decoy":     "ignored",
		},
		wantBody: map[string]any{
			"source":           "ingress-gateway",
			"source_entity_id": "entity-9",
			"repo_id":          "repo-any",
			"max_depth":        4,
		},
	},
}

// defaultBodies is the exact body each selecting tool sends when the caller
// passes no arguments at all: every string key travels as an explicit empty
// string, every numeric default is the dispatcher's, direct_only defaults to
// true, and changed_paths travels as nil. explain_dependency_path is absent
// here because it forwards the argument map itself.
var defaultBodies = map[string]map[string]any{
	"trace_deployment_chain": {
		"service_name":                 "",
		"direct_only":                  true,
		"max_depth":                    0,
		"include_related_module_usage": false,
	},
	"investigate_deployment_config": {
		"service_name": "", "workload_id": "", "environment": "", "limit": 25,
	},
	"find_blast_radius": {
		"target": "", "target_type": "", "limit": 50,
	},
	"find_change_surface": {
		"target": "", "environment": "", "limit": 50,
	},
	"investigate_contract_impact": {
		"family": "", "provider_repo_id": "", "consumer_repo_id": "",
		"repo_id": "", "route": "", "topic": "", "service_name": "",
		"method": "", "limit": 25,
	},
	"investigate_change_surface": {
		"target": "", "target_type": "", "service_name": "", "workload_id": "",
		"resource_id": "", "module_id": "", "topic": "", "repo_id": "",
		"changed_paths": []any(nil), "environment": "", "max_depth": 4,
		"limit": 25, "offset": 0,
	},
	"trace_resource_to_code": {
		"start": "", "environment": "", "max_depth": 8, "limit": 50,
	},
	"trace_exposure_path": {
		"source": "", "source_entity_id": "", "repo_id": "", "max_depth": 5,
	},
}

func TestRouteOwnsExactlyTheImpactAnalysisFamily(t *testing.T) {
	t.Parallel()

	for _, tt := range populatedRouteCases {
		request, handled := Route(tt.tool, routecontract.Arguments{})
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", tt.tool)
			continue
		}
		if request.Method != "POST" {
			t.Errorf("Route(%s) method = %q, want POST", tt.tool, request.Method)
		}
		if request.Query != nil {
			t.Errorf("Route(%s) query = %#v, want nil", tt.tool, request.Query)
		}
	}
	// explain_dependency_path is excluded from populatedRouteCases because it
	// forwards the argument map rather than a populated body, but its transport
	// contract is the same as the other eight and must be pinned here too --
	// asserting only `handled` would let a method or query-string change pass.
	if request, handled := Route("explain_dependency_path", routecontract.Arguments{}); !handled {
		t.Error("Route(explain_dependency_path) handled = false, want true")
	} else {
		if request.Method != "POST" {
			t.Errorf("Route(explain_dependency_path) method = %q, want POST", request.Method)
		}
		if request.Query != nil {
			t.Errorf("Route(explain_dependency_path) query = %#v, want nil", request.Query)
		}
	}

	// Neighbours registered in the same ecosystem group, other extracted
	// families, arms of the root resolveRoute switch, and near-miss names:
	// this package must claim none of them.
	for _, toolName := range []string{
		"compare_environments",
		"get_ecosystem_overview",
		"get_repository_coverage",
		"find_infra_resources",
		"analyze_infra_relationships",
		"find_code",
		"list_relationship_edges",
		"list_kubernetes_correlations",
		"investigate_resource",
		"trace_route_callers",
		"trace_deployment",
		"trace_deployment_chains",
		"find_blast",
		"blast_radius",
		"change_surface",
		"find_change_surfaces",
		"investigate_change",
		"explain_dependency",
		"trace_exposure",
		"trace_resource",
		"TRACE_DEPLOYMENT_CHAIN",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesImpactRequestContracts(t *testing.T) {
	t.Parallel()

	for _, tt := range populatedRouteCases {
		request, handled := Route(tt.tool, tt.args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.tool)
		}
		want := routecontract.Request{Method: "POST", Path: tt.path, Body: tt.wantBody}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("Route(%s) = %#v, want %#v", tt.tool, request, want)
		}
	}
}

// TestRouteCarriesEveryImpactBodyKey pins each selecting tool's key set and
// each key's value individually, so a dropped or misspelled field is named
// rather than buried in a whole-request diff. A dropped key here is silent at
// the handler for defaulted fields (limit, max_depth, offset) and visibly
// narrows or widens results for selector fields, so the per-key assertion is
// the loud failure the request-level comparison alone would not give.
func TestRouteCarriesEveryImpactBodyKey(t *testing.T) {
	t.Parallel()

	for _, tt := range populatedRouteCases {
		request, handled := Route(tt.tool, tt.args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tt.tool, request.Body)
		}
		if got, want := len(body), len(tt.wantBody); got != want {
			t.Fatalf("Route(%s) body carries %d keys (%#v), want %d", tt.tool, got, body, want)
		}
		for key, want := range tt.wantBody {
			value, present := body[key]
			if !present {
				t.Errorf("Route(%s) body dropped %q entirely", tt.tool, key)
				continue
			}
			if !reflect.DeepEqual(value, want) {
				t.Errorf("Route(%s) body[%s] = %#v, want %#v", tt.tool, key, value, want)
			}
		}
	}
}

func TestRouteAppliesImpactDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	var typedNil map[string]any
	argCases := []struct {
		name string
		args routecontract.Arguments
	}{
		{name: "nil literal", args: nil},
		{name: "typed nil map", args: routecontract.Arguments(typedNil)},
		{name: "empty", args: routecontract.Arguments{}},
	}
	for tool, wantBody := range defaultBodies {
		for _, tt := range argCases {
			request, handled := Route(tool, tt.args)
			if !handled {
				t.Fatalf("Route(%s, %s) handled = false, want true", tool, tt.name)
			}
			if !reflect.DeepEqual(request.Body, map[string]any(wantBody)) {
				t.Fatalf("Route(%s, %s) body = %#v, want %#v", tool, tt.name, request.Body, wantBody)
			}
		}
	}

	// explain_dependency_path forwards the argument map itself, so an absent
	// map travels as a nil body rather than a defaulted one.
	for _, tt := range argCases {
		request, handled := Route("explain_dependency_path", tt.args)
		if !handled {
			t.Fatalf("Route(explain_dependency_path, %s) handled = false, want true", tt.name)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(explain_dependency_path, %s) body type = %T, want map[string]any", tt.name, request.Body)
		}
		// DeepEqual rather than a length check: a nil map and an empty map are
		// both length zero but serialize differently on the wire (null versus
		// {}), and this route forwards the argument map itself, so that
		// distinction is the contract. A length-only assertion would pass if a
		// regression defaulted an absent map into an empty one.
		if !reflect.DeepEqual(body, map[string]any(tt.args)) {
			t.Fatalf("Route(explain_dependency_path, %s) body = %#v, want the argument map unchanged", tt.name, body)
		}
		if (body == nil) != (tt.args == nil) {
			t.Fatalf("Route(explain_dependency_path, %s) body nil = %t, want %t; an absent map must stay nil rather than becoming an empty map", tt.name, body == nil, tt.args == nil)
		}
	}
}

// TestRouteAppliesImpactCoercions pins the numeric, boolean, and string
// coercions to routecontract.Arguments exactly: float64 truncates toward
// zero, unsupported numeric types fall back to the default, a non-bool
// direct_only falls back to true, and a wrong-typed string reads as empty
// rather than as a formatted Go value. Out-of-range numbers are forwarded
// as-is; the handler, not the selector, owns each route's bound.
