package query

import (
	"context"
	"strings"
	"testing"
)

func TestCompareEnvironmentsReturnsStoryGradePacket(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: compareStoryWorkloadAndInstances,
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "LIMIT $limit") {
					t.Fatalf("resource cypher = %q, want LIMIT $limit", cypher)
				}
				switch params["instance_id"] {
				case "instance:prod":
					return []map[string]any{
						{"id": "cloud:shared-db", "name": "shared-db", "kind": "database", "provider": "aws", "confidence": 1.0, "reason": "materialized_cloud_dependency"},
						{"id": "cloud:prod-dlq", "name": "prod-dlq", "kind": "queue", "provider": "aws", "confidence": 0.9, "reason": "materialized_cloud_dependency"},
					}, nil
				case "instance:staging":
					return []map[string]any{
						{"id": "cloud:shared-db", "name": "shared-db", "kind": "database", "provider": "aws", "confidence": 1.0, "reason": "materialized_cloud_dependency"},
						{"id": "cloud:stage-cache", "name": "stage-cache", "kind": "cache", "provider": "aws", "confidence": 0.8, "reason": "materialized_cloud_dependency"},
					}, nil
				}
				return nil, nil
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:checkout-service","left":"prod","right":"staging","limit":10}`)

	if got := resp["story"]; got == "" {
		t.Fatal("story is empty, want prompt-ready summary")
	}
	summary := requireMap(t, resp, "summary")
	if got, want := summary["comparison"], "different"; got != want {
		t.Fatalf("summary.comparison = %#v, want %#v", got, want)
	}
	if got, want := summary["shared_resource_count"], float64(1); got != want {
		t.Fatalf("summary.shared_resource_count = %#v, want %#v", got, want)
	}
	if got, want := summary["left_dedicated_resource_count"], float64(1); got != want {
		t.Fatalf("summary.left_dedicated_resource_count = %#v, want %#v", got, want)
	}
	if got, want := summary["right_dedicated_resource_count"], float64(1); got != want {
		t.Fatalf("summary.right_dedicated_resource_count = %#v, want %#v", got, want)
	}

	shared := requireMap(t, resp, "shared")
	if got := requireMapSlice(t, shared, "cloud_resources"); len(got) != 1 {
		t.Fatalf("len(shared.cloud_resources) = %d, want 1", len(got))
	}
	dedicated := requireMap(t, resp, "dedicated")
	leftDedicated := requireMap(t, dedicated, "left")
	if got := requireMapSlice(t, leftDedicated, "cloud_resources"); len(got) != 1 {
		t.Fatalf("len(dedicated.left.cloud_resources) = %d, want 1", len(got))
	}
	rightDedicated := requireMap(t, dedicated, "right")
	if got := requireMapSlice(t, rightDedicated, "cloud_resources"); len(got) != 1 {
		t.Fatalf("len(dedicated.right.cloud_resources) = %d, want 1", len(got))
	}

	evidence := requireMapSlice(t, resp, "evidence")
	if len(evidence) < 3 {
		t.Fatalf("len(evidence) = %d, want materialized evidence rows", len(evidence))
	}
	nextCalls := requireMapSlice(t, resp, "recommended_next_calls")
	if len(nextCalls) == 0 {
		t.Fatal("recommended_next_calls is empty, want exact drilldown calls")
	}
	coverage := requireMap(t, resp, "coverage")
	if got, want := coverage["comparison_basis"], "materialized_cloud_resources"; got != want {
		t.Fatalf("coverage.comparison_basis = %#v, want %#v", got, want)
	}
	if got, want := coverage["freshness_state"], "fresh"; got != want {
		t.Fatalf("coverage.freshness_state = %#v, want %#v", got, want)
	}
}

func TestCompareEnvironmentsStoryReportsMissingEvidenceLimitations(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
				if strings.Contains(cypher, "MATCH (w:Workload)") {
					return map[string]any{
						"id":      "workload:checkout-service",
						"name":    "checkout-service",
						"kind":    "service",
						"repo_id": "repo-checkout-service",
					}, nil
				}
				return nil, nil
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:checkout-service","left":"prod","right":"staging"}`)

	summary := requireMap(t, resp, "summary")
	if got, want := summary["comparison"], "unsupported"; got != want {
		t.Fatalf("summary.comparison = %#v, want %#v", got, want)
	}
	limitations := requireMapSlice(t, resp, "limitations")
	if len(limitations) == 0 {
		t.Fatal("limitations is empty, want missing environment evidence limitation")
	}
	if got, want := limitations[0]["kind"], "missing_environment_evidence"; got != want {
		t.Fatalf("limitations[0].kind = %#v, want %#v", got, want)
	}
}

func TestCompareEnvironmentsStoryRecommendsLargerBoundWhenTruncated(t *testing.T) {
	t.Parallel()

	handler := &CompareHandler{
		Neo4j: fakeCompareGraphReader{
			runSingle: compareStoryWorkloadAndInstances,
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				return []map[string]any{
					{"id": "cloud:" + params["instance_id"].(string) + ":a", "name": "queue-a", "kind": "queue", "provider": "aws", "confidence": 1.0},
					{"id": "cloud:" + params["instance_id"].(string) + ":b", "name": "queue-b", "kind": "queue", "provider": "aws", "confidence": 1.0},
				}, nil
			},
		},
	}

	resp := executeCompareEnvironmentsRequest(t, handler, `{"workload_id":"workload:checkout-service","left":"prod","right":"staging","limit":1}`)

	nextCalls := requireMapSlice(t, resp, "recommended_next_calls")
	var pagination map[string]any
	for _, call := range nextCalls {
		if call["tool"] == "compare_environments" {
			pagination = call
			break
		}
	}
	if pagination == nil {
		t.Fatal("recommended_next_calls missing compare_environments pagination call")
	}
	args := requireMap(t, pagination, "arguments")
	if got, want := args["limit"], float64(2); got != want {
		t.Fatalf("pagination limit = %#v, want %#v", got, want)
	}
}

func compareStoryWorkloadAndInstances(_ context.Context, cypher string, params map[string]any) (map[string]any, error) {
	switch {
	case strings.Contains(cypher, "MATCH (w:Workload)"):
		return map[string]any{
			"id":      "workload:checkout-service",
			"name":    "checkout-service",
			"kind":    "service",
			"repo_id": "repo-checkout-service",
		}, nil
	case strings.Contains(cypher, "MATCH (i:WorkloadInstance)"):
		environment := params["environment"].(string)
		return map[string]any{
			"id":          "instance:" + environment,
			"name":        "checkout-service-" + environment,
			"kind":        "service",
			"environment": environment,
			"workload_id": "workload:checkout-service",
		}, nil
	default:
		return nil, nil
	}
}
