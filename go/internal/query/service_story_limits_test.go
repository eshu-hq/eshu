package query

import (
	"fmt"
	"testing"
)

func TestServiceStoryDossierUsesAggregateAPICountsAndSpecPaths(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["api_surface"] = map[string]any{
		"endpoint_count": 73,
		"method_count":   9,
		"spec_paths":     []string{"openapi.yaml", "admin.yaml"},
		"endpoints":      []map[string]any{},
	}

	got := buildServiceStoryResponse("sample-service-api", ctx)
	apiSurface := mapValue(got, "api_surface")
	if got, want := IntVal(apiSurface, "endpoint_count"), 73; got != want {
		t.Fatalf("api_surface.endpoint_count = %d, want %d", got, want)
	}
	if got, want := IntVal(apiSurface, "spec_count"), 2; got != want {
		t.Fatalf("api_surface.spec_count = %d, want len(spec_paths) %d", got, want)
	}
	limits := mapValue(got, "result_limits")
	if got, want := IntVal(limits, "endpoint_count"), 73; got != want {
		t.Fatalf("result_limits.endpoint_count = %d, want aggregate %d", got, want)
	}
}

func TestServiceStoryDossierBoundsRawPayloadsAndNestedAPISurface(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["api_surface"] = map[string]any{
		"endpoint_count": 60,
		"method_count":   1,
		"endpoints":      numberedRows("path", 60),
	}
	ctx["hostnames"] = numberedRows("hostname", 60)
	ctx["deployment_evidence"] = map[string]any{
		"artifacts":       numberedRows("resolved_id", 60),
		"delivery_paths":  numberedRows("path", 60),
		"artifact_count":  60,
		"tool_families":   []string{"argocd"},
		"evidence_family": "deployment",
	}

	got := buildServiceStoryResponse("sample-service-api", ctx)
	if got := len(mapSliceValue(got, "hostnames")); got != serviceStoryItemLimit {
		t.Fatalf("top-level hostnames length = %d, want cap %d", got, serviceStoryItemLimit)
	}
	deploymentEvidence := mapValue(got, "deployment_evidence")
	if got := len(mapSliceValue(deploymentEvidence, "artifacts")); got != serviceStoryItemLimit {
		t.Fatalf("deployment_evidence.artifacts length = %d, want cap %d", got, serviceStoryItemLimit)
	}
	overviewAPI := mapValue(mapValue(got, "deployment_overview"), "api_surface")
	if got := len(mapSliceValue(overviewAPI, "endpoints")); got != serviceStoryItemLimit {
		t.Fatalf("deployment_overview.api_surface.endpoints length = %d, want cap %d", got, serviceStoryItemLimit)
	}
	rawLimits := mapValue(got, "raw_context_limits")
	if !BoolVal(mapValue(rawLimits, "hostnames"), "truncated") {
		t.Fatalf("raw_context_limits.hostnames.truncated = false, want true")
	}
}

func TestServiceStoryDossierDedupesRelationshipPreviewsAndIncludesDependents(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["dependents"] = []map[string]any{
		{"repository": "consumer-a", "repo_id": "repo-consumer-a", "relationship_types": []string{"CALLS"}},
	}
	ctx["deployment_evidence"] = map[string]any{
		"artifacts": []map[string]any{
			{"direction": "incoming", "relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-same", "confidence": 0.7, "source_repo_id": "repo-deploy", "source_repo_name": "deploy", "target_repo_id": "repo-service", "target_repo_name": "service"},
			{"direction": "incoming", "relationship_type": "DEPLOYS_FROM", "resolved_id": "resolved-same", "confidence": 0.9, "source_repo_id": "repo-deploy", "source_repo_name": "deploy", "target_repo_id": "repo-service", "target_repo_name": "service"},
		},
	}

	got := buildServiceStoryResponse("sample-service-api", ctx)
	upstream := mapSliceValue(got, "upstream_dependencies")
	if countRowsWithValue(upstream, "resolved_id", "resolved-same") != 1 {
		t.Fatalf("upstream_dependencies = %#v, want one row for resolved-same", upstream)
	}
	edges := mapSliceValue(mapValue(got, "evidence_graph"), "edges")
	if countRowsWithValue(edges, "resolved_id", "resolved-same") != 1 {
		t.Fatalf("evidence_graph.edges = %#v, want one edge for resolved-same", edges)
	}
	nodes := mapSliceValue(mapValue(got, "evidence_graph"), "nodes")
	if countRowsWithValue(nodes, "id", "repo-consumer-a") != 1 {
		t.Fatalf("evidence_graph.nodes = %#v, want graph dependent repo node", nodes)
	}
}

func TestServiceStoryResultLimitsMatchIndependentDownstreamCaps(t *testing.T) {
	t.Parallel()

	ctx := sampleServiceDossierContext()
	ctx["dependents"] = numberedRows("repo_id", 30)
	ctx["consumer_repositories"] = numberedRows("repo_id", 30)

	got := buildServiceStoryResponse("sample-service-api", ctx)
	downstream := mapValue(got, "downstream_consumers")
	if BoolVal(downstream, "truncated") {
		t.Fatalf("downstream_consumers.truncated = true, want false for two independently uncapped lists")
	}
	limits := mapValue(got, "result_limits")
	if BoolVal(limits, "truncated") {
		t.Fatalf("result_limits.truncated = true, want false when no section cap is exceeded")
	}
}

func numberedRows(key string, count int) []map[string]any {
	rows := make([]map[string]any, 0, count)
	for i := range count {
		rows = append(rows, map[string]any{
			key: fmt.Sprintf("%s-%03d", key, i),
		})
	}
	return rows
}

func countRowsWithValue(rows []map[string]any, key string, want string) int {
	count := 0
	for _, row := range rows {
		if StringVal(row, key) == want {
			count++
		}
	}
	return count
}
