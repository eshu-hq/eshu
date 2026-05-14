package mcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

func TestResolveRouteMapsResolveEntityQueryToName(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("resolve_entity", map[string]any{
		"query":       "sample-service-api",
		"types":       []any{"workload"},
		"environment": "qa",
		"limit":       float64(5),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/entities/resolve" {
		t.Fatalf("route.path = %q, want /api/v0/entities/resolve", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["name"], "sample-service-api"; got != want {
		t.Fatalf("body[name] = %#v, want %#v", got, want)
	}
	if got, want := body["type"], "workload"; got != want {
		t.Fatalf("body[type] = %#v, want %#v", got, want)
	}
	if _, exists := body["query"]; exists {
		t.Fatalf("body should not contain query, got %#v", body["query"])
	}
	if _, exists := body["types"]; exists {
		t.Fatalf("body should not contain types, got %#v", body["types"])
	}
}

func TestResolveRouteMapsQualifiedServiceIDToServicePath(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_service_context", map[string]any{
		"workload_id": "workload:sample-service-api",
		"environment": "prod",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/services/sample-service-api/context"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	if got, want := route.query["environment"], "prod"; got != want {
		t.Fatalf("route.query[environment] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsRelationshipEvidenceToDrilldownPath(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_relationship_evidence", map[string]any{
		"resolved_id": "resolved/example id",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/evidence/relationships/resolved%2Fexample%20id"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
}

func TestDispatchToolServiceStoryReturnsStructuredEnvelopeData(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v0/services/sample-service-api/story", func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/eshu.envelope+json"; got != want {
			t.Fatalf("Accept = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"service_identity": map[string]any{"service_id": "workload:sample-service-api"},
				"deployment_lanes": []map[string]any{{"lane_type": "k8s_gitops"}},
				"evidence_graph":   map[string]any{"edges": []map[string]any{{"resolved_id": "resolved-gitops"}}},
			},
			"truth": map[string]any{
				"level":      "exact",
				"capability": "platform_impact.context_overview",
				"profile":    "production",
				"basis":      "hybrid",
				"freshness":  map[string]any{"state": "fresh"},
			},
			"error": nil,
		})
	})

	result, err := dispatchTool(
		context.Background(),
		mux,
		"get_service_story",
		map[string]any{"workload_id": "workload:sample-service-api"},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want structured service story envelope")
	}
	if result.Envelope.Data == nil {
		t.Fatal("dispatchTool() envelope data is nil, want service dossier data")
	}
}

func TestResolveRouteMapsPackageRegistryPackagesToBoundedQuery(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_package_registry_packages", map[string]any{
		"package_id": "package:npm:@eshu/core-api",
		"ecosystem":  "npm",
		"name":       "core-api",
		"limit":      float64(25),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/package-registry/packages"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"package_id": "package:npm:@eshu/core-api",
		"ecosystem":  "npm",
		"name":       "core-api",
		"limit":      "25",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestResolveRouteMapsCodeRelationshipStoryToBoundedBody(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("get_code_relationship_story", map[string]any{
		"target":             "process_payment",
		"repo_id":            "repo-1",
		"relationship_type":  "CALLS",
		"direction":          "incoming",
		"include_transitive": true,
		"max_depth":          float64(4),
		"limit":              float64(25),
		"offset":             float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "POST"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/code/relationships/story"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	for key, want := range map[string]any{
		"target":             "process_payment",
		"repo_id":            "repo-1",
		"relationship_type":  "CALLS",
		"direction":          "incoming",
		"include_transitive": true,
		"max_depth":          4,
		"limit":              25,
		"offset":             50,
	} {
		if got := body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestResolveRouteMapsPackageRegistryVersionsToPackageScope(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("list_package_registry_versions", map[string]any{
		"package_id": "package:npm:@eshu/core-api",
		"limit":      float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.method, "GET"; got != want {
		t.Fatalf("route.method = %q, want %q", got, want)
	}
	if got, want := route.path, "/api/v0/package-registry/versions"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	for key, want := range map[string]string{
		"package_id": "package:npm:@eshu/core-api",
		"limit":      "50",
	} {
		if got := route.query[key]; got != want {
			t.Fatalf("route.query[%s] = %#v, want %#v", key, got, want)
		}
	}
}

func TestResolveRouteMapsSearchFileContentPatternAndRepoIDs(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("search_file_content", map[string]any{
		"pattern":  "sample-service-api",
		"repo_ids": []any{"repo://sample-service", "repo://shared"},
		"limit":    float64(25),
		"offset":   float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/content/files/search"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["query"], "sample-service-api"; got != want {
		t.Fatalf("body[query] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 50; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
	repoIDs, ok := body["repo_ids"].([]any)
	if !ok {
		t.Fatalf("body[repo_ids] type = %T, want []any", body["repo_ids"])
	}
	if got, want := len(repoIDs), 2; got != want {
		t.Fatalf("len(body[repo_ids]) = %d, want %d", got, want)
	}
	if _, exists := body["pattern"]; exists {
		t.Fatalf("body should not contain pattern, got %#v", body["pattern"])
	}
}

func TestResolveRouteMapsSearchEntityContentSingleRepoID(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("search_entity_content", map[string]any{
		"pattern":  "sample-service-api",
		"repo_ids": []any{"repo://sample-service"},
		"limit":    float64(10),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["query"], "sample-service-api"; got != want {
		t.Fatalf("body[query] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 10; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 0; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo://sample-service"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if _, exists := body["repo_ids"]; exists {
		t.Fatalf("body should not contain repo_ids, got %#v", body["repo_ids"])
	}
}

func TestSearchContentToolsAdvertisePagingContract(t *testing.T) {
	t.Parallel()

	for _, tool := range contentTools() {
		if tool.Name != "search_file_content" && tool.Name != "search_entity_content" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s InputSchema type = %T, want map", tool.Name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s properties type = %T, want map", tool.Name, schema["properties"])
		}
		for _, field := range []string{"limit", "offset"} {
			if _, ok := properties[field]; !ok {
				t.Fatalf("%s missing %s in InputSchema", tool.Name, field)
			}
		}
	}
}

func TestResolveRouteMapsCalculateCyclomaticComplexityToFunctionName(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("calculate_cyclomatic_complexity", map[string]any{
		"function_name": "search",
		"repo_id":       "repo-1",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/complexity"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["function_name"], "search"; got != want {
		t.Fatalf("body[function_name] = %#v, want %#v", got, want)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if _, exists := body["entity_id"]; exists {
		t.Fatalf("body should not contain entity_id, got %#v", body["entity_id"])
	}
}

func TestResolveRouteMapsFindMostComplexFunctionsWithoutEntitySelector(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_most_complex_functions", map[string]any{
		"repo_id": "repo-1",
		"limit":   float64(7),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if got, want := route.path, "/api/v0/code/complexity"; got != want {
		t.Fatalf("route.path = %q, want %q", got, want)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 7; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if _, exists := body["entity_id"]; exists {
		t.Fatalf("body should not contain entity_id, got %#v", body["entity_id"])
	}
	if _, exists := body["function_name"]; exists {
		t.Fatalf("body should not contain function_name, got %#v", body["function_name"])
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsCallers(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_callers",
		"target":     "helper",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["name"], "helper"; got != want {
		t.Fatalf("body[name] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "incoming"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "CALLS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsAllCallers(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_all_callers",
		"target":     "helper",
		"context":    "7",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["name"], "helper"; got != want {
		t.Fatalf("body[name] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "incoming"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "CALLS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["transitive"], true; got != want {
		t.Fatalf("body[transitive] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 7; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsAllCallees(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_all_callees",
		"target":     "wrapper",
		"context":    "6",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/relationships" {
		t.Fatalf("route.path = %q, want /api/v0/code/relationships", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["name"], "wrapper"; got != want {
		t.Fatalf("body[name] = %#v, want %#v", got, want)
	}
	if got, want := body["direction"], "outgoing"; got != want {
		t.Fatalf("body[direction] = %#v, want %#v", got, want)
	}
	if got, want := body["relationship_type"], "CALLS"; got != want {
		t.Fatalf("body[relationship_type] = %#v, want %#v", got, want)
	}
	if got, want := body["transitive"], true; got != want {
		t.Fatalf("body[transitive] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 6; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsAnalyzeCodeRelationshipsImporters(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "find_importers",
		"target":     "payments",
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["name"], "payments"; got != want {
		t.Fatalf("body[name] = %#v, want %#v", got, want)
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
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
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
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
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
