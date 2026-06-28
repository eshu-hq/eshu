// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "testing"

func TestAnalyzeCodeRelationshipsSchemaAdvertisesCrossRepoQueriesAndSelectors(t *testing.T) {
	t.Parallel()

	schema := analyzeCodeRelationshipsSchema()
	properties := schema["properties"].(map[string]any)
	queryType := properties["query_type"].(map[string]any)
	enums := queryType["enum"].([]string)
	for _, value := range []string{
		"find_cross_repo_callers",
		"find_cross_repo_callees",
		"find_cross_repo_importers",
		"cross_repo_class_hierarchy",
		"cross_repo_overrides",
		"find_cross_repo_call_chain",
	} {
		if !stringSliceContains(enums, value) {
			t.Fatalf("query_type enum missing %q in %#v", value, enums)
		}
	}
	for _, field := range []string{"cross_repo", "start_repo_id", "end_repo_id"} {
		if _, ok := properties[field]; !ok {
			t.Fatalf("schema properties missing %q", field)
		}
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCrossRepoCallersToOptInStory(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_cross_repo_callers",
		"target":     "chargeCard",
		"repo_id":    "billing",
		"limit":      13,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/relationships/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	if got, want := body["cross_repo"], true; got != want {
		t.Fatalf("body[cross_repo] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "incoming"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "CALLS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "billing"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 13; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCrossRepoImportersAndInheritance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		queryType        string
		wantDirection    string
		wantRelationship string
		wantQueryType    string
	}{
		{
			name:             "importers",
			queryType:        "find_cross_repo_importers",
			wantDirection:    "incoming",
			wantRelationship: "IMPORTS",
		},
		{
			name:             "class hierarchy",
			queryType:        "cross_repo_class_hierarchy",
			wantDirection:    "both",
			wantRelationship: "INHERITS",
		},
		{
			name:             "overrides",
			queryType:        "cross_repo_overrides",
			wantDirection:    "both",
			wantRelationship: "OVERRIDES",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			route, err := resolveRoute("analyze_code_relationships", map[string]any{
				"query_type": tt.queryType,
				"target":     "PaymentProcessor",
				"repo_id":    "payments",
			})
			if err != nil {
				t.Fatalf("resolveRoute() error = %v, want nil", err)
			}
			body := requireRouteBody(t, route)
			if got, want := route.path, "/api/v0/code/relationships/story"; got != want {
				t.Fatalf("route.path = %q, want %q", got, want)
			}
			if got, want := body["cross_repo"], true; got != want {
				t.Fatalf("body[cross_repo] = %#v, want %#v", got, want)
			}
			if got, want := body["direction"], tt.wantDirection; got != want {
				t.Fatalf("body[direction] = %#v, want %#v", got, want)
			}
			if got, want := body["relationship_type"], tt.wantRelationship; got != want {
				t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
			}
			if tt.wantQueryType != "" {
				if got, want := body["query_type"], tt.wantQueryType; got != want {
					t.Fatalf("body[query_type] = %#v, want %#v", got, want)
				}
			}
		})
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCrossRepoCallChain(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type":      "find_cross_repo_call_chain",
		"start_entity_id": "entity:api-handler",
		"end_entity_id":   "entity:billing-charge",
		"start_repo_id":   "api",
		"end_repo_id":     "billing",
		"max_depth":       6,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/call-chain"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body := requireRouteBody(t, route)
	for key, want := range map[string]any{
		"cross_repo":      true,
		"start_entity_id": "entity:api-handler",
		"end_entity_id":   "entity:billing-charge",
		"start_repo_id":   "api",
		"end_repo_id":     "billing",
		"max_depth":       6,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}
