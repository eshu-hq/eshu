// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Coercion and aliasing coverage for the impact-analysis route selector.
// Split from routes_test.go to keep each file under the 500-line cap; the
// shared populatedRouteCases and defaultBodies fixtures live there and are
// visible here because both files are package impact.
func TestRouteAppliesImpactCoercions(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		limit any
		want  int
	}{
		{limit: 25, want: 25},
		{limit: int64(26), want: 26},
		{limit: 27.9, want: 27},
		{limit: -3.9, want: -3},
		{limit: 0, want: 0},
		{limit: 500, want: 500},
		{limit: "25", want: 50},
		{limit: true, want: 50},
		{limit: nil, want: 50},
		{limit: float32(25), want: 50},
	} {
		request, handled := Route("find_blast_radius", routecontract.Arguments{"limit": tt.limit})
		if !handled {
			t.Fatal("Route(find_blast_radius) handled = false, want true")
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("body type = %T, want map[string]any", request.Body)
		}
		if got := body["limit"]; got != tt.want {
			t.Errorf("limit=%#v -> %#v, want %d", tt.limit, got, tt.want)
		}
	}

	for _, value := range []any{"yes", 1, nil, []bool{false}} {
		request, _ := Route("trace_deployment_chain", routecontract.Arguments{"direct_only": value})
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("body type = %T, want map[string]any", request.Body)
		}
		if got := body["direct_only"]; got != true {
			t.Errorf("direct_only=%#v -> %#v, want the default true", value, got)
		}
	}

	for _, value := range []any{42, nil, true, []string{"prod"}, struct{}{}} {
		request, _ := Route("trace_resource_to_code", routecontract.Arguments{"start": value})
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("body type = %T, want map[string]any", request.Body)
		}
		if got := body["start"]; got != "" {
			t.Errorf("start=%#v -> %#v, want empty", value, got)
		}
	}

	// changed_paths follows StringSlice: a []string is widened to []any, a
	// wrong-typed value travels as nil, never as a formatted Go value.
	request, _ := Route("investigate_change_surface", routecontract.Arguments{"changed_paths": []string{"a.tf"}})
	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("body type = %T, want map[string]any", request.Body)
	}
	if got, want := body["changed_paths"], []any{"a.tf"}; !reflect.DeepEqual(got, want) {
		t.Errorf("changed_paths=[]string -> %#v, want %#v", got, want)
	}
	request, _ = Route("investigate_change_surface", routecontract.Arguments{"changed_paths": "a.tf"})
	body, ok = request.Body.(map[string]any)
	if !ok {
		t.Fatalf("body type = %T, want map[string]any", request.Body)
	}
	if got, want := body["changed_paths"], []any(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("changed_paths=string -> %#v, want the typed nil slice %#v", got, want)
	}
}

// TestRouteDoesNotAliasCallerImpactArguments proves the eight selecting
// builders hand back fresh body maps, while explain_dependency_path is
// pinned to its pre-extraction behavior of forwarding the caller's map
// itself.
func TestRouteDoesNotAliasCallerImpactArguments(t *testing.T) {
	t.Parallel()

	for _, tt := range populatedRouteCases {
		args := routecontract.Arguments{"service_name": "checkout", "limit": float64(25)}
		first, handled := Route(tt.tool, args)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", tt.tool)
		}
		firstBody, ok := first.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tt.tool, first.Body)
		}
		firstBody["service_name"] = "mutated"
		if got := args["service_name"]; got != "checkout" {
			t.Fatalf("Route(%s) mutated caller arguments through the returned body: %#v", tt.tool, got)
		}
		if len(args) != 2 {
			t.Fatalf("Route(%s) grew caller arguments to %d keys, want 2", tt.tool, len(args))
		}

		second, _ := Route(tt.tool, args)
		secondBody, ok := second.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tt.tool, second.Body)
		}
		if got := secondBody["service_name"]; got == "mutated" {
			t.Fatalf("Route(%s) shares a body map between calls", tt.tool)
		}
	}

	args := routecontract.Arguments{"from": "svc-a", "to": "svc-b"}
	request, handled := Route("explain_dependency_path", args)
	if !handled {
		t.Fatal("Route(explain_dependency_path) handled = false, want true")
	}
	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("body type = %T, want map[string]any", request.Body)
	}
	body["from"] = "mutated"
	if got := args["from"]; got != "mutated" {
		t.Fatalf("explain_dependency_path body no longer aliases the caller's map: args[from] = %#v; this pass-through is the pre-extraction contract and changing it changes the wire body", got)
	}
}
