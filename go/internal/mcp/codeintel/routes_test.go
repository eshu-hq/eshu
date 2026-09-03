// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeinteltools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyPaths pins the eight owned tool names to their internal paths,
// literally, so the ownership test cannot drift with the selector's own
// table.
var familyPaths = map[string]string{
	"find_code":                  "/api/v0/code/search",
	"find_symbol":                "/api/v0/code/symbols/search",
	"inspect_code_inventory":     "/api/v0/code/structure/inventory",
	"inspect_call_graph_metrics": "/api/v0/code/call-graph/metrics",
	"trace_route_callers":        "/api/v0/code/routes/callers",
	"investigate_code_topic":     "/api/v0/code/topics/investigate",
	"execute_language_query":     "/api/v0/code/language-query",
	"find_function_call_chain":   "/api/v0/code/call-chain",
}

func TestRouteOwnsExactlyTheCodeIntelFamily(t *testing.T) {
	t.Parallel()

	for tool, wantPath := range familyPaths {
		request, handled := Route(tool, routecontract.Arguments{})
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tool)
		}
		if request.Method != "POST" {
			t.Errorf("Route(%s) method = %q, want POST", tool, request.Method)
		}
		if request.Path != wantPath {
			t.Errorf("Route(%s) path = %q, want %q", tool, request.Path, wantPath)
		}
		if request.Query != nil {
			t.Errorf("Route(%s) query = %#v, want nil", tool, request.Query)
		}
	}

	for _, tool := range []string{
		"",
		"search_entity_content",
		"search_file_content",
		"investigate_import_dependencies",
		"find_dead_code",
		"find_cross_repo_dead_code",
		"analyze_code_relationships",
		"execute_cypher_query",
		"FIND_CODE",
		"find_code_extra",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryCodeIntelBodyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			tool: "find_code",
			args: routecontract.Arguments{
				"query": "auth", "repo_id": "repo-1", "language": "go",
				"limit": float64(10), "exact": true,
			},
			want: map[string]any{
				"query": "auth", "repo_id": "repo-1", "language": "go",
				"limit": 10, "exact": true,
			},
		},
		{
			tool: "find_symbol",
			args: routecontract.Arguments{
				"symbol": "MyFunc", "match_mode": "exact", "repo_id": "repo-1",
				"language": "go", "entity_type": "function",
				"entity_types": []any{"function", "method"},
				"limit":        float64(25), "offset": float64(5),
			},
			want: map[string]any{
				"symbol": "MyFunc", "match_mode": "exact", "repo_id": "repo-1",
				"language": "go", "entity_type": "function",
				"entity_types": []any{"function", "method"},
				"limit":        25, "offset": 5,
			},
		},
		{
			tool: "inspect_code_inventory",
			args: routecontract.Arguments{
				"repo_id": "repo-1", "language": "python", "inventory_kind": "decorated",
				"entity_kind": "function", "file_path": "src/app.py", "symbol": "handler",
				"decorator": "route", "method_name": "handler", "class_name": "App",
				"limit": float64(12), "offset": float64(24),
			},
			want: map[string]any{
				"repo_id": "repo-1", "language": "python", "inventory_kind": "decorated",
				"entity_kind": "function", "file_path": "src/app.py", "symbol": "handler",
				"decorator": "route", "method_name": "handler", "class_name": "App",
				"limit": 12, "offset": 24,
			},
		},
		{
			tool: "inspect_call_graph_metrics",
			args: routecontract.Arguments{
				"metric_type": "hub_functions", "repo_id": "repo-1", "language": "go",
				"limit": float64(10), "offset": float64(5),
			},
			want: map[string]any{
				"metric_type": "hub_functions", "repo_id": "repo-1", "language": "go",
				"limit": 10, "offset": 5,
			},
		},
		{
			tool: "trace_route_callers",
			args: routecontract.Arguments{
				"repo_id": "repo-payments", "service_id": "svc-1", "service_name": "payments",
				"method": "get", "path": "/payments/{id}", "max_depth": float64(3), "limit": float64(25),
			},
			want: map[string]any{
				"repo_id": "repo-payments", "service_id": "svc-1", "service_name": "payments",
				"method": "get", "path": "/payments/{id}", "max_depth": 3, "limit": 25,
			},
		},
		{
			tool: "investigate_code_topic",
			args: routecontract.Arguments{
				"topic": "repo sync authentication", "intent": "explain_auth_flow",
				"repo_id": "repo-1", "language": "go", "limit": float64(25), "offset": float64(50),
			},
			want: map[string]any{
				"topic": "repo sync authentication", "intent": "explain_auth_flow",
				"repo_id": "repo-1", "language": "go", "limit": 25, "offset": 50,
			},
		},
		{
			tool: "execute_language_query",
			args: routecontract.Arguments{
				"language": "go", "entity_type": "function", "query": "(func_declaration)",
				"repo_id": "repo-1", "limit": float64(50),
			},
			want: map[string]any{
				"language": "go", "entity_type": "function", "query": "(func_declaration)",
				"repo_id": "repo-1", "limit": 50,
			},
		},
		{
			tool: "find_function_call_chain",
			args: routecontract.Arguments{
				"start": "main", "end": "handler", "repo_id": "repo-1", "cross_repo": true,
				"start_repo_id": "repo-1", "end_repo_id": "repo-2",
				"start_entity_id": "entity-1", "end_entity_id": "entity-2", "max_depth": float64(7),
			},
			want: map[string]any{
				"start": "main", "end": "handler", "repo_id": "repo-1", "cross_repo": true,
				"start_repo_id": "repo-1", "end_repo_id": "repo-2",
				"start_entity_id": "entity-1", "end_entity_id": "entity-2", "max_depth": 7,
			},
		},
	}

	for _, tt := range cases {
		request, handled := Route(tt.tool, tt.args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tt.tool, request.Body)
		}
		if !reflect.DeepEqual(body, tt.want) {
			t.Errorf("Route(%s) body = %#v, want %#v", tt.tool, body, tt.want)
		}
	}
}

