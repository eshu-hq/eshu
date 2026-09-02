// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	deadcodetools "github.com/eshu-hq/eshu/go/internal/mcp/deadcode"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// deadCodeRouteTools maps every tool the child package owns to the path it
// must select, pinned literally so this file stays independent of the child's
// own table.
var deadCodeRouteTools = map[string]string{
	"find_dead_code":            "/api/v0/code/dead-code",
	"investigate_dead_code":     "/api/v0/code/dead-code/investigate",
	"find_cross_repo_dead_code": "/api/v0/code/dead-code/cross-repo",
}

// deadCodeBodyKeys lists the body keys each route must still send through
// dispatch, per tool, so a dropped or misspelled key fails here even if the
// child and the parity test drift together.
var deadCodeBodyKeys = map[string][]string{
	"find_dead_code":            {"repo_id", "limit", "exclude_decorated_with"},
	"investigate_dead_code":     {"repo_id", "language", "limit", "offset", "exclude_decorated_with"},
	"find_cross_repo_dead_code": {"repo_id", "consumer_repo_ids", "language", "limit", "exclude_decorated_with"},
}

func TestResolveRouteUsesExactDeadCodeChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"repo_id":                "repo-1",
			"language":               "python",
			"limit":                  float64(7),
			"offset":                 float64(3),
			"consumer_repo_ids":      []any{"repo-consumer"},
			"exclude_decorated_with": []any{"deprecated", "celery.task"},
		}},
		{name: "repo only", args: map[string]any{
			"repo_id": "repo-1",
		}},
		{name: "malformed", args: map[string]any{
			"repo_id":                42,
			"language":               nil,
			"limit":                  "25",
			"offset":                 "3",
			"consumer_repo_ids":      "repo-consumer",
			"exclude_decorated_with": "deprecated",
		}},
	}

	for tool := range deadCodeRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := deadcodetools.Route(tool, routecontract.Arguments(tt.args))
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

// TestDeadCodeDispatchKeepsEveryBodyKey proves the fields survive the adapter
// boundary on every route, against literal expectations that are deliberately
// independent of the child selector: the parity test above builds both of its
// sides from that selector, so it cannot notice a key the child itself
// dropped or misspelled.
func TestDeadCodeDispatchKeepsEveryBodyKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"repo_id":                "repo-1",
		"language":               "go",
		"limit":                  float64(7),
		"offset":                 float64(3),
		"consumer_repo_ids":      []any{"repo-consumer", "", 7, "repo-consumer-2"},
		"exclude_decorated_with": []any{"deprecated", "celery.task"},
	}
	want := map[string]any{
		"repo_id":                "repo-1",
		"language":               "go",
		"limit":                  7,
		"offset":                 3,
		"consumer_repo_ids":      []string{"repo-consumer", "repo-consumer-2"},
		"exclude_decorated_with": []any{"deprecated", "celery.task"},
	}

	for tool, wantPath := range deadCodeRouteTools {
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
		keys := deadCodeBodyKeys[tool]
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
	// limit 100 matches the handler's own substitute for a nonpositive limit,
	// offset 0 is the first page, and the unset string filters still travel
	// as explicit empty strings.
	bare, err := resolveRoute("investigate_dead_code", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(bare) error = %v, want nil", err)
	}
	bareBody := bare.body.(map[string]any)
	if value := bareBody["limit"]; value != 100 {
		t.Errorf("absent limit -> %#v, want the default 100", value)
	}
	if value := bareBody["offset"]; value != 0 {
		t.Errorf("absent offset -> %#v, want 0", value)
	}
	for _, key := range []string{"repo_id", "language"} {
		if value, present := bareBody[key]; !present || value != "" {
			t.Errorf("absent %s -> (%#v, %v), want an explicit empty string", key, value, present)
		}
	}

	// The two list arguments keep their opposite absent shapes on the wire:
	// exclude_decorated_with is a nil []any (JSON null) while
	// consumer_repo_ids is a non-nil empty []string (JSON []). Both are
	// length zero, so the assertions pin nil-ness, not len.
	if slice, ok := bareBody["exclude_decorated_with"].([]any); !ok || slice != nil {
		t.Errorf("absent exclusions -> %#v, want a nil []any", bareBody["exclude_decorated_with"])
	}
	cross, err := resolveRoute("find_cross_repo_dead_code", map[string]any{})
	if err != nil {
		t.Fatalf("resolveRoute(cross bare) error = %v, want nil", err)
	}
	crossBody := cross.body.(map[string]any)
	consumers, ok := crossBody["consumer_repo_ids"].([]string)
	if !ok || consumers == nil || len(consumers) != 0 {
		t.Errorf("absent consumer_repo_ids -> %#v (%T), want a non-nil empty []string",
			crossBody["consumer_repo_ids"], crossBody["consumer_repo_ids"])
	}
}

// TestResolveRouteStillOwnsItsArmsAfterDeadCode proves the delegation added
// ahead of the switch claims only this family and leaves every neighbouring
// code, IaC, relationship, and content arm answered as before.
func TestResolveRouteStillOwnsItsArmsAfterDeadCode(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"find_code",
		"find_symbol",
		"execute_language_query",
		"find_function_call_chain",
		"inspect_call_graph_metrics",
		"investigate_hardcoded_secrets",
		"find_dead_iac",
		"calculate_cyclomatic_complexity",
		"inspect_code_quality",
		"dispatch_taint_path",
		"get_file_content",
		"search_file_content",
	} {
		if _, handled := deadCodeRoute(tool, map[string]any{}); handled {
			t.Errorf("deadCodeRoute(%s) handled = true, want false", tool)
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

// TestDeadCodeRouteRejectsNonFamilyTools mutation-proves the child selector
// through the adapter: the three owned names are claimed, and near-miss names
// are not.
func TestDeadCodeRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for tool := range deadCodeRouteTools {
		if _, handled := deadCodeRoute(tool, map[string]any{}); !handled {
			t.Errorf("deadCodeRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
		"find_dead",
		"find_dead_codes",
		"find_dead_code_extra",
		"dead_code",
		"find_dead_iac",
		"investigate_dead",
		"find_cross_repo_dead",
		"analyze_code_relationships",
		"FIND_DEAD_CODE",
	} {
		if _, handled := deadCodeRoute(tool, map[string]any{}); handled {
			t.Errorf("deadCodeRoute(%q) handled = true, want false", tool)
		}
	}
}
