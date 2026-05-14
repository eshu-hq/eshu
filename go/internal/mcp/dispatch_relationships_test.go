package mcp

import "testing"

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

func TestResolveRouteKeepsGenericAnalyzeCodeRelationshipsFallback(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "class_hierarchy",
		"target":     "PaymentProcessor",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	body := requireRouteBody(t, route)
	if got, want := body["entity_id"], "PaymentProcessor"; got != want {
		t.Fatalf("body[entity_id] = %#v, want %#v", got, want)
	}
	if got, want := body["query_type"], "class_hierarchy"; got != want {
		t.Fatalf("body[query_type] = %#v, want %#v", got, want)
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

func requireRouteBody(t *testing.T, route *route) map[string]any {
	t.Helper()

	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	return body
}