// codeIntelDefaults pins the fallback each tool substitutes for an absent
// numeric argument, matching the values the root switch sent before the
// extraction.
var codeIntelDefaults = map[string]map[string]int{
	"find_code":                  {"limit": 10},
	"find_symbol":                {"limit": 25, "offset": 0},
	"inspect_code_inventory":     {"limit": 25, "offset": 0},
	"inspect_call_graph_metrics": {"limit": 25, "offset": 0},
	"trace_route_callers":        {"max_depth": 2, "limit": 25},
	"investigate_code_topic":     {"limit": 25, "offset": 0},
	"execute_language_query":     {"limit": 50},
	"find_function_call_chain":   {"max_depth": 5},
}

func TestRouteAppliesCodeIntelDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	for tool, defaults := range codeIntelDefaults {
		request, handled := Route(tool, nil)
		if !handled {
			t.Fatalf("Route(%s, nil) handled = false, want true", tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tool, request.Body)
		}
		for key, want := range defaults {
			if got := body[key]; got != want {
				t.Errorf("Route(%s) absent %s -> %#v, want %#v", tool, key, got, want)
			}
		}
	}

	find, _ := Route("find_code", nil)
	findBody := find.Body.(map[string]any)
	if got, present := findBody["repo_id"]; !present || got != "" {
		t.Errorf("find_code absent repo_id -> (%#v, %v), want an explicit empty string", got, present)
	}
	if got, present := findBody["exact"]; !present || got != false {
		t.Errorf("find_code absent exact -> (%#v, %v), want an explicit false", got, present)
	}

	chain, _ := Route("find_function_call_chain", nil)
	chainBody := chain.Body.(map[string]any)
	if got, present := chainBody["cross_repo"]; !present || got != false {
		t.Errorf("find_function_call_chain absent cross_repo -> (%#v, %v), want an explicit false", got, present)
	}
}

func TestRouteCoercesIntegerArguments(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value any
		want  int
	}{
		{name: "int", value: int(9), want: 9},
		{name: "int64", value: int64(11), want: 11},
		{name: "float64", value: float64(13), want: 13},
		{name: "string falls back", value: "17", want: 10},
		{name: "bool falls back", value: true, want: 10},
		{name: "nil falls back", value: nil, want: 10},
	}

	for _, tt := range cases {
		request, _ := Route("find_code", routecontract.Arguments{"limit": tt.value})
		body := request.Body.(map[string]any)
		if got := body["limit"]; got != tt.want {
			t.Errorf("limit %s (%#v) -> %#v, want %d", tt.name, tt.value, got, tt.want)
		}
	}
}

// TestRouteBuildsAFreshBodyMap proves the selected body is not the caller's
// argument map: a probe key written through the body must stay invisible to
// the caller, so a later dispatch cannot see one call's mutation.
func TestRouteBuildsAFreshBodyMap(t *testing.T) {
	t.Parallel()

	for tool := range familyPaths {
		args := routecontract.Arguments{"repo_id": "repo-1"}
		request, _ := Route(tool, args)
		body := request.Body.(map[string]any)
		body["probe"] = "written-through-body"
		if _, leaked := args["probe"]; leaked {
			t.Errorf("Route(%s) body aliases the caller's argument map", tool)
		}
		if got := args["repo_id"]; got != "repo-1" {
			t.Errorf("Route(%s) mutated the caller's arguments: repo_id = %#v", tool, got)
		}
	}
}
