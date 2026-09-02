// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	codequalitytools "github.com/eshu-hq/eshu/go/internal/mcp/codequality"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// codeQualityRouteTools maps every tool the child package owns to the path it
// must select, pinned literally so this file stays independent of the child's
// own table.
var codeQualityRouteTools = map[string]string{
	"calculate_cyclomatic_complexity": "/api/v0/code/complexity",
	"find_most_complex_functions":     "/api/v0/code/complexity",
	"inspect_code_quality":            "/api/v0/code/quality/inspect",
}

// codeQualityBodyKeys lists the body keys each route must still send through
// dispatch, per tool, so a dropped or misspelled key fails here even if the
// child and the parity test drift together. calculate_cyclomatic_complexity's
// entity_id is conditional and pinned separately below.
var codeQualityBodyKeys = map[string][]string{
	"calculate_cyclomatic_complexity": {"function_name", "repo_id", "entity_id"},
	"find_most_complex_functions":     {"repo_id", "limit"},
	"inspect_code_quality": {
		"check", "repo_id", "language", "entity_id", "function_name",
		"min_complexity", "min_lines", "min_arguments", "limit", "offset",
	},
}

func TestResolveRouteUsesExactCodeQualityChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"entity_id":      "function:processPayment",
			"function_name":  "processPayment",
			"check":          "argument_count",
			"repo_id":        "repo-1",
			"language":       "go",
			"min_complexity": float64(3),
			"min_lines":      float64(30),
			"min_arguments":  float64(5),
			"limit":          float64(7),
			"offset":         float64(3),
		}},
		{name: "repo only", args: map[string]any{
			"repo_id": "repo-1",
		}},
		{name: "malformed", args: map[string]any{
			"entity_id":      42,
			"function_name":  nil,
			"check":          7,
			"repo_id":        42,
			"language":       nil,
			"min_complexity": "3",
			"limit":          "25",
			"offset":         "3",
		}},
	}

	for tool := range codeQualityRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := codequalitytools.Route(tool, routecontract.Arguments(tt.args))
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

