// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtimedeclarationtools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/route"
)

func TestRouteOwnsExactlyTheDeclarationTool(t *testing.T) {
	t.Parallel()

	request, handled := Route("list_runtime_version_declarations", routecontract.Arguments{})
	if !handled {
		t.Fatal("Route(list_runtime_version_declarations) handled = false, want true")
	}
	if request.Method != "POST" || request.Path != "/api/v0/runtime-declarations/query" {
		t.Errorf("request = %s %s", request.Method, request.Path)
	}
	for _, name := range []string{
		"list_runtime_declarations", "list_runtime_version_declaration", "list_runtime_version_declarations_extra",
		"get_runtime_version_declarations", "list_codeowners_ownership", "get_repo_context",
		"LIST_RUNTIME_VERSION_DECLARATIONS", "",
	} {
		if request, handled := Route(name, routecontract.Arguments{}); handled {
			t.Errorf("Route(%q) handled = true (%#v), want false", name, request)
		}
	}
}

func TestRouteForwardsEveryArgumentUnselected(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{
		"runtime": "node", "major_below": float64(20), "repo_ids": []any{"repository:r_a"},
		"limit": float64(10), "cursor": "abc",
		// A misspelled filter and a mistyped value must reach the handler,
		// which rejects them; dropping either would answer with every row.
		"majorbelow": float64(20), "source_kind": float64(7),
	}
	request, _ := Route(ToolName, args)
	want := map[string]any{
		"runtime": "node", "major_below": float64(20), "repo_ids": []any{"repository:r_a"},
		"limit": float64(10), "cursor": "abc", "majorbelow": float64(20), "source_kind": float64(7),
	}
	if !reflect.DeepEqual(request.Body, want) {
		t.Errorf("body = %#v, want %#v", request.Body, want)
	}
}

func TestRouteBodyIsACopy(t *testing.T) {
	t.Parallel()

	args := routecontract.Arguments{"runtime": "node"}
	first, _ := Route(ToolName, args)
	first.Body.(map[string]any)["runtime"] = "mutated"
	second, _ := Route(ToolName, args)
	if second.Body.(map[string]any)["runtime"] != "node" {
		t.Error("a caller's mutation of one request body leaked into the next")
	}
}
