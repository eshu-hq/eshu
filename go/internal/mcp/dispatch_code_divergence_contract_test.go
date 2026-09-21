// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	codedivergencetools "github.com/eshu-hq/eshu/go/internal/mcp/code/divergence"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

// codeDivergenceRouteTools maps every tool the child package owns to the
// path it must select, pinned literally so this file stays independent of
// the child's own table.
var codeDivergenceRouteTools = map[string]string{
	"find_code_divergence":        "/api/v0/code/divergence/findings",
	"investigate_code_divergence": "/api/v0/code/divergence/investigate",
}

// codeDivergenceBodyKeys lists the body keys each route must still send
// through dispatch, per tool, so a dropped or misspelled key fails here
// even if the child and the parity test drift together.
var codeDivergenceBodyKeys = map[string][]string{
	"find_code_divergence":        {"repo_id", "kind", "limit", "offset", "include_tests"},
	"investigate_code_divergence": {"repo_id", "kind", "fingerprint", "include_tests"},
}

func TestResolveRouteUsesExactCodeDivergenceChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"repo_id":       "repo-1",
			"kind":          "exact",
			"fingerprint":   "fp-1",
			"limit":         float64(7),
			"offset":        float64(3),
			"include_tests": true,
		}},
		{name: "repo only", args: map[string]any{
			"repo_id": "repo-1",
		}},
		{name: "malformed", args: map[string]any{
			"repo_id":       42,
			"kind":          7,
			"fingerprint":   nil,
			"limit":         "25",
			"offset":        "3",
			"include_tests": "yes",
		}},
	}

	for tool := range codeDivergenceRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := codedivergencetools.Route(tool, routecontract.Arguments(tt.args))
			if !handled {
				t.Fatalf("child Route(%s) handled = false, want true", tool)
			}
			want := &routecontract.Request{
				Method: request.Method,
				Path:   request.Path,
				Body:   request.Body,
				Query:  request.Query,
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolveRoute(%s, %s) = %#v, want child request %#v", tool, tt.name, got, want)
			}
		}
	}
}

// TestCodeDivergenceDispatchKeepsEveryBodyKey proves the fields survive the
// adapter boundary on every route, against literal expectations that are
// deliberately independent of the child selector.
func TestCodeDivergenceDispatchKeepsEveryBodyKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"repo_id":       "repo-1",
		"kind":          "exact",
		"fingerprint":   "fp-1",
		"limit":         float64(7),
		"offset":        float64(3),
		"include_tests": true,
	}
	want := map[string]any{
		"repo_id":       "repo-1",
		"kind":          "exact",
		"fingerprint":   "fp-1",
		"limit":         7,
		"offset":        3,
		"include_tests": true,
	}

	for tool, wantPath := range codeDivergenceRouteTools {
		got, err := resolveRoute(tool, args)
		if err != nil {
			t.Fatalf("resolveRoute(%s) error = %v, want nil", tool, err)
		}
		if got.Method != "POST" {
			t.Errorf("%s method = %q, want POST", tool, got.Method)
		}
		if got.Path != wantPath {
			t.Errorf("%s path = %q, want %q", tool, got.Path, wantPath)
		}
		if got.Query != nil {
			t.Errorf("%s query = %#v, want nil", tool, got.Query)
		}
		body, ok := got.Body.(map[string]any)
		if !ok {
			t.Fatalf("%s body type = %T, want map[string]any", tool, got.Body)
		}
		keys := codeDivergenceBodyKeys[tool]
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

	// The defaults reach the handler unchanged when the caller sends
	// nothing: limit 25 matches the handler's own substitute for a
	// nonpositive limit, offset 0 is the first page, kind travels blank
	// for the all-families read, and include_tests travels false so test
	// files suppress by default.
	empty, err := resolveRoute("find_code_divergence", map[string]any{"repo_id": "repo-1"})
	if err != nil {
		t.Fatalf("resolveRoute defaults error = %v, want nil", err)
	}
	emptyBody := empty.Body.(map[string]any)
	for key, wantValue := range map[string]any{
		"limit": 25, "offset": 0, "kind": "", "include_tests": false,
	} {
		if emptyBody[key] != wantValue {
			t.Errorf("default body[%s] = %#v, want %#v", key, emptyBody[key], wantValue)
		}
	}
}
