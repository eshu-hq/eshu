package query

import (
	"context"
	"database/sql/driver"
	"strings"
	"testing"
)

func TestEnrichServiceQueryContextKeepsStrongAWSCloudResourceAnchorAsCandidate(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, emptyServiceQueryContentResults())
	workloadContext := sampleServiceCloudDependencyContext()
	graphCalls := []serviceCloudResourceGraphCall{}

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				call := recordServiceCloudResourceGraphCall(params)
				graphCalls = append(graphCalls, call)
				if strings.Contains(cypher, "rel:USES") {
					return nil, nil
				}
				if strings.Contains(cypher, "MATCH (n:CloudResource)") {
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
	if got, want := countServiceCloudResourceGraphCalls(graphCalls, isUncorrelatedCloudResourceCandidateCall), 1; got != want {
		t.Fatalf("uncorrelated cloud resource candidate graph calls = %d, want %d", got, want)
	}

	resources := mapSliceValue(workloadContext, "cloud_resources")
	if got, want := len(resources), 0; got != want {
		t.Fatalf("len(cloud_resources) = %d, want %d", got, want)
	}
	candidates := mapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(uncorrelated_cloud_resources) = %d, want %d", got, want)
	}
	if got, want := StringVal(candidates[0], "candidate_status"), "uncorrelated"; got != want {
		t.Fatalf("candidate_status = %q, want %q", got, want)
	}
	if got, want := StringVal(candidates[0], "service_anchor_status"), "strong"; got != want {
		t.Fatalf("service_anchor_status = %q, want %q", got, want)
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
	if got, want := IntVal(mapValue(story, "deployment_overview"), "cloud_resource_count"), 0; got != want {
		t.Fatalf("deployment_overview.cloud_resource_count = %d, want %d", got, want)
	}
}

func TestEnrichServiceQueryContextPrefersMaterializedWorkloadCloudRelationship(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, emptyServiceQueryContentResults())
	workloadContext := sampleServiceCloudDependencyContext()
	materializedCalls := 0
	fallbackCalls := 0

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "rel:USES") {
					materializedCalls++
					return []map[string]any{
						{
							"id":                    "cloud-resource:ssm-config",
							"name":                  "orders-api/database-url",
							"resource_type":         "aws_ssm_parameter",
							"provider":              "aws",
							"region":                "us-east-1",
							"relationship_basis":    "aws_resource_service_anchor",
							"resolution_mode":       "explicit_workload_anchor",
							"evidence_source":       "reducer/workload-cloud-relationship",
							"service_anchor_source": "payload.workload_id",
							"service_anchor_reason": "explicit_workload_anchor",
						},
					}, nil
				}
				if strings.Contains(cypher, "service_anchor_status = 'strong'") {
					fallbackCalls++
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
	if materializedCalls != 1 {
		t.Fatalf("materialized cloud dependency calls = %d, want 1", materializedCalls)
	}
	if fallbackCalls != 0 {
		t.Fatalf("fallback cloud dependency calls = %d, want 0 when materialized edge exists", fallbackCalls)
	}
	resources := mapSliceValue(workloadContext, "cloud_resources")
	if got, want := len(resources), 1; got != want {
		t.Fatalf("len(cloud_resources) = %d, want %d", got, want)
	}
	if got, want := StringVal(resources[0], "relationship_basis"), "aws_resource_service_anchor"; got != want {
		t.Fatalf("relationship_basis = %q, want %q", got, want)
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
	if got, want := IntVal(cloud, "promoted_count"), 1; got != want {
		t.Fatalf("cloud_dependencies promoted_count = %d, want %d", got, want)
	}
	if missing := StringSliceVal(cloud, "missing_evidence"); len(missing) != 0 {
		t.Fatalf("cloud_dependencies missing_evidence = %#v, want empty", missing)
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
				if strings.Contains(cypher, "rel:USES") {
					return nil, nil
				}
				if strings.Contains(cypher, "MATCH (n:CloudResource)") {
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

func TestEnrichServiceQueryContextKeepsStaleAWSCloudResourceAnchorAsCandidate(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, emptyServiceQueryContentResults())
	workloadContext := sampleServiceCloudDependencyContext()

	err := enrichServiceQueryContextWithOptions(
		context.Background(),
		fakeGraphReader{
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "rel:USES") {
					return nil, nil
				}
				if strings.Contains(cypher, "MATCH (n:CloudResource)") {
					return []map[string]any{
						{
							"id":                    "cloud-resource:old-queue",
							"name":                  "orders-api-old-queue",
							"resource_type":         "aws_sqs_queue",
							"provider":              "aws",
							"service_anchor_status": "stale",
							"service_anchor_reason": "stale_deployment_evidence",
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
		t.Fatalf("cloud_resources = %#v, want no promoted resource for stale anchor", resources)
	}
	candidates := mapSliceValue(workloadContext, "uncorrelated_cloud_resources")
	if got, want := len(candidates), 1; got != want {
		t.Fatalf("len(uncorrelated_cloud_resources) = %d, want %d", got, want)
	}
	if got, want := StringVal(candidates[0], "candidate_status"), "stale_anchor"; got != want {
		t.Fatalf("candidate_status = %q, want %q", got, want)
	}
	if got, want := StringVal(candidates[0], "service_anchor_reason"), "stale_deployment_evidence"; got != want {
		t.Fatalf("service_anchor_reason = %q, want %q", got, want)
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
	missing := StringSliceVal(cloud, "missing_evidence")
	if len(missing) != 1 || missing[0] != "stale_deployment_evidence" {
		t.Fatalf("cloud_dependencies missing_evidence = %#v, want stale_deployment_evidence", missing)
	}
	evidence := mapSliceValue(cloud, "evidence")
	if got, want := StringVal(evidence[0], "candidate_status"), "stale_anchor"; got != want {
		t.Fatalf("cloud candidate status = %q, want %q", got, want)
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
				if strings.Contains(cypher, "rel:USES") {
					if params["workload_id"] != "workload:orders-api" {
						t.Fatalf("workload_id param = %#v, want selected workload", params["workload_id"])
					}
					return nil, nil
				}
				if strings.Contains(cypher, "MATCH (n:CloudResource)") {
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

func TestConfigDerivedCloudResourceDependenciesRequireConfigReadEvidence(t *testing.T) {
	t.Parallel()

	calls := 0
	got, err := loadConfigDerivedCloudResourceDependencies(
		t.Context(),
		fakeGraphReader{
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				calls++
				return nil, nil
			},
		},
		map[string]any{
			"artifacts": []map[string]any{{
				"relationship_type": "DEPLOYS_FROM",
				"matched_value":     "/config/orders-api/*",
			}},
		},
		serviceStoryItemLimit,
	)
	if err != nil {
		t.Fatalf("loadConfigDerivedCloudResourceDependencies() error = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("config-derived resources = %#v, want none without READS_CONFIG_FROM", got)
	}
	if calls != 0 {
		t.Fatalf("graph calls = %d, want 0 without READS_CONFIG_FROM evidence", calls)
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

func isUncorrelatedCloudResourceCandidateCall(call serviceCloudResourceGraphCall) bool {
	_, hasWorkloadID := call.params["workload_id"]
	return !hasWorkloadID &&
		call.params["query"] == "orders-api" &&
		call.params["resource_type_query"] == "orders-api" &&
		call.params["limit"] == serviceStoryItemLimit+1
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
		{
			columns: []string{"payload"},
			rows:    [][]driver.Value{},
		},
	}
}
