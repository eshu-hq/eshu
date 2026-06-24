// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestResolveRouteMapsFindFunctionCallChainExactSelectors(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_function_call_chain", map[string]any{
		"start":           "wrapper",
		"end":             "helper",
		"repo_id":         "repo-1",
		"start_entity_id": "entity:start",
		"end_entity_id":   "entity:end",
		"max_depth":       float64(4),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/call-chain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	if got, want := body["start"], "wrapper"; got != want {
		t.Fatalf("body[start] = %#v, want %#v", got, want)
	}
	if got, want := body["end"], "helper"; got != want {
		t.Fatalf("body[end] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["start_entity_id"], "entity:start"; got != want {
		t.Fatalf("body[start_entity_id] = %#v, want %#v", got, want)
	}
	if got, want := body["end_entity_id"], "entity:end"; got != want {
		t.Fatalf("body[end_entity_id] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 4; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsFindFunctionCallChainExactSelectorsWithoutNames(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_function_call_chain", map[string]any{
		"repo_id":         "repo-1",
		"start_entity_id": "entity:start",
		"end_entity_id":   "entity:end",
		"max_depth":       float64(4),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/call-chain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	if got, want := body["start"], ""; got != want {
		t.Fatalf("body[start] = %#v, want %#v", got, want)
	}
	if got, want := body["end"], ""; got != want {
		t.Fatalf("body[end] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["start_entity_id"], "entity:start"; got != want {
		t.Fatalf("body[start_entity_id] = %#v, want %#v", got, want)
	}
	if got, want := body["end_entity_id"], "entity:end"; got != want {
		t.Fatalf("body[end_entity_id] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 4; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}
