// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeflowtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// familyPaths pins the four names this package owns and the path each must
// select. The paths are literal here, independent of routes.go's own map, so
// a swapped or retargeted path fails this table rather than moving both sides
// of a comparison.
var familyPaths = map[string]string{
	"dispatch_taint_path":   "/api/v0/code/flow/taint-path",
	"dispatch_reaching_def": "/api/v0/code/flow/reaching-def",
	"dispatch_cfg_summary":  "/api/v0/code/flow/cfg-summary",
	"dispatch_pdg_summary":  "/api/v0/code/flow/pdg-summary",
}

// flowBodyKeys is the exact six-key body every code-flow request sends. The
// handler requires repo_id and reads the rest by name, so the count and the
// spelling of each key are pinned rather than left to the request comparison
// alone.
var flowBodyKeys = []string{
	"repo_id",
	"language",
	"symbol",
	"file_path",
	"line",
	"limit",
}

// populatedArguments gives every key a distinct value, so two keys swapped in
// the request builder fail the exact comparison below instead of passing on a
// shared value.
var populatedArguments = routecontract.Arguments{
	"repo_id":      "repo-1",
	"language":     "go",
	"symbol":       "handleCheckout",
	"file_path":    "internal/checkout/handler.go",
	"line":         float64(41),
	"limit":        float64(7),
	"unused_decoy": "ignored",
}

// wantPopulatedBody is the body the populated arguments must select on every
// route. Both integers travel as Go ints inside the JSON body, not strings.
var wantPopulatedBody = map[string]any{
	"repo_id":   "repo-1",
	"language":  "go",
	"symbol":    "handleCheckout",
	"file_path": "internal/checkout/handler.go",
	"line":      41,
	"limit":     7,
}

func TestRouteOwnsExactlyTheCodeFlowFamily(t *testing.T) {
	t.Parallel()

	for toolName, wantPath := range familyPaths {
		request, handled := Route(toolName, routecontract.Arguments{})
		if !handled {
			t.Errorf("Route(%s) handled = false, want true", toolName)
			continue
		}
		if request.Method != "POST" {
			t.Errorf("Route(%s) method = %q, want POST", toolName, request.Method)
		}
		if request.Path != wantPath {
			t.Errorf("Route(%s) path = %q, want %q", toolName, request.Path, wantPath)
		}
		if request.Query != nil {
			t.Errorf("Route(%s) query = %#v, want nil", toolName, request.Query)
		}
	}

	// Neighbours in the root resolveRoute chain, the other extracted
	// families, and near-miss names: this package must claim none of them.
	for _, toolName := range []string{
		"find_code",
		"find_symbol",
		"execute_language_query",
		"find_function_call_chain",
		"inspect_call_graph_metrics",
		"investigate_hardcoded_secrets",
		"get_code_relationship_story",
		"list_relationship_edges",
		"find_infra_resources",
		"trace_exposure_path",
		"dispatch_taint",
		"dispatch_taint_paths",
		"dispatch_taint_path_extra",
		"taint_path",
		"dispatch_cfg",
		"dispatch_pdg",
		"dispatch_reaching_defs",
		"dispatch_code_flow",
		"DISPATCH_TAINT_PATH",
		"",
		"not_a_tool",
	} {
		if request, handled := Route(toolName, routecontract.Arguments{}); handled {
			t.Errorf("Route(%s) handled = true (%#v), want false", toolName, request)
		}
	}
}

func TestRoutePreservesCodeFlowRequestContract(t *testing.T) {
	t.Parallel()

	for toolName, wantPath := range familyPaths {
		request, handled := Route(toolName, populatedArguments)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		want := routecontract.Request{Method: "POST", Path: wantPath, Body: map[string]any{
			"repo_id":   "repo-1",
			"language":  "go",
			"symbol":    "handleCheckout",
			"file_path": "internal/checkout/handler.go",
			"line":      41,
			"limit":     7,
		}}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("Route(%s) = %#v, want %#v", toolName, request, want)
		}
	}
}

