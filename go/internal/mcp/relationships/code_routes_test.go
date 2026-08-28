// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationshiptools

import (
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

func TestCodeRouteDirectStoryPreservesDefaultsAndCoercions(t *testing.T) {
	t.Parallel()

	request, handled, err := CodeRoute("get_code_relationship_story", routecontract.Arguments{
		"target":             "checkout",
		"entity_id":          "entity:checkout",
		"repo_id":            "repo-1",
		"language":           "go",
		"relationship_type":  "CALLS",
		"relationship_types": []string{"CALLS", "IMPORTS"},
		"direction":          "both",
		"include_transitive": true,
		"max_depth":          int64(9),
		"limit":              float64(19.8),
		"offset":             int64(3),
		"token_budget":       float64(1200),
		"cross_repo":         true,
		"min_confidence":     float32(0.75),
	})
	if err != nil || !handled {
		t.Fatalf("CodeRoute() = (_, %v, %v), want handled without error", handled, err)
	}
	want := routecontract.Request{
		Method: "POST",
		Path:   "/api/v0/code/relationships/story",
		Body: map[string]any{
			"target":             "checkout",
			"entity_id":          "entity:checkout",
			"repo_id":            "repo-1",
			"language":           "go",
			"relationship_type":  "CALLS",
			"relationship_types": []any{"CALLS", "IMPORTS"},
			"direction":          "both",
			"include_transitive": true,
			"max_depth":          9,
			"limit":              19,
			"offset":             3,
			"token_budget":       1200,
			"cross_repo":         true,
			"min_confidence":     0.75,
		},
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("CodeRoute() = %#v, want %#v", request, want)
	}

	request, handled, err = CodeRoute("get_code_relationship_story", nil)
	if err != nil || !handled {
		t.Fatalf("CodeRoute(defaults) = (_, %v, %v), want handled without error", handled, err)
	}
	body := requireRequestBody(t, request)
	for key, wantValue := range map[string]any{
		"target": "", "entity_id": "", "repo_id": "", "language": "",
		"relationship_type": "", "direction": "", "include_transitive": false,
		"max_depth": 5, "limit": 25, "offset": 0, "token_budget": 0, "cross_repo": false,
	} {
		if got := body[key]; got != wantValue {
			t.Errorf("default body[%s] = %#v, want %#v", key, got, wantValue)
		}
	}
	if got, want := body["relationship_types"], []any(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("default body[relationship_types] = %#v, want typed nil []any", got)
	}
	if _, ok := body["min_confidence"]; ok {
		t.Errorf("default body[min_confidence] = %#v, want absent", body["min_confidence"])
	}
}

func TestCodeRouteAnalysisStoryMappings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		queryType        string
		direction        string
		relationshipType string
		transitive       bool
		crossRepo        bool
	}{
		{queryType: "find_callers", direction: "incoming", relationshipType: "CALLS"},
		{queryType: "find_callees", direction: "outgoing", relationshipType: "CALLS"},
		{queryType: "find_all_callers", direction: "incoming", relationshipType: "CALLS", transitive: true},
		{queryType: "find_all_callees", direction: "outgoing", relationshipType: "CALLS", transitive: true},
		{queryType: "find_cross_repo_callers", direction: "incoming", relationshipType: "CALLS", crossRepo: true},
		{queryType: "find_cross_repo_callees", direction: "outgoing", relationshipType: "CALLS", crossRepo: true},
		{queryType: "find_importers", direction: "incoming", relationshipType: "IMPORTS"},
		{queryType: "find_cross_repo_importers", direction: "incoming", relationshipType: "IMPORTS", crossRepo: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.queryType, func(t *testing.T) {
			t.Parallel()

			request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
				"query_type": test.queryType,
				"target":     "checkout",
			})
			if err != nil || !handled {
				t.Fatalf("CodeRoute() = (_, %v, %v), want handled without error", handled, err)
			}
			if request.Path != "/api/v0/code/relationships/story" {
				t.Fatalf("request.Path = %q, want story path", request.Path)
			}
			body := requireRequestBody(t, request)
			for key, want := range map[string]any{
				"direction": test.direction, "relationship_type": test.relationshipType,
				"include_transitive": test.transitive, "cross_repo": test.crossRepo,
			} {
				if got := body[key]; got != want {
					t.Errorf("body[%s] = %#v, want %#v", key, got, want)
				}
			}
		})
	}
}

