package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestEnrichServiceQueryContextPromotesStrongAWSCloudResourceAnchor(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, emptyServiceQueryContentResults())
	workloadContext := sampleServiceCloudDependencyContext()
	graphCalls := []serviceCloudResourceGraphCall{}

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
				call := recordServiceCloudResourceGraphCall(params)
				graphCalls = append(graphCalls, call)
				if isStrongServiceCloudResourceDependencyCall(call) {
					return []map[string]any{
						{
							"id":                    "cloud-resource:orders-listener",
							"name":                  "orders-listener",
							"resource_type":         "aws_vpclattice_listener",
							"provider":              "aws",
							"region":                "us-east-1",
							"service_anchor_status": "strong",
							"service_anchor_source": "attributes.service_name",
						},
					}, nil
				}
				return nil, nil
			},
		},
		NewContentReader(db),
		workloadContext,
		serviceQueryEnrichmentOptions{
			IncludeRelatedModuleUsage: true,
			Operation:                 "service_story",
		},
	)
	if err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}
	if got, want := countServiceCloudResourceGraphCalls(graphCalls, isStrongServiceCloudResourceDependencyCall), 1; got != want {
		t.Fatalf("strong cloud resource dependency graph calls = %d, want %d", got, want)
	}
	if got := countServiceCloudResourceGraphCalls(graphCalls, isUncorrelatedCloudResourceCandidateCall); got != 0 {
		t.Fatalf("uncorrelated cloud resource candidate graph calls = %d, want 0", got)
	}

	resources := mapSliceValue(workloadContext, "cloud_resources")
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(cloud_resources) = %d, want %d", got, want)
	}
	if got, want := StringVal(resources[0], "relationship_basis"), "aws_resource_service_anchor"; got != want {
		t.Fatalf("relationship_basis = %q, want %q", got, want)
	}
	if got, want := StringVal(resources[0], "service_anchor_status"), "strong"; got != want {
		t.Fatalf("service_anchor_status = %q, want %q", got, want)
	}
	if candidates := mapSliceValue(workloadContext, "uncorrelated_cloud_resources"); len(candidates) != 0 {
		t.Fatalf("uncorrelated_cloud_resources = %#v, want omitted when strong anchor promotes", candidates)
	}

	story := buildServiceStoryResponse("orders-api", workloadContext)
	trace := mapValue(story, "code_to_runtime_trace")
	cloud := segmentByName(mapSliceValue(trace, "segments"), "cloud_dependencies")
	if cloud == nil {
		t.Fatalf("cloud_dependencies segment missing from trace: %#v", trace)
	}
	if got, want := StringVal(cloud, "status"), "derived"; got != want {
		t.Fatalf("cloud_dependencies status = %q, want %q", got, want)
	}
	if got, want := IntVal(mapValue(story, "deployment_overview"), "cloud_resource_count"), 1; got != want {
		t.Fatalf("deployment_overview.cloud_resource_count = %d, want %d", got, want)
	}
}

func TestEnrichServiceQueryContextKeepsAmbiguousAWSCloudResourceAnchorAsCandidate(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, emptyServiceQueryContentResults())
	workloadContext := sampleServiceCloudDependencyContext()

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "service_anchor_status = 'strong'") {
					return nil, nil
				}
				if strings.Contains(cypher, "WHERE (n:CloudResource)") {
					return []map[string]any{
						{
							"id":                    "cloud-resource:shared-listener",
							"name":                  "orders-api-shared-listener",
							"resource_type":         "aws_vpclattice_listener",
							"provider":              "aws",
							"service_anchor_status": "ambiguous",
							"service_anchor_reason": "multiple_service_anchors",
						},
					}, nil
				}
				return nil, nil
			},
		},
		NewContentReader(db),
		workloadContext,
		serviceQueryEnrichmentOptions{
			IncludeRelatedModuleUsage: true,
			Operation:                 "service_story",
		},
	)
	if err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}

	if resources := mapSliceValue(workloadContext, "cloud_resources"); len(resources) != 0 {
		t.Fatalf("cloud_resources = %#v, want no promoted resource for ambiguous anchor", resources)
	}
	candidates := mapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(uncorrelated_cloud_resources) = %d, want %d", got, want)
	}
	if got, want := StringVal(candidates[0], "candidate_status"), "ambiguous_anchor"; got != want {
		t.Fatalf("candidate_status = %q, want %q", got, want)
	}

	story := buildServiceStoryResponse("orders-api", workloadContext)
	trace := mapValue(story, "code_to_runtime_trace")
	cloud := segmentByName(mapSliceValue(trace, "segments"), "cloud_dependencies")
	if cloud == nil {
		t.Fatalf("cloud_dependencies segment missing from trace: %#v", trace)
	}
	if got, want := StringVal(cloud, "status"), "missing_evidence"; got != want {
		t.Fatalf("cloud_dependencies status = %q, want %q", got, want)
	}
	if got, want := StringVal(cloud, "missing_relationship"), "workload_cloud_relationship"; got != want {
		t.Fatalf("missing_relationship = %q, want %q", got, want)
	}
}