// TestRouteCarriesEveryCodeFlowBodyKey pins each of the six keys on its own.
// The exact-request comparison already covers the set, but a per-key assertion
// names the dropped field when one goes missing, and the keys fail differently
// when lost: a dropped repo_id 400s every caller ("repo_id is required"),
// while a dropped language, symbol, file_path, or line silently widens the
// page past the filter the caller named, and a dropped limit never fails at
// all because the handler substitutes its own 25.
func TestRouteCarriesEveryCodeFlowBodyKey(t *testing.T) {
	t.Parallel()

	for toolName := range familyPaths {
		request, handled := Route(toolName, populatedArguments)
		if !handled {
			t.Fatalf("Route(%s) handled = false, want true", toolName)
		}
		body, ok := request.Body.(map[string]any)
		if !ok {
			t.Fatalf("%s body type = %T, want map[string]any", toolName, request.Body)
		}
		if got, want := len(body), len(flowBodyKeys); got != want {
			t.Fatalf("%s body carries %d keys (%#v), want %d", toolName, got, body, want)
		}
		for _, key := range flowBodyKeys {
			value, present := body[key]
			if !present {
				t.Errorf("%s body dropped %q entirely", toolName, key)
				continue
			}
			if want := wantPopulatedBody[key]; value != want {
				t.Errorf("%s body[%s] = %#v, want %#v", toolName, key, value, want)
			}
		}
		for _, key := range []string{"line", "limit"} {
			if _, isInt := body[key].(int); !isInt {
				t.Errorf("%s body[%s] = %#v (%T), want a Go int the handler decodes", toolName, key, body[key], body[key])
			}
		}

		// The code-flow reads have no paging cursor, no offset, and no
		// free-text query, so these must never appear.
		for _, key := range []string{"offset", "cursor", "query", "entity_id", "max_depth"} {
			if value, present := body[key]; present {
				t.Errorf("%s body carries %q = %#v, want the key absent", toolName, key, value)
			}
		}
	}
}

func TestRouteAppliesCodeFlowDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	request, handled := Route("dispatch_taint_path", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(dispatch_taint_path) handled = false, want true")
	}
	body := request.Body.(map[string]any)
	if got := body["limit"]; got != 25 {
		t.Errorf("absent limit -> %#v, want the dispatcher default 25", got)
	}
	if got := body["line"]; got != 0 {
		t.Errorf("absent line -> %#v, want 0, the handler's no-line-filter value", got)
	}

	// Numeric coercions match routecontract.Arguments.IntOr exactly,
	// including float truncation toward zero and the fallback for unsupported
	// types. Out-of-range values are forwarded as-is: the handler, not the
	// selector, substitutes 25 for a nonpositive limit, clamps anything above
	// 100, and floors a negative line to 0.
	for _, key := range []string{"line", "limit"} {
		def := 25
		if key == "line" {
			def = 0
		}
		for _, tt := range []struct {
			value any
			want  int
		}{
			{value: 5, want: 5},
			{value: int64(6), want: 6},
			{value: 7.9, want: 7},
			{value: -3.9, want: -3},
			{value: -7, want: -7},
			{value: 0, want: 0},
			{value: 500, want: 500},
			{value: "5", want: def},
			{value: true, want: def},
			{value: nil, want: def},
			{value: float32(5), want: def},
		} {
			request, _ := Route("dispatch_pdg_summary", routecontract.Arguments{key: tt.value})
			if got := request.Body.(map[string]any)[key]; got != tt.want {
				t.Errorf("%s=%#v -> %#v, want %d", key, tt.value, got, tt.want)
			}
		}
	}

	// Wrong-typed string arguments read as empty, never as a formatted Go
	// value, on every one of the four string keys.
	for _, value := range []any{42, nil, true, []string{"go"}, struct{}{}, []byte("repo-1")} {
		for _, key := range []string{"repo_id", "language", "symbol", "file_path"} {
			request, _ := Route("dispatch_cfg_summary", routecontract.Arguments{key: value})
			if got := request.Body.(map[string]any)[key]; got != "" {
				t.Errorf("%s=%#v -> %#v, want empty", key, value, got)
			}
		}
	}
}

func TestRouteHandlesNilAndTypedNilCodeFlowArguments(t *testing.T) {
	t.Parallel()

	want := routecontract.Request{Method: "POST", Path: "/api/v0/code/flow/reaching-def", Body: map[string]any{
		"repo_id":   "",
		"language":  "",
		"symbol":    "",
		"file_path": "",
		"line":      0,
		"limit":     25,
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
		request, handled := Route("dispatch_reaching_def", tt.args)
		if !handled {
			t.Fatalf("%s: handled = false, want true", tt.name)
		}
		if !reflect.DeepEqual(request, want) {
			t.Fatalf("%s: Route() = %#v, want %#v", tt.name, request, want)
		}
	}
}

func TestRouteDoesNotAliasCallerCodeFlowArguments(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"repo_id": "repo-1", "limit": float64(5)}
	request, handled := Route("dispatch_taint_path", args)
	if !handled {
		t.Fatal("Route(dispatch_taint_path) handled = false, want true")
	}
	request.Body.(map[string]any)["repo_id"] = "mutated"
	if got := args["repo_id"]; got != "repo-1" {
		t.Fatalf("Route mutated caller arguments through the returned body: repo_id = %#v", got)
	}
	if len(args) != 2 {
		t.Fatalf("Route grew caller arguments to %d keys, want 2", len(args))
	}

	// Two calls with the same arguments hand back independent body maps.
	first, _ := Route("dispatch_taint_path", args)
	second, _ := Route("dispatch_taint_path", args)
	first.Body.(map[string]any)["repo_id"] = "mutated"
	if got := second.Body.(map[string]any)["repo_id"]; got != "repo-1" {
		t.Fatalf("Route shares a body map between calls: repo_id = %#v", got)
	}
}
