// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"reflect"
	"testing"
)

func TestCodeRelationshipRouteClaimsOnlyFamilyTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		toolName    string
		args        map[string]any
		wantPath    string
		wantBody    map[string]any
		wantHandled bool
		wantError   string
	}{
		{
			name:     "relationship story",
			toolName: "get_code_relationship_story",
			args: map[string]any{
				"target":             "checkout",
				"entity_id":          "entity:checkout",
				"repo_id":            "repo-1",
				"language":           "go",
				"relationship_type":  "CALLS",
				"relationship_types": []any{"CALLS", "IMPORTS"},
				"direction":          "both",
				"include_transitive": true,
				"max_depth":          4,
				"limit":              19,
				"offset":             3,
				"token_budget":       1200,
				"cross_repo":         true,
				"min_confidence":     0.75,
			},
			wantPath: "/api/v0/code/relationships/story",
			wantBody: map[string]any{
				"target":             "checkout",
				"entity_id":          "entity:checkout",
				"repo_id":            "repo-1",
				"language":           "go",
				"relationship_type":  "CALLS",
				"relationship_types": []any{"CALLS", "IMPORTS"},
				"direction":          "both",
				"include_transitive": true,
				"max_depth":          4,
				"limit":              19,
				"offset":             3,
				"token_budget":       1200,
				"cross_repo":         true,
				"min_confidence":     0.75,
			},
			wantHandled: true,
		},
		{
			name:     "relationship analysis",
			toolName: "analyze_code_relationships",
			args: map[string]any{
				"query_type":         "find_callers",
				"target":             "charge",
				"repo_id":            "repo-2",
				"relationship_types": []any{"CALLS"},
				"limit":              11,
			},
			wantPath: "/api/v0/code/relationships/story",
			wantBody: map[string]any{
				"target":             "charge",
				"repo_id":            "repo-2",
				"direction":          "incoming",
				"relationship_type":  "CALLS",
				"relationship_types": []any{"CALLS"},
				"include_transitive": false,
				"max_depth":          5,
				"limit":              11,
				"offset":             0,
				"token_budget":       0,
				"cross_repo":         false,
			},
			wantHandled: true,
		},
		{
			name:        "invalid call chain stays claimed",
			toolName:    "analyze_code_relationships",
			args:        map[string]any{"query_type": "call_chain", "target": "missing-arrow"},
			wantHandled: true,
			wantError:   "call_chain target must use start->end format",
		},
		{
			name:     "unrelated tool",
			toolName: "find_code",
			args:     map[string]any{"query": "checkout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotRoute, gotHandled, err := codeRelationshipRoute(tt.toolName, tt.args)
			if gotHandled != tt.wantHandled {
				t.Fatalf("codeRelationshipRoute() handled = %v, want %v", gotHandled, tt.wantHandled)
			}
			if tt.wantError != "" {
				if err == nil || err.Error() != tt.wantError {
					t.Fatalf("codeRelationshipRoute() error = %v, want %q", err, tt.wantError)
				}
				if gotRoute != nil {
					t.Fatalf("codeRelationshipRoute() route = %#v, want nil on error", gotRoute)
				}
				return
			}
			if err != nil {
				t.Fatalf("codeRelationshipRoute() error = %v, want nil", err)
			}
			if !tt.wantHandled {
				if gotRoute != nil {
					t.Fatalf("codeRelationshipRoute() route = %#v, want nil for unrelated tool", gotRoute)
				}
				return
			}
			if got, want := gotRoute.method, "POST"; got != want {
				t.Fatalf("route.method = %q, want %q", got, want)
			}
			if gotRoute.path != tt.wantPath {
				t.Fatalf("route.path = %q, want %q", gotRoute.path, tt.wantPath)
			}
			gotBody := requireRouteBody(t, gotRoute)
			if !reflect.DeepEqual(gotBody, tt.wantBody) {
				t.Fatalf("route.body = %#v, want %#v", gotBody, tt.wantBody)
			}
		})
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCallersToStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_callers",
		"target":     "helper",
		"repo_id":    "repo-1",
		"limit":      float64(17),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships/story" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships/story", route.path)
	}
	body := requireRouteBody(t, route)
	if got, want := body["target"], "helper"; got != want {
		t.Fatalf("body[target] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "incoming"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "CALLS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 17; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsAllCallersToStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_all_callers",
		"target":     "helper",
		"context":    "7",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships/story" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships/story", route.path)
	}
	body := requireRouteBody(t, route)
	if got, want := body["target"], "helper"; got != want {
		t.Fatalf("body[target] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "incoming"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "CALLS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["include_transitive"], true; got != want {
		t.Fatalf("body[include_transitive] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 7; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsAllCalleesToStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_all_callees",
		"target":     "wrapper",
		"context":    "6",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships/story" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships/story", route.path)
	}
	body := requireRouteBody(t, route)
	if got, want := body["target"], "wrapper"; got != want {
		t.Fatalf("body[target] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "outgoing"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "CALLS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["include_transitive"], true; got != want {
		t.Fatalf("body[include_transitive] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 6; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsImportersToStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_importers",
		"target":     "payments",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships/story" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships/story", route.path)
	}
	body := requireRouteBody(t, route)
	if got, want := body["target"], "payments"; got != want {
		t.Fatalf("body[target] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "incoming"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "IMPORTS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsClassHierarchyToStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "class_hierarchy",
		"target":     "PaymentProcessor",
		"repo_id":    "repo-1",
		"language":   "go",
		"max_depth":  4,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/relationships/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	if got, want := body["target"], "PaymentProcessor"; got != want {
		t.Fatalf("body[target] = %#v, want %#v", got, want)
	}
	if got, want := body["query_type"], "class_hierarchy"; got != want {
		t.Fatalf("body[query_type] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "INHERITS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["language"], "go"; got != want {
		t.Fatalf("body[language] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 4; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsOverridesToStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "overrides",
		"repo_id":    "repo-1",
		"limit":      50,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/relationships/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	if got, want := body["query_type"], "overrides"; got != want {
		t.Fatalf("body[query_type] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "OVERRIDES"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 50; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCallChain(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "call_chain",
		"target":     "wrapper->helper",
		"context":    "7",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/call-chain" {
		t.Fatalf("route.path = %q, want /api/v0/code/call-chain", route.path)
	}
	body := requireRouteBody(t, route)
	if got, want := body["start"], "wrapper"; got != want {
		t.Fatalf("body[start] = %#v, want %#v", got, want)
	}
	if got, want := body["end"], "helper"; got != want {
		t.Fatalf("body[end] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 7; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCallChainExactSelectors(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type":      "call_chain",
		"target":          "wrapper->helper",
		"repo_id":         "repo-1",
		"start_entity_id": "entity:start",
		"end_entity_id":   "entity:end",
		"context":         "7",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/call-chain" {
		t.Fatalf("route.path = %q, want /api/v0/code/call-chain", route.path)
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
	if got, want := body["max_depth"], 7; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCallChainExactSelectorsWithoutTarget(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type":      "call_chain",
		"repo_id":         "repo-1",
		"start_entity_id": "entity:start",
		"end_entity_id":   "entity:end",
		"context":         "7",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/call-chain" {
		t.Fatalf("route.path = %q, want /api/v0/code/call-chain", route.path)
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
	if got, want := body["max_depth"], 7; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func requireRouteBody(t *testing.T, route *route) map[string]any {
	t.Helper()

	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	return body
}