func TestCodeRouteTypedAndCrossRepoStoryShapesRemainDistinct(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		queryType        string
		relationshipType string
	}{
		{queryType: "class_hierarchy", relationshipType: "INHERITS"},
		{queryType: "overrides", relationshipType: "OVERRIDES"},
	} {
		request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
			"query_type": test.queryType,
			"target":     "PaymentProcessor",
			"language":   "go",
		})
		if err != nil || !handled {
			t.Fatalf("CodeRoute(%s) = (_, %v, %v), want handled without error", test.queryType, handled, err)
		}
		body := requireRequestBody(t, request)
		for key, want := range map[string]any{
			"query_type": test.queryType, "language": "go", "direction": "both",
			"relationship_type": test.relationshipType, "cross_repo": false,
		} {
			if got := body[key]; got != want {
				t.Errorf("%s body[%s] = %#v, want %#v", test.queryType, key, got, want)
			}
		}
		for _, absent := range []string{"relationship_types", "include_transitive"} {
			if _, ok := body[absent]; ok {
				t.Errorf("%s body[%s] present, want absent", test.queryType, absent)
			}
		}
	}

	for _, test := range []struct {
		queryType        string
		relationshipType string
	}{
		{queryType: "cross_repo_class_hierarchy", relationshipType: "INHERITS"},
		{queryType: "cross_repo_overrides", relationshipType: "OVERRIDES"},
	} {
		request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
			"query_type":         test.queryType,
			"language":           "go",
			"relationship_types": []any{"CUSTOM"},
		})
		if err != nil || !handled {
			t.Fatalf("CodeRoute(%s) = (_, %v, %v), want handled without error", test.queryType, handled, err)
		}
		body := requireRequestBody(t, request)
		if _, ok := body["query_type"]; ok {
			t.Errorf("%s body[query_type] present, want absent", test.queryType)
		}
		if _, ok := body["language"]; ok {
			t.Errorf("%s body[language] present, want absent", test.queryType)
		}
		for key, want := range map[string]any{
			"relationship_type": test.relationshipType, "include_transitive": false, "cross_repo": true,
		} {
			if got := body[key]; got != want {
				t.Errorf("%s body[%s] = %#v, want %#v", test.queryType, key, got, want)
			}
		}
		if got, want := body["relationship_types"], []any{"CUSTOM"}; !reflect.DeepEqual(got, want) {
			t.Errorf("%s body[relationship_types] = %#v, want %#v", test.queryType, got, want)
		}
	}
}

func TestCodeRouteGenericFallbackPreservesQueryType(t *testing.T) {
	t.Parallel()

	for _, queryType := range []string{"", "unknown_query"} {
		request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
			"query_type": queryType,
			"target":     "entity-1",
		})
		if err != nil || !handled {
			t.Fatalf("CodeRoute(%q) = (_, %v, %v), want handled without error", queryType, handled, err)
		}
		want := routecontract.Request{Method: "POST", Path: "/api/v0/code/relationships", Body: map[string]any{
			"entity_id": "entity-1", "query_type": queryType,
		}}
		if !reflect.DeepEqual(request, want) {
			t.Errorf("CodeRoute(%q) = %#v, want %#v", queryType, request, want)
		}
	}
}

