// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	impacttools "github.com/eshu-hq/eshu/go/internal/mcp/impact"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// impactRouteTools lists every tool the impact child package owns.
var impactRouteTools = []string{
	"trace_deployment_chain",
	"investigate_deployment_config",
	"find_blast_radius",
	"find_change_surface",
	"investigate_contract_impact",
	"investigate_change_surface",
	"trace_resource_to_code",
	"explain_dependency_path",
	"trace_exposure_path",
}

func TestResolveRouteUsesExactImpactChildRequests(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"service_name":  "checkout",
			"workload_id":   "workload:checkout",
			"environment":   "prod",
			"target":        "payments-db",
			"target_type":   "resource",
			"start":         "arn:aws:s3:::checkout-bucket",
			"source":        "ingress-gateway",
			"repo_id":       "repo-any",
			"changed_paths": []any{"a.tf"},
			"direct_only":   false,
			"max_depth":     float64(3),
			"limit":         float64(7),
			"offset":        float64(5),
		}},
		{name: "malformed", args: map[string]any{
			"service_name": 42,
			"target":       nil,
			"limit":        "25",
			"max_depth":    true,
			"direct_only":  "yes",
			"environment":  []string{"prod"},
		}},
	}

	for _, tool := range impactRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := impacttools.Route(tool, routecontract.Arguments(tt.args))
			if !handled {
				t.Fatalf("child Route(%s) handled = false, want true", tool)
			}
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

// TestImpactDispatchKeepsEveryBodyKey proves each family tool's method,
// path, and full body survive the adapter boundary, asserted against literal
// expectations rather than against the child selector: the parity test above
// builds both of its sides from that selector, so it cannot notice a key the
// child itself dropped, misspelled, or defaulted differently. Every key
// carries a distinct populated value so two swapped keys cannot pass on a
// shared one.
func TestImpactDispatchKeepsEveryBodyKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"service_name":                 "billing",
		"workload_id":                  "workload:billing",
		"environment":                  "prod",
		"direct_only":                  false,
		"max_depth":                    float64(3),
		"include_related_module_usage": true,
		"target":                       "payments-db",
		"target_type":                  "resource",
		"resource_id":                  "res-42",
		"module_id":                    "mod-7",
		"topic":                        "charges",
		"repo_id":                      "repo-any",
		"provider_repo_id":             "repo-provider",
		"consumer_repo_id":             "repo-consumer",
		"family":                       "http",
		"route":                        "/v1/charge",
		"method":                       "POST",
		"changed_paths":                []any{"a.tf", "b.tf"},
		"start":                        "arn:aws:s3:::checkout-bucket",
		"source":                       "ingress-gateway",
		"source_entity_id":             "entity-9",
		"limit":                        float64(7),
		"offset":                       float64(5),
	}

	wantBodies := map[string]map[string]any{
		"trace_deployment_chain": {
			"service_name":                 "billing",
			"direct_only":                  false,
			"max_depth":                    3,
			"include_related_module_usage": true,
		},
		"investigate_deployment_config": {
			"service_name": "billing", "workload_id": "workload:billing",
			"environment": "prod", "limit": 7,
		},
		"find_blast_radius": {
			"target": "payments-db", "target_type": "resource", "limit": 7,
		},
		"find_change_surface": {
			"target": "payments-db", "environment": "prod", "limit": 7,
		},
		"investigate_contract_impact": {
			"family": "http", "provider_repo_id": "repo-provider",
			"consumer_repo_id": "repo-consumer", "repo_id": "repo-any",
			"route": "/v1/charge", "topic": "charges",
			"service_name": "billing", "method": "POST", "limit": 7,
		},
		"investigate_change_surface": {
			"target": "payments-db", "target_type": "resource",
			"service_name": "billing", "workload_id": "workload:billing",
			"resource_id": "res-42", "module_id": "mod-7",
			"topic": "charges", "repo_id": "repo-any",
			"changed_paths": []any{"a.tf", "b.tf"}, "environment": "prod",
			"max_depth": 3, "limit": 7, "offset": 5,
		},
		"trace_resource_to_code": {
			"start": "arn:aws:s3:::checkout-bucket", "environment": "prod",
			"max_depth": 3, "limit": 7,
		},
		"trace_exposure_path": {
			"source": "ingress-gateway", "source_entity_id": "entity-9",
			"repo_id": "repo-any", "max_depth": 3,
		},
	}
	wantPaths := map[string]string{
		"trace_deployment_chain":        "/api/v0/impact/trace-deployment-chain",
		"investigate_deployment_config": "/api/v0/impact/deployment-config-influence",
		"find_blast_radius":             "/api/v0/impact/blast-radius",
		"find_change_surface":           "/api/v0/impact/change-surface",
		"investigate_contract_impact":   "/api/v0/impact/contracts",
		"investigate_change_surface":    "/api/v0/impact/change-surface/investigate",
		"trace_resource_to_code":        "/api/v0/impact/trace-resource-to-code",
		"explain_dependency_path":       "/api/v0/impact/explain-dependency-path",
		"trace_exposure_path":           "/api/v0/impact/trace-exposure-path",
	}

	for tool, wantBody := range wantBodies {
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if got.method != "POST" {
			t.Errorf("%s method = %q, want POST", tool, got.method)
		}
		if got.path != wantPaths[tool] {
			t.Errorf("%s path = %q, want %q", tool, got.path, wantPaths[tool])
		}
		if got.query != nil {
			t.Errorf("%s query = %#v, want nil", tool, got.query)
		}
		body, ok := got.body.(map[string]any)
		if !ok {
			t.Fatalf("%s body type = %T, want map[string]any", tool, got.body)
		}
		if n, wantN := len(body), len(wantBody); n != wantN {
			t.Fatalf("%s body carries %d keys (%#v), want %d", tool, n, body, wantN)
		}
		for key, want := range wantBody {
			value, present := body[key]
			if !present {
				t.Errorf("%s dispatch dropped %q entirely", tool, key)
				continue
			}
			if !reflect.DeepEqual(value, want) {
				t.Errorf("%s body[%s] = %#v, want %#v", tool, key, value, want)
			}
		}
	}

	// explain_dependency_path forwards the caller's argument map unchanged;
	// asserting the full 23-key map again would only re-list args, so pin
	// the pass-through by identity of content instead.
	got, err := resolveRoute("explain_dependency_path", args)
	if err != nil {
		t.Fatalf("resolveRoute(explain_dependency_path) error = %v, want nil", err)
	}
	if got.path != wantPaths["explain_dependency_path"] {
		t.Errorf("explain_dependency_path path = %q, want %q", got.path, wantPaths["explain_dependency_path"])
	}
	passBody, ok := got.body.(map[string]any)
	if !ok {
		t.Fatalf("explain_dependency_path body type = %T, want map[string]any", got.body)
	}
	if !reflect.DeepEqual(passBody, args) {
		t.Fatalf("explain_dependency_path body = %#v, want the argument map unchanged", passBody)
	}
}