func TestEnrichServiceQueryContextDoesNotPromoteWrongTargetAWSCloudResourceAnchor(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, emptyServiceQueryContentResults())
	workloadContext := sampleServiceCloudDependencyContext()

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "service_anchor_status = 'strong'") {
					if params["service_name"] != "orders-api" {
						t.Fatalf("service_name param = %#v, want selected service", params["service_name"])
					}
					return nil, nil
				}
				if strings.Contains(cypher, "WHERE (n:CloudResource)") {
					return nil, nil
				}
				return nil, nil
			},
		},
		NewContentReader(db),
		workloadContext,
		serviceQueryEnrichmentOptions{
			IncludeRelatedModuleUsage: true,
			Operation:                 "service_story",
		},
	)
	if err != nil {
		t.Fatalf("enrichServiceQueryContextWithOptions() error = %v, want nil", err)
	}

	if resources := mapSliceValue(workloadContext, "cloud_resources"); len(resources) != 0 {
		t.Fatalf("cloud_resources = %#v, want no promoted resource for wrong target", resources)
	}
	if candidates := mapSliceValue(workloadContext, "uncorrelated_cloud_resources"); len(candidates) != 0 {
		t.Fatalf("uncorrelated_cloud_resources = %#v, want empty for wrong target without candidate match", candidates)
	}

	story := buildServiceStoryResponse("orders-api", workloadContext)
	trace := mapValue(story, "code_to_runtime_trace")
	cloud := segmentByName(mapSliceValue(trace, "segments"), "cloud_dependencies")
	if got, want := StringVal(cloud, "status"), "missing_evidence"; got != want {
		t.Fatalf("cloud_dependencies status = %q, want %q", got, want)
	}
	if got, want := len(mapSliceValue(cloud, "evidence")), 0; got != want {
		t.Fatalf("cloud_dependencies evidence len = %d, want %d", got, want)
	}
}

func sampleServiceCloudDependencyContext() map[string]any {
	return map[string]any{
		"id":        "workload:orders-api",
		"name":      "orders-api",
		"kind":      "service",
		"repo_id":   "repo-orders-api",
		"repo_name": "orders-api",
		"instances": []map[string]any{
			{
				"instance_id":   "instance:orders-api:production",
				"platform_name": "production-runtime",
				"platform_kind": "kubernetes",
				"environment":   "production",
			},
		},
	}
}

type serviceCloudResourceGraphCall struct {
	params map[string]any
}

func recordServiceCloudResourceGraphCall(params map[string]any) serviceCloudResourceGraphCall {
	copied := make(map[string]any, len(params))
	for key, value := range params {
		copied[key] = value
	}
	return serviceCloudResourceGraphCall{params: copied}
}

func countServiceCloudResourceGraphCalls(
	calls []serviceCloudResourceGraphCall,
	match func(serviceCloudResourceGraphCall) bool,
) int {
	count := 0
	for _, call := range calls {
		if match(call) {
			count++
		}
	}
	return count
}

func isStrongServiceCloudResourceDependencyCall(call serviceCloudResourceGraphCall) bool {
	return call.params["workload_id"] == "workload:orders-api" &&
		call.params["service_name"] == "orders-api" &&
		call.params["service_token"] == "orders-api" &&
		call.params["limit"] == serviceStoryItemLimit
}

func isUncorrelatedCloudResourceCandidateCall(call serviceCloudResourceGraphCall) bool {
	_, hasWorkloadID := call.params["workload_id"]
	return !hasWorkloadID &&
		call.params["service_name"] == "orders-api" &&
		call.params["service_token"] == "orders-api" &&
		call.params["limit"] == serviceStoryItemLimit
}

func emptyServiceQueryContentResults() []contentReaderQueryResult {
	return []contentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content",
				"content_hash", "line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{},
		},
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{},
		},
	}
}