func TestCodeRouteCallChainErrorsStayClaimedWithZeroRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      routecontract.Arguments
		wantError string
	}{
		{
			name:      "target missing arrow",
			args:      routecontract.Arguments{"query_type": "call_chain", "target": "missing-arrow"},
			wantError: "call_chain target must use start->end format",
		},
		{
			name:      "missing endpoint and selector",
			args:      routecontract.Arguments{"query_type": "call_chain", "target": "start->"},
			wantError: "call_chain target must use start->end format or provide start_entity_id and end_entity_id",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request, handled, err := CodeRoute("analyze_code_relationships", test.args)
			if !handled {
				t.Fatal("CodeRoute() handled = false, want true")
			}
			if err == nil || err.Error() != test.wantError {
				t.Fatalf("CodeRoute() error = %v, want %q", err, test.wantError)
			}
			if !reflect.DeepEqual(request, routecontract.Request{}) {
				t.Fatalf("CodeRoute() request = %#v, want zero request", request)
			}
		})
	}
}

func TestCodeRouteCallChainUsesFirstArrowAndEntityIDFallback(t *testing.T) {
	t.Parallel()

	request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
		"query_type":      "call_chain",
		"target":          "  start  ->  middle -> end  ",
		"repo_id":         "repo-1",
		"start_entity_id": "entity:start",
		"end_entity_id":   "entity:end",
		"context":         " 8 ",
	})
	if err != nil || !handled {
		t.Fatalf("CodeRoute() = (_, %v, %v), want handled without error", handled, err)
	}
	body := requireRequestBody(t, request)
	for key, want := range map[string]any{
		"start": "start", "end": "middle -> end", "repo_id": "repo-1",
		"start_entity_id": "entity:start", "end_entity_id": "entity:end", "max_depth": 8,
	} {
		if got := body[key]; got != want {
			t.Errorf("body[%s] = %#v, want %#v", key, got, want)
		}
	}

	request, handled, err = CodeRoute("analyze_code_relationships", routecontract.Arguments{
		"query_type":      "call_chain",
		"target":          " -> named-end ",
		"start_entity_id": "entity:start",
	})
	if err != nil || !handled {
		t.Fatalf("CodeRoute(entity fallback) = (_, %v, %v), want handled without error", handled, err)
	}
	body = requireRequestBody(t, request)
	if got := body["start"]; got != "" {
		t.Errorf("body[start] = %#v, want empty", got)
	}
	if got := body["end"]; got != "named-end" {
		t.Errorf("body[end] = %#v, want named-end", got)
	}
}

func TestCodeRouteCrossRepoScopeIsCaseInsensitiveButUntrimmed(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		scope string
		want  bool
	}{
		{scope: "CrOsS_RePo", want: true},
		{scope: " cross_repo ", want: false},
	} {
		request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
			"query_type": "find_callers",
			"scope":      test.scope,
		})
		if err != nil || !handled {
			t.Fatalf("CodeRoute(%q) = (_, %v, %v), want handled without error", test.scope, handled, err)
		}
		if got := requireRequestBody(t, request)["cross_repo"]; got != test.want {
			t.Errorf("scope %q cross_repo = %#v, want %v", test.scope, got, test.want)
		}
	}
}

func TestCodeRouteAnalyzeMaxDepthKeepsNarrowCoercion(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		args routecontract.Arguments
		want int
	}{
		{name: "int", args: routecontract.Arguments{"max_depth": 4}, want: 4},
		{name: "float64 truncates", args: routecontract.Arguments{"max_depth": float64(4.9)}, want: 4},
		{name: "int64 ignored", args: routecontract.Arguments{"max_depth": int64(9)}, want: 5},
		{name: "float32 ignored", args: routecontract.Arguments{"max_depth": float32(9)}, want: 5},
		{name: "context fallback", args: routecontract.Arguments{"max_depth": int64(9), "context": " 7 "}, want: 7},
		{name: "invalid context", args: routecontract.Arguments{"context": "deep"}, want: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			test.args["query_type"] = "find_callers"
			request, handled, err := CodeRoute("analyze_code_relationships", test.args)
			if err != nil || !handled {
				t.Fatalf("CodeRoute() = (_, %v, %v), want handled without error", handled, err)
			}
			if got := requireRequestBody(t, request)["max_depth"]; got != test.want {
				t.Errorf("max_depth = %#v, want %d", got, test.want)
			}
		})
	}
}