// TestCodeQualityDispatchKeepsEveryBodyKey proves the fields survive the
// adapter boundary on every route, against literal expectations that are
// deliberately independent of the child selector: the parity test above
// builds both of its sides from that selector, so it cannot notice a key the
// child itself dropped or misspelled.
func TestCodeQualityDispatchKeepsEveryBodyKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"entity_id":      "function:processPayment",
		"function_name":  "processPayment",
		"check":          "argument_count",
		"repo_id":        "repo-1",
		"language":       "go",
		"min_complexity": float64(3),
		"min_lines":      float64(30),
		"min_arguments":  float64(5),
		"limit":          float64(7),
		"offset":         float64(3),
	}
	want := map[string]any{
		"entity_id":      "function:processPayment",
		"function_name":  "processPayment",
		"check":          "argument_count",
		"repo_id":        "repo-1",
		"language":       "go",
		"min_complexity": 3,
		"min_lines":      30,
		"min_arguments":  5,
		"limit":          7,
		"offset":         3,
	}

	for tool, wantPath := range codeQualityRouteTools {
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if got.method != "POST" {
			t.Errorf("%s method = %q, want POST", tool, got.method)
		}
		if got.path != wantPath {
			t.Errorf("%s path = %q, want %q", tool, got.path, wantPath)
		}
		if got.query != nil {
			t.Errorf("%s query = %#v, want nil", tool, got.query)
		}
		body, ok := got.body.(map[string]any)
		if !ok {
			t.Fatalf("%s body type = %T, want map[string]any", tool, got.body)
		}
		keys := codeQualityBodyKeys[tool]
		if n, wantN := len(body), len(keys); n != wantN {
			t.Fatalf("%s body carries %d keys (%#v), want %d", tool, n, body, wantN)
		}
		for _, key := range keys {
			value, present := body[key]
			if !present {
				t.Errorf("%s dispatch dropped %q entirely", tool, key)
				continue
			}
			if !reflect.DeepEqual(value, want[key]) {
				t.Errorf("%s body[%s] = %#v, want %#v", tool, key, value, want[key])
			}
		}
	}

	// The defaults reach the handler unchanged when the caller sends nothing:
	// limit 10 matches both handlers' own substitute for a nonpositive limit,
	// offset 0 is the first page, the min_* thresholds travel as 0 so the
	// handler resolves its check-specific defaults, and the unset string
	// filters still travel as explicit empty strings.
	bare, err := resolveRoute("inspect_code_quality", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(bare) error = %v, want nil", err)
	}
	bareBody := bare.body.(map[string]any)
	if value := bareBody["limit"]; value != 10 {
		t.Errorf("absent limit -> %#v, want the default 10", value)
	}
	for _, key := range []string{"min_complexity", "min_lines", "min_arguments", "offset"} {
		if value := bareBody[key]; value != 0 {
			t.Errorf("absent %s -> %#v, want 0", key, value)
		}
	}
	for _, key := range []string{"check", "repo_id", "language", "entity_id", "function_name"} {
		if value, present := bareBody[key]; !present || value != "" {
			t.Errorf("absent %s -> (%#v, %v), want an explicit empty string", key, value, present)
		}
	}

	// calculate_cyclomatic_complexity keeps its two conditional shapes through
	// the adapter: entity_id is present only when non-empty, and no limit key
	// is ever sent, so a blank-selector call reaches the handler's own
	// list-mode default page.
	calc, err := resolveRoute("calculate_cyclomatic_complexity", map[string]any{
		"function_name": "search",
		"repo_id":       "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute(calc bare) error = %v, want nil", err)
	}
	calcBody := calc.body.(map[string]any)
	if value, present := calcBody["entity_id"]; present {
		t.Errorf("blank entity_id -> present %#v, want the key absent", value)
	}
	if _, present := calcBody["limit"]; present {
		t.Errorf("calculate_cyclomatic_complexity sends limit %#v, want no limit key", calcBody["limit"])
	}
}

// TestResolveRouteStillOwnsItsArmsAfterCodeQuality proves the delegation
// added ahead of the switch claims only this family and leaves every
// neighbouring code, IaC, dead-code, and content arm answered as before.
func TestResolveRouteStillOwnsItsArmsAfterCodeQuality(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"find_code",
		"find_symbol",
		"inspect_code_inventory",
		"execute_language_query",
		"find_function_call_chain",
		"inspect_call_graph_metrics",
		"investigate_code_topic",
		"investigate_hardcoded_secrets",
		"find_dead_code",
		"find_dead_iac",
		"execute_cypher_query",
		"visualize_graph_query",
		"search_registry_bundles",
		"dispatch_taint_path",
		"get_file_content",
		"search_file_content",
	} {
		if _, handled := codeQualityRoute(tool, map[string]any{}); handled {
			t.Errorf("codeQualityRoute(%s) handled = true, want false", tool)
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

	// resolveRoute still reports an unknown tool as an error, not a nil route.
	if _, err := resolveRoute("not_a_tool", map[string]any{}); err == nil {
		t.Fatal("resolveRoute(not_a_tool) error = nil, want an unknown-tool error")
	}
}

// TestCodeQualityRouteRejectsNonFamilyTools mutation-proves the child
// selector through the adapter: the three owned names are claimed, and
// near-miss names are not.
func TestCodeQualityRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for tool := range codeQualityRouteTools {
		if _, handled := codeQualityRoute(tool, map[string]any{}); !handled {
			t.Errorf("codeQualityRoute(%s) handled = false, want true", tool)
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
		"CALCULATE_CYCLOMATIC_COMPLEXITY",
	} {
		if _, handled := codeQualityRoute(tool, map[string]any{}); handled {
			t.Errorf("codeQualityRoute(%q) handled = true, want false", tool)
		}
	}
}
