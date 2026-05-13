package mcp

import "testing"

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
	if got, want := body["repo_id"], "repo://sample-service"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if _, exists := body["repo_ids"]; exists {
		t.Fatalf("body should not contain repo_ids, got %#v", body["repo_ids"])
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

func TestResolveRouteMapsFindDeadCodeExclusions(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_dead_code", map[string]any{
		"repo_id":                "repo-1",
		"limit":                  float64(25),
		"exclude_decorated_with": []any{"@route", "@app.route"},
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/dead-code" {
		t.Fatalf("route.path = %q, want /api/v0/code/dead-code", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["repo_id"], "repo-1"; got != want {
		t.Fatalf("body[repo_id] = %#v, want %#v", got, want)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	exclusions, ok := body["exclude_decorated_with"].([]any)
	if !ok {
		t.Fatalf("body[exclude_decorated_with] type = %T, want []any", body["exclude_decorated_with"])
	}
	if len(exclusions) != 2 {
		t.Fatalf("len(body[exclude_decorated_with]) = %d, want 2", len(exclusions))
	}
}

func TestResolveRouteMapsFindDeadIaC(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("find_dead_iac", map[string]any{
		"repo_ids":          []any{"terraform-stack", "terraform-modules"},
		"families":          []any{"terraform"},
		"include_ambiguous": true,
		"limit":             float64(25),
		"offset":            float64(50),
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/iac/dead" {
		t.Fatalf("route.path = %q, want /api/v0/iac/dead", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["limit"], 25; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
	if got, want := body["offset"], 50; got != want {
		t.Fatalf("body[offset] = %#v, want %#v", got, want)
	}
	if got, want := body["include_ambiguous"], true; got != want {
		t.Fatalf("body[include_ambiguous] = %#v, want %#v", got, want)
	}
	repoIDs := body["repo_ids"].([]any)
	if len(repoIDs) != 2 {
		t.Fatalf("len(repo_ids) = %d, want 2", len(repoIDs))
	}
	families := body["families"].([]any)
	if len(families) != 1 || families[0] != "terraform" {
		t.Fatalf("families = %#v, want terraform", families)
	}
}

func TestResolveRouteMapsAnalyzeDeadCodeLimit(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("analyze_code_relationships", map[string]any{
		"query_type": "dead_code",
		"repo_id":    "repo-1",
		"limit":      12,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/code/dead-code" {
		t.Fatalf("route.path = %q, want /api/v0/code/dead-code", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["limit"], 12; got != want {
		t.Fatalf("body[limit] = %#v, want %#v", got, want)
	}
}

func TestResolveRouteMapsTraceDeploymentChain(t *testing.T) {
	t.Parallel()

	route, err := resolveRoute("trace_deployment_chain", map[string]any{
		"service_name":                 "payments-api",
		"direct_only":                  true,
		"max_depth":                    float64(6),
		"include_related_module_usage": true,
	})
	if err != nil {
		t.Fatalf("resolveRoute() error = %v, want nil", err)
	}
	if route.path != "/api/v0/impact/trace-deployment-chain" {
		t.Fatalf("route.path = %q, want /api/v0/impact/trace-deployment-chain", route.path)
	}
	body, ok := route.body.(map[string]any)
	if !ok {
		t.Fatalf("route.body type = %T, want map[string]any", route.body)
	}
	if got, want := body["service_name"], "payments-api"; got != want {
		t.Fatalf("body[service_name] = %#v, want %#v", got, want)
	}
	if got, want := body["direct_only"], true; got != want {
		t.Fatalf("body[direct_only] = %#v, want %#v", got, want)
	}
	if got, want := body["max_depth"], 6; got != want {
		t.Fatalf("body[max_depth] = %#v, want %#v", got, want)
	}
	if got, want := body["include_related_module_usage"], true; got != want {
		t.Fatalf("body[include_related_module_usage] = %#v, want %#v", got, want)
	}
}