func TestCodeRouteStringSliceShapesFlowThroughBody(t *testing.T) {
	t.Parallel()

	mixed := []any{"CALLS", 7, nil}
	request, handled, err := CodeRoute("get_code_relationship_story", routecontract.Arguments{
		"relationship_types": mixed,
	})
	if err != nil || !handled {
		t.Fatalf("CodeRoute(mixed) = (_, %v, %v), want handled without error", handled, err)
	}
	gotMixed := requireRequestBody(t, request)["relationship_types"].([]any)
	gotMixed[0] = "MUTATED"
	if got := mixed[0]; got != "MUTATED" {
		t.Fatalf("relationship_types copy changed aliasing; original[0] = %#v", got)
	}

	var nilStrings []string
	for _, test := range []struct {
		name string
		raw  any
		want []any
	}{
		{name: "strings", raw: []string{"CALLS", "IMPORTS"}, want: []any{"CALLS", "IMPORTS"}},
		{name: "nil strings", raw: nilStrings, want: []any{}},
		{name: "wrong type", raw: "CALLS", want: []any(nil)},
	} {
		request, handled, err := CodeRoute("get_code_relationship_story", routecontract.Arguments{
			"relationship_types": test.raw,
		})
		if err != nil || !handled {
			t.Fatalf("CodeRoute(%s) = (_, %v, %v), want handled without error", test.name, handled, err)
		}
		got := requireRequestBody(t, request)["relationship_types"]
		if !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s relationship_types = %#v, want %#v", test.name, got, test.want)
		}
	}
}

func TestCodeRouteDeadCodeAndForcedCrossRepoCallChain(t *testing.T) {
	t.Parallel()

	request, handled, err := CodeRoute("analyze_code_relationships", routecontract.Arguments{
		"query_type":             "dead_code",
		"repo_id":                "repo-1",
		"limit":                  int64(12),
		"exclude_decorated_with": []string{"test", "generated"},
	})
	if err != nil || !handled {
		t.Fatalf("CodeRoute(dead_code) = (_, %v, %v), want handled without error", handled, err)
	}
	wantDead := routecontract.Request{Method: "POST", Path: "/api/v0/code/dead-code", Body: map[string]any{
		"repo_id": "repo-1", "limit": 12, "exclude_decorated_with": []any{"test", "generated"},
	}}
	if !reflect.DeepEqual(request, wantDead) {
		t.Fatalf("CodeRoute(dead_code) = %#v, want %#v", request, wantDead)
	}

	request, handled, err = CodeRoute("analyze_code_relationships", routecontract.Arguments{
		"query_type":      "find_cross_repo_call_chain",
		"start_entity_id": "entity:start",
		"end_entity_id":   "entity:end",
	})
	if err != nil || !handled {
		t.Fatalf("CodeRoute(cross-repo call chain) = (_, %v, %v), want handled without error", handled, err)
	}
	if got := requireRequestBody(t, request)["cross_repo"]; got != true {
		t.Fatalf("forced call-chain cross_repo = %#v, want true", got)
	}
}

func TestCodeRouteDoesNotClaimUnrelatedTools(t *testing.T) {
	t.Parallel()

	request, handled, err := CodeRoute("find_code", routecontract.Arguments{"query": "checkout"})
	if err != nil || handled || !reflect.DeepEqual(request, routecontract.Request{}) {
		t.Fatalf("CodeRoute(unrelated) = (%#v, %v, %v), want zero request, false, nil", request, handled, err)
	}
}

func requireRequestBody(t *testing.T, request routecontract.Request) map[string]any {
	t.Helper()

	body, ok := request.Body.(map[string]any)
	if !ok {
		t.Fatalf("request.Body type = %T, want map[string]any", request.Body)
	}
	return body
}
