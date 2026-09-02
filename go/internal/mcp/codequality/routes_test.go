// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequalitytools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyPaths pins the three owned tool names to their internal paths,
// literally, so the ownership test cannot drift with the selector's own
// table.
var familyPaths = map[string]string{
	"calculate_cyclomatic_complexity": "/api/v0/code/complexity",
	"find_most_complex_functions":     "/api/v0/code/complexity",
	"inspect_code_quality":            "/api/v0/code/quality/inspect",
}

func TestRouteOwnsExactlyTheComplexityQualityFamily(t *testing.T) {
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
		"calculate_cyclomatic",
		"calculate_cyclomatic_complexity_extra",
		"cyclomatic_complexity",
		"find_most_complex",
		"find_most_complex_function",
		"inspect_code_quality_extra",
		"code_quality",
		"inspect_code_inventory",
		"find_dead_code",
		"CALCULATE_CYCLOMATIC_COMPLEXITY",
	} {
		if _, handled := Route(tool, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true, want false", tool)
		}
	}
}

func TestRouteCarriesEveryComplexityQualityBodyKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		tool string
		args routecontract.Arguments
		want map[string]any
	}{
		{
			tool: "calculate_cyclomatic_complexity",
			args: routecontract.Arguments{
				"entity_id":     "function:processPayment",
				"function_name": "processPayment",
				"repo_id":       "repo-1",
			},
			want: map[string]any{
				"entity_id":     "function:processPayment",
				"function_name": "processPayment",
				"repo_id":       "repo-1",
			},
		},
		{
			tool: "find_most_complex_functions",
			args: routecontract.Arguments{
				"repo_id": "repo-1",
				"limit":   float64(7),
			},
			want: map[string]any{
				"repo_id": "repo-1",
				"limit":   7,
			},
		},
		{
			tool: "inspect_code_quality",
			args: routecontract.Arguments{
				"check":          "argument_count",
				"repo_id":        "repo-payments",
				"language":       "go",
				"entity_id":      "function:handler",
				"function_name":  "handler",
				"min_complexity": float64(3),
				"min_lines":      float64(30),
				"min_arguments":  float64(5),
				"limit":          float64(25),
				"offset":         float64(50),
			},
			want: map[string]any{
				"check":          "argument_count",
				"repo_id":        "repo-payments",
				"language":       "go",
				"entity_id":      "function:handler",
				"function_name":  "handler",
				"min_complexity": 3,
				"min_lines":      30,
				"min_arguments":  5,
				"limit":          25,
				"offset":         50,
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

func TestRouteAppliesComplexityQualityDefaultsForAbsentArguments(t *testing.T) {
	t.Parallel()

	for tool := range familyPaths {
		request, handled := Route(tool, nil)
		if !handled {
			t.Fatalf("Route(%s, nil) handled = false, want true", tool)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("Route(%s) body type = %T, want map[string]any", tool, request.Body)
		}
		if got, present := body["repo_id"]; !present || got != "" {
			t.Errorf("%s absent repo_id -> (%#v, %v), want an explicit empty string", tool, got, present)
		}
	}

	// calculate_cyclomatic_complexity sends no limit key at all; the handler
	// resolves its own list default when both selectors are blank.
	calc, _ := Route("calculate_cyclomatic_complexity", nil)
	calcBody := calc.Body.(map[string]any)
	if _, present := calcBody["limit"]; present {
		t.Errorf("calculate_cyclomatic_complexity sends limit %#v, want no limit key", calcBody["limit"])
	}
	if got, present := calcBody["function_name"]; !present || got != "" {
		t.Errorf("absent function_name -> (%#v, %v), want an explicit empty string", got, present)
	}

	// The two paged tools default limit to 10, the same value both handlers
	// substitute for a nonpositive limit before clamping above 100.
	for _, tool := range []string{"find_most_complex_functions", "inspect_code_quality"} {
		request, _ := Route(tool, nil)
		body := request.Body.(map[string]any)
		if got := body["limit"]; got != 10 {
			t.Errorf("%s absent limit -> %#v, want the handler-matching default 10", tool, got)
		}
	}

	// The quality thresholds travel as 0 so the handler resolves its own
	// check-specific defaults; offset 0 is the first page.
	quality, _ := Route("inspect_code_quality", nil)
	qualityBody := quality.Body.(map[string]any)
	for _, key := range []string{"min_complexity", "min_lines", "min_arguments", "offset"} {
		if got := qualityBody[key]; got != 0 {
			t.Errorf("absent %s -> %#v, want 0 so the handler resolves its own default", key, got)
		}
	}
	for _, key := range []string{"check", "language", "entity_id", "function_name"} {
		if got, present := qualityBody[key]; !present || got != "" {
			t.Errorf("absent %s -> (%#v, %v), want an explicit empty string", key, got, present)
		}
	}
}

// TestRouteSendsEntityIDOnlyWhenNonEmpty pins the one conditional key in the
// family: calculate_cyclomatic_complexity's body carries entity_id only when
// the caller supplied a non-empty string. Absent, empty, and wrong-typed
// values all leave the key out entirely — the key's absence is the pinned
// wire shape, not an empty value.
func TestRouteSendsEntityIDOnlyWhenNonEmpty(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args routecontract.Arguments
		want bool
	}{
		{name: "absent", args: routecontract.Arguments{"function_name": "f"}, want: false},
		{name: "empty", args: routecontract.Arguments{"entity_id": ""}, want: false},
		{name: "wrong type", args: routecontract.Arguments{"entity_id": 42}, want: false},
		{name: "populated", args: routecontract.Arguments{"entity_id": "function:f"}, want: true},
	}

	for _, tt := range cases {
		request, _ := Route("calculate_cyclomatic_complexity", tt.args)
		body := request.Body.(map[string]any)
		value, present := body["entity_id"]
		if present != tt.want {
			t.Errorf("%s: entity_id present = %v (%#v), want %v", tt.name, present, value, tt.want)
		}
		if tt.want && value != "function:f" {
			t.Errorf("%s: entity_id = %#v, want %q", tt.name, value, "function:f")
		}
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
		request, _ := Route("inspect_code_quality", routecontract.Arguments{"limit": tt.value})
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
