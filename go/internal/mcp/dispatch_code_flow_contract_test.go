// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"

	codeflowtools "github.com/eshu-hq/eshu/go/internal/mcp/codeflow"
	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// codeFlowRouteTools maps every tool the child package owns to the path it
// must select, pinned literally so this file stays independent of the child's
// own table.
var codeFlowRouteTools = map[string]string{
	"dispatch_taint_path":   "/api/v0/code/flow/taint-path",
	"dispatch_reaching_def": "/api/v0/code/flow/reaching-def",
	"dispatch_cfg_summary":  "/api/v0/code/flow/cfg-summary",
	"dispatch_pdg_summary":  "/api/v0/code/flow/pdg-summary",
}

// codeFlowBodyKeys is the six-key body every code-flow request must still
// send through dispatch.
var codeFlowBodyKeys = []string{
	"repo_id",
	"language",
	"symbol",
	"file_path",
	"line",
	"limit",
}

func TestResolveRouteUsesExactCodeFlowChildRequest(t *testing.T) {
	t.Parallel()

	argumentCases := []struct {
		name string
		args map[string]any
	}{
		{name: "nil", args: nil},
		{name: "empty", args: map[string]any{}},
		{name: "populated", args: map[string]any{
			"repo_id":   "repo-1",
			"language":  "go",
			"symbol":    "handleCheckout",
			"file_path": "internal/checkout/handler.go",
			"line":      float64(41),
			"limit":     float64(7),
		}},
		{name: "repo only", args: map[string]any{
			"repo_id": "repo-1",
		}},
		{name: "malformed", args: map[string]any{
			"repo_id":   42,
			"language":  nil,
			"symbol":    []string{"handle"},
			"file_path": struct{}{},
			"line":      "12",
			"limit":     "25",
		}},
	}

	for tool := range codeFlowRouteTools {
		for _, tt := range argumentCases {
			got, err := resolveRoute(tool, tt.args)
			if err != nil {
				t.Fatalf("resolveRoute(%s, %s) error = %v, want nil", tool, tt.name, err)
			}
			request, handled := codeflowtools.Route(tool, routecontract.Arguments(tt.args))
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

// TestCodeFlowDispatchKeepsEveryBodyKey proves the six fields survive the
// adapter boundary on every one of the four routes, where the handler
// actually decodes them. The literal expectations here are deliberately
// independent of the child selector: the parity test above builds both of its
// sides from that selector, so it cannot notice a key the child itself
// dropped or misspelled.
func TestCodeFlowDispatchKeepsEveryBodyKey(t *testing.T) {
	t.Parallel()

	args := map[string]any{
		"repo_id":   "repo-1",
		"language":  "go",
		"symbol":    "handleCheckout",
		"file_path": "internal/checkout/handler.go",
		"line":      float64(41),
		"limit":     float64(7),
	}
	want := map[string]any{
		"repo_id":   "repo-1",
		"language":  "go",
		"symbol":    "handleCheckout",
		"file_path": "internal/checkout/handler.go",
		"line":      41,
		"limit":     7,
	}

	for tool, wantPath := range codeFlowRouteTools {
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
		if n, wantN := len(body), len(codeFlowBodyKeys); n != wantN {
			t.Fatalf("%s body carries %d keys (%#v), want %d", tool, n, body, wantN)
		}
		for _, key := range codeFlowBodyKeys {
			value, present := body[key]
			if !present {
				t.Errorf("%s dispatch dropped %q entirely", tool, key)
				continue
			}
			if value != want[key] {
				t.Errorf("%s body[%s] = %#v, want %#v", tool, key, value, want[key])
			}
		}
		for _, key := range []string{"offset", "cursor", "query", "entity_id", "max_depth"} {
			if value, present := body[key]; present {
				t.Errorf("%s body carries %q = %#v, want the key absent", tool, key, value)
			}
		}
	}

	// The defaults reach the handler unchanged when the caller omits the two
	// integers, and every unset string filter is still sent as an explicit
	// empty string.
	bare, err := resolveRoute("dispatch_cfg_summary", map[string]any{
		"repo_id": "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute(repo only) error = %v, want nil", err)
	}
	bareBody := bare.body.(map[string]any)
	if value := bareBody["limit"]; value != 25 {
		t.Errorf("absent limit -> %#v, want the default 25", value)
	}
	if value := bareBody["line"]; value != 0 {
		t.Errorf("absent line -> %#v, want 0, the handler's no-line-filter value", value)
	}
	for _, key := range []string{"language", "symbol", "file_path"} {
		if value, present := bareBody[key]; !present || value != "" {
			t.Errorf("absent %s -> (%#v, %v), want an explicit empty string", key, value, present)
		}
	}
}

// TestResolveRouteStillOwnsItsArmsAfterCodeFlow proves the delegation kept at
// the same position in the chain claims only this family and leaves every
// neighbouring code, relationship, and content arm answered as before.
func TestResolveRouteStillOwnsItsArmsAfterCodeFlow(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{
		"find_code",
		"find_symbol",
		"execute_language_query",
		"find_function_call_chain",
		"inspect_call_graph_metrics",
		"investigate_hardcoded_secrets",
		"find_dead_code",
		"get_file_content",
		"search_file_content",
		"find_infra_resources",
	} {
		if _, handled := codeFlowRoute(tool, map[string]any{}); handled {
			t.Errorf("codeFlowRoute(%s) handled = true, want false", tool)
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

// TestCodeFlowRouteRejectsNonFamilyTools mutation-proves the child selector
// through the adapter: the four owned names are claimed, and near-miss names
// are not.
func TestCodeFlowRouteRejectsNonFamilyTools(t *testing.T) {
	t.Parallel()

	for tool := range codeFlowRouteTools {
		if _, handled := codeFlowRoute(tool, map[string]any{}); !handled {
			t.Errorf("codeFlowRoute(%s) handled = false, want true", tool)
		}
	}
	for _, tool := range []string{
		"",
		"dispatch_taint",
		"dispatch_taint_paths",
		"dispatch_taint_path_extra",
		"taint_path",
		"dispatch_cfg",
		"dispatch_pdg",
		"dispatch_reaching_defs",
		"dispatch_code_flow",
		"DISPATCH_TAINT_PATH",
	} {
		if _, handled := codeFlowRoute(tool, map[string]any{}); handled {
			t.Errorf("codeFlowRoute(%q) handled = true, want false", tool)
		}
	}
}
