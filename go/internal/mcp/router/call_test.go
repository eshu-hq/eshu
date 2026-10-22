// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package router

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestCallRefusesRecursiveTargets(t *testing.T) {
	t.Parallel()

	for _, target := range []string{FindToolName, CallToolName, "ask"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			d := &recordingDispatcher{}
			out := newFixtureEngine(t, orderRanker{}).Call(sharedContext(), fixtureRequest(d),
				map[string]any{"name": target, "arguments": map[string]any{"intent": "x", "question": "q", "name": "find_code", "arguments": map[string]any{}}})
			if out.Dispatched || !out.Result.IsError {
				t.Fatalf("Call(%s) = %+v, want a refusal", target, out)
			}
			if code := errorCode(t, out.Result); code != "mcp_router_refused" {
				t.Fatalf("Call(%s) code = %q, want mcp_router_refused", target, code)
			}
			if len(d.calls) != 0 {
				t.Fatalf("dispatcher called %v, want none", d.calls)
			}
		})
	}
}

func TestCallUnknownToolSuggestsClosestNames(t *testing.T) {
	t.Parallel()

	d := &recordingDispatcher{}
	out := newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(d),
		map[string]any{"name": "find_cod", "arguments": map[string]any{}})
	if out.Dispatched || errorCode(t, out.Result) != "mcp_router_unknown_tool" {
		t.Fatalf("Call(find_cod) = %+v, want mcp_router_unknown_tool", out)
	}
	details := structured(t, out.Result)["error"].(map[string]any)["details"].(map[string]any)
	suggestions, _ := details["suggestions"].([]any)
	if len(suggestions) == 0 || suggestions[0] != "find_code" {
		t.Fatalf("suggestions = %v, want find_code first", suggestions)
	}
	if slices.Contains(suggestions, any("ask")) {
		t.Fatalf("suggestions = %v, must not suggest a refused target", suggestions)
	}
	if len(d.calls) != 0 {
		t.Fatalf("dispatcher called %v, want none", d.calls)
	}
}

func TestCallInvalidArgumentsReturnsSchemaAndExample(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]any{
		"missing required": {"limit": 5},
		"wrong type":       {"query": 7},
		"out of range":     {"query": "x", "limit": 51},
		"unknown enum":     {"query": "x", "mode": "regex"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := &recordingDispatcher{}
			out := newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(d),
				map[string]any{"name": "find_code", "arguments": args})
			if out.Dispatched || !out.Result.IsError {
				t.Fatalf("Call(%v) = %+v, want invalid arguments", args, out)
			}
			errObj := structured(t, out.Result)["error"].(map[string]any)
			if errObj["code"] != "mcp_router_invalid_arguments" {
				t.Fatalf("code = %v, want mcp_router_invalid_arguments", errObj["code"])
			}
			details := errObj["details"].(map[string]any)
			if fields, _ := details["field_errors"].([]any); len(fields) == 0 {
				t.Fatalf("details = %v, want field_errors", details)
			}
			if _, ok := details["input_schema"].(map[string]any); !ok {
				t.Fatalf("details = %v, want input_schema", details)
			}
			example, _ := details["example"].(map[string]any)
			if example["name"] != "find_code" {
				t.Fatalf("example = %v, want a find_code call", example)
			}
			if exampleArgs, _ := example["arguments"].(map[string]any); exampleArgs["query"] == nil {
				t.Fatalf("example arguments = %v, want query", example["arguments"])
			}
			if len(d.calls) != 0 {
				t.Fatalf("dispatcher called %v, want none", d.calls)
			}
		})
	}
}

func TestCallScopedTokenRefusesSharedKeyOnlyTool(t *testing.T) {
	t.Parallel()

	d := &recordingDispatcher{}
	out := newFixtureEngine(t, orderRanker{}).Call(scopedContext(), fixtureRequest(d),
		map[string]any{"name": "execute_cypher_query", "arguments": map[string]any{"cypher_query": "MATCH (n) RETURN n LIMIT 1"}})
	if out.Dispatched || errorCode(t, out.Result) != "mcp_router_refused" {
		t.Fatalf("scoped Call(execute_cypher_query) = %+v, want mcp_router_refused", out)
	}
	if len(d.calls) != 0 {
		t.Fatalf("dispatcher called %v, want none", d.calls)
	}
}

func TestCallDispatchesValidArgumentsUnchanged(t *testing.T) {
	t.Parallel()

	sentinel := &struct{ marker string }{"dispatch-result"}
	d := &recordingDispatcher{value: sentinel}
	args := map[string]any{"query": "handler", "limit": 5, "extra": nil}
	out := newFixtureEngine(t, orderRanker{}).Call(scopedContext(), fixtureRequest(d),
		map[string]any{"name": "find_code", "arguments": args})
	if !out.Dispatched || out.Err != nil || out.Target != "find_code" {
		t.Fatalf("Call(find_code) = %+v, want dispatched", out)
	}
	if out.Value != any(sentinel) {
		t.Fatalf("Value = %v, want the dispatcher's own result", out.Value)
	}
	if len(d.args) != 1 || reflect.ValueOf(d.args[0]).Pointer() != reflect.ValueOf(args).Pointer() {
		t.Fatalf("dispatcher args = %v, want the caller's map passed through untouched", d.args)
	}
	if len(args) != 3 {
		t.Fatalf("arguments mutated: %v", args)
	}
}

func TestCallPassesDispatchErrorsThrough(t *testing.T) {
	t.Parallel()

	boom := errors.New("upstream 500")
	d := &recordingDispatcher{err: boom}
	out := newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(d),
		map[string]any{"name": "get_repo_summary", "arguments": map[string]any{}})
	if !out.Dispatched || !errors.Is(out.Err, boom) {
		t.Fatalf("Call() = %+v, want the dispatch error passed through", out)
	}
}

func TestCallReachesProfileHiddenTools(t *testing.T) {
	t.Parallel()

	// A direct tools/call reaches a profile-hidden tool; call_tool must too.
	d := &recordingDispatcher{}
	out := newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(d),
		map[string]any{"name": "find_divergent_code", "arguments": map[string]any{}})
	if !out.Dispatched {
		t.Fatalf("Call(find_divergent_code) = %+v, want dispatched", out)
	}
}

func TestCallRejectsMalformedCallToolArguments(t *testing.T) {
	t.Parallel()

	for name, args := range map[string]map[string]any{
		"missing name":          {"arguments": map[string]any{}},
		"missing arguments":     {"name": "find_code"},
		"arguments not object":  {"name": "find_code", "arguments": "query=x"},
		"name not string":       {"name": 3, "arguments": map[string]any{}},
		"nil call_tool payload": nil,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := &recordingDispatcher{}
			out := newFixtureEngine(t, orderRanker{}).Call(context.Background(), fixtureRequest(d), args)
			if out.Dispatched || errorCode(t, out.Result) != "mcp_router_invalid_arguments" {
				t.Fatalf("Call(%v) = %+v, want mcp_router_invalid_arguments", args, out)
			}
		})
	}
}