// TestResolveRouteStillOwnsItsArmsAfterImpactExtraction proves the adapter
// consulted in resolveRoute's default case claims only this family, leaving
// every delegation and switch arm answered as before, and that an unknown
// tool still resolves to an error rather than a nil route.
func TestResolveRouteStillOwnsItsArmsAfterImpactExtraction(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"compare_environments",
		"get_ecosystem_overview",
		"get_repository_coverage",
		"find_infra_resources",
		"analyze_infra_relationships",
		"find_code",
		"find_symbol",
		"list_kubernetes_correlations",
		"investigate_resource",
		"trace_route_callers",
		"list_relationship_edges",
	} {
		if _, handled := impactRoute(tool, map[string]any{}); handled {
			t.Errorf("impactRoute(%s) handled = true, want false", tool)
		}
		got, err := resolveRoute(tool, map[string]any{})
		if err != nil {
			t.Errorf("resolveRoute(%s) error = %v, want nil", tool, err)
			continue
		}
		if got == nil {
			t.Errorf("resolveRoute(%s) = nil, want a route", tool)
		}
	}

	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestImpactRouteRejectsNonFamilyTools mutation-proves the child selector
// through the adapter: each owned name is claimed, and near-miss names are
// not.
func TestImpactRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for _, tool := range impactRouteTools {
		if _, handled := impactRoute(tool, map[string]any{}); !handled {
			t.Errorf("impactRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
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
		"not_a_tool",
	} {
		if _, handled := impactRoute(tool, map[string]any{}); handled {
			t.Errorf("impactRoute(%q) handled = true, want false", tool)
		}
	}
}
