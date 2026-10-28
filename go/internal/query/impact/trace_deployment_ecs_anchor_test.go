// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/hcl"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/query/impact/deployment"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

func TestFetchCloudResourceResultRetainsECSServiceIdentity(t *testing.T) {
	arn := "arn:aws:ecs:us-east-1:000000000000:service/c/api"
	h := &Handler{Neo4j: graph.FakeWorkloadGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if !strings.Contains(cypher, "c.resource_type as resource_type, c.arn as arn") {
			t.Fatalf("cloud resource query drops ECS service identity: %s", cypher)
		}
		return []map[string]any{{"id": "cloud-1", "name": "api", "kind": "aws_ecs_service", "resource_type": "aws_ecs_service", "arn": arn, "observation": map[string]any{"confidence": 0.9}}}, nil
	}}}
	result, err := h.fetchCloudResourceResult(context.Background(), "repo-api", "workload-api")
	if err != nil {
		t.Fatal(err)
	}
	got := deployment.ECSServicesFromCloudResources(result.Rows)
	if len(got) != 1 || got[0] != arn {
		t.Fatalf("graph-backed service anchors = %#v, want %q", got, arn)
	}
}

type fakeECSTaskStore struct {
	called int
	arn    string
	result *deployment.ECSServiceTaskEvidence
}

func (s *fakeECSTaskStore) ListRunningTasks(_ context.Context, arn string, _ querycontract.RepositoryAccessFilter) (*deployment.ECSServiceTaskEvidence, error) {
	s.called++
	s.arn = arn
	return s.result, nil
}

func TestFetchWorkloadECSTaskSummaryUsesExactGraphServiceAnchor(t *testing.T) {
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	arn := "arn:aws:ecs:us-east-1:000000000000:service/c/api"
	running := 1
	store := &fakeECSTaskStore{result: &deployment.ECSServiceTaskEvidence{RunningCount: &running, ImageDigests: []string{digest}, Tasks: []deployment.ECSTaskFact{{ARN: "task-a", Containers: []deployment.ECSContainerFact{{Name: "api", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}}}}}}
	h := &Handler{ECSTasks: store}
	got, err := h.fetchWorkloadECSTaskSummary(context.Background(), "api", "repo-api", CloudResourceResult{Rows: []map[string]any{{"resource_type": "aws_ecs_service", "arn": arn}}, Limits: map[string]any{"truncated": false}}, querycontract.RepositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count == nil || *got.Count != 1 || got.MatchedContainer != "api" || store.called != 1 || store.arn != arn {
		t.Fatalf("ECS result = %#v, store calls=%d arn=%q", got, store.called, store.arn)
	}
}

func TestFetchWorkloadECSTaskSummaryUsesOwnRepoLiteralDeclaration(t *testing.T) {
	const arn = "arn:aws:ecs:us-east-1:000000000000:service/prod/api"
	zero := 0
	store := &fakeECSTaskStore{result: &deployment.ECSServiceTaskEvidence{RunningCount: &zero}}
	h := &Handler{ECSTasks: store, Content: content.FakePortContentStore{Entities: []querycontract.EntityContent{{RepoID: "repo-api", EntityType: "TerraformResource", EntityName: "aws_ecs_service.api", Metadata: map[string]any{"resource_type": "aws_ecs_service", "attributes": map[string]any{"name": "api", "cluster": "arn:aws:ecs:us-east-1:000000000000:cluster/prod"}}}}}, Neo4j: graph.FakeWorkloadGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "RETURN DISTINCT w.id as workload_id") {
			return []map[string]any{{"workload_id": "workload-api"}}, nil
		}
		return nil, nil
	}}}
	got, err := h.fetchWorkloadECSTaskSummary(context.Background(), "api", "repo-api", CloudResourceResult{Limits: map[string]any{"truncated": false}}, querycontract.RepositoryAccessFilter{AllScopes: true})
	if err != nil || got.Count == nil || *got.Count != 0 || store.arn != arn {
		t.Fatalf("summary=%#v err=%v store.arn=%q", got, err, store.arn)
	}
}

func TestFetchWorkloadECSTaskSummaryParsesFixtureDeclaration(t *testing.T) {
	parsed, err := hcl.Parse(filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", "deployable-source", "main.tf"), false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	resources, ok := parsed["terraform_resources"].([]map[string]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("parsed resources = %#v", parsed["terraform_resources"])
	}
	const arn = "arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/deployable-source"
	zero := 0
	store := &fakeECSTaskStore{result: &deployment.ECSServiceTaskEvidence{RunningCount: &zero}}
	h := &Handler{ECSTasks: store, Content: content.FakePortContentStore{Entities: []querycontract.EntityContent{{RepoID: "repo-deployable-source", EntityType: "TerraformResource", EntityName: "aws_ecs_service.deployable_source", Metadata: resources[0]}}}, Neo4j: graph.FakeWorkloadGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "RETURN DISTINCT w.id as workload_id") {
			return []map[string]any{{"workload_id": "workload-deployable-source"}}, nil
		}
		return nil, nil
	}}}
	got, err := h.fetchWorkloadECSTaskSummary(context.Background(), "deployable-source", "repo-deployable-source", CloudResourceResult{Limits: map[string]any{"truncated": false}}, querycontract.RepositoryAccessFilter{AllScopes: true})
	if err != nil || got.Count == nil || *got.Count != 0 || store.arn != arn {
		t.Fatalf("summary=%#v err=%v store.arn=%q", got, err, store.arn)
	}
}

func TestFetchWorkloadECSTaskSummaryRejectsDeclarationWhenGraphIdentityIncomplete(t *testing.T) {
	store := &fakeECSTaskStore{}
	h := &Handler{ECSTasks: store, Content: content.FakePortContentStore{Entities: []querycontract.EntityContent{{RepoID: "repo-api", EntityType: "TerraformResource", Metadata: map[string]any{"resource_type": "aws_ecs_service", "attributes": map[string]any{"name": "api", "cluster": "arn:aws:ecs:us-east-1:000000000000:cluster/c"}}}}}, Neo4j: graph.FakeWorkloadGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "RETURN DISTINCT w.id as workload_id") {
			return []map[string]any{{"workload_id": "workload-api"}}, nil
		}
		return nil, nil
	}}}
	for _, tc := range []struct {
		name   string
		limits map[string]any
		want   string
	}{
		{name: "truncated graph identity", limits: map[string]any{"truncated": true}, want: "ecs_service_identity_truncated"},
		{name: "unknown graph coverage", want: "ecs_service_identity_coverage_unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := h.fetchWorkloadECSTaskSummary(context.Background(), "api", "repo-api", CloudResourceResult{Limits: tc.limits}, querycontract.RepositoryAccessFilter{AllScopes: true})
			if err != nil || got.Count != nil || got.Reason != tc.want || store.called != 0 {
				t.Fatalf("summary=%#v err=%v store calls=%d", got, err, store.called)
			}
		})
	}
}

func TestTraceDeploymentChainSurfacesGraphBoundECSTaskCount(t *testing.T) {
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	arn := "arn:aws:ecs:us-east-1:000000000000:service/c/api"
	running := 1
	store := &fakeECSTaskStore{result: &deployment.ECSServiceTaskEvidence{RunningCount: &running, ImageDigests: []string{digest}, Tasks: []deployment.ECSTaskFact{{ARN: "task-a", Containers: []deployment.ECSContainerFact{{Name: "api", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}}}}}}
	workload := map[string]any{"id": "workload-api", "name": "api", "repo_id": "repo-api", "repo_name": "api", "instances": []any{}, "image_refs": []string{digest}}
	h := &Handler{ECSTasks: store, Neo4j: graph.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{"w.name = $service_name": workload, "w.id = $workload_id": workload},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{workload}, nil
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-api", "repo_name": "api"}}, nil
			case strings.Contains(cypher, "MATCH (repo:Repository)-[:DEFINES]->(w:Workload") && strings.Contains(cypher, "-[rel:USES]->(c:CloudResource)"):
				return []map[string]any{{"id": "cloud-ecs-service", "name": "api", "kind": "aws_ecs_service", "resource_type": "aws_ecs_service", "arn": arn, "observation": map[string]any{"confidence": 0.9}}}, nil
			default:
				return nil, nil
			}
		},
	}, Content: content.FakePortContentStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"api"}`))
	w := httptest.NewRecorder()
	h.TraceDeploymentChain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	summary := querycontract.MapValue(body, "deployment_fact_summary")
	if got := summary["ecs_running_task_count"]; got != float64(1) {
		t.Fatalf("ECS count=%v, body=%s", got, w.Body.String())
	}
	if store.called != 1 || store.arn != arn {
		t.Fatalf("store calls=%d arn=%q", store.called, store.arn)
	}
}

func TestTraceDeploymentChainOmitsECSTaskCountWhenCloudIdentityPageTruncated(t *testing.T) {
	digest := "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"
	arn := "arn:aws:ecs:us-east-1:000000000000:service/c/api"
	running := 1
	store := &fakeECSTaskStore{result: &deployment.ECSServiceTaskEvidence{RunningCount: &running, ImageDigests: []string{digest}, Tasks: []deployment.ECSTaskFact{{ARN: "task-a", Containers: []deployment.ECSContainerFact{{Name: "api", Image: "000000000000.dkr.ecr.us-east-1.amazonaws.com/api:latest", Digest: digest}}}}}}
	workload := map[string]any{"id": "workload-api", "name": "api", "repo_id": "repo-api", "repo_name": "api", "instances": []any{}}
	cloudRows := make([]map[string]any, 0, querycontract.ServiceStoryItemLimit+1)
	cloudRows = append(cloudRows, map[string]any{"id": "cloud-api", "name": "api", "kind": "aws_ecs_service", "resource_type": "aws_ecs_service", "arn": arn, "observation": map[string]any{"confidence": 0.9}})
	for i := 1; i <= querycontract.ServiceStoryItemLimit; i++ {
		kind := "aws_s3_bucket"
		if i == querycontract.ServiceStoryItemLimit {
			kind = "aws_ecs_service"
		}
		cloudRows = append(cloudRows, map[string]any{"id": fmt.Sprintf("cloud-%03d", i), "name": fmt.Sprintf("other-%03d", i), "kind": kind, "resource_type": kind, "arn": fmt.Sprintf("arn:aws:ecs:us-east-1:000000000000:service/c/other-%03d", i), "observation": map[string]any{"confidence": 0.9}})
	}
	h := &Handler{ECSTasks: store, Neo4j: graph.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{"w.name = $service_name": workload, "w.id = $workload_id": workload},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{workload}, nil
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-api", "repo_name": "api"}}, nil
			case strings.Contains(cypher, "-[rel:USES]->(c:CloudResource)"):
				return cloudRows, nil
			default:
				return nil, nil
			}
		},
	}, Content: content.FakePortContentStore{}}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"api"}`))
	w := httptest.NewRecorder()
	h.TraceDeploymentChain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	summary := querycontract.MapValue(body, "deployment_fact_summary")
	if summary["ecs_running_task_count"] != nil || summary["ecs_running_task_count_unavailable_reason"] != "ecs_service_identity_truncated" || store.called != 0 {
		t.Fatalf("truncated cloud identity was counted: summary=%#v store calls=%d", summary, store.called)
	}
}

func TestTraceDeploymentChainSurfacesParsedECSDeclarationCount(t *testing.T) {
	parsed, err := hcl.Parse(filepath.Join("..", "..", "..", "..", "tests", "fixtures", "ecosystems", "deployable-source", "main.tf"), false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	resources, ok := parsed["terraform_resources"].([]map[string]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("parsed resources = %#v", parsed["terraform_resources"])
	}
	const arn = "arn:aws:ecs:us-east-1:123456789012:service/supply-chain-demo/deployable-source"
	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	running := 1
	store := &fakeECSTaskStore{result: &deployment.ECSServiceTaskEvidence{RunningCount: &running, ImageDigests: []string{digest}, Tasks: []deployment.ECSTaskFact{{ARN: "arn:aws:ecs:us-east-1:123456789012:task/supply-chain-demo/b", Containers: []deployment.ECSContainerFact{{Name: "deployable-source", Image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/deployable-source:latest", Digest: digest}}}}}}
	workload := map[string]any{"id": "workload-deployable-source", "name": "deployable-source", "repo_id": "repo-deployable-source", "repo_name": "deployable-source", "instances": []any{}}
	h := &Handler{ECSTasks: store, Content: content.FakePortContentStore{Entities: []querycontract.EntityContent{{RepoID: "repo-deployable-source", EntityType: "TerraformResource", EntityName: "aws_ecs_service.deployable_source", Metadata: resources[0]}}}, Neo4j: graph.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{"w.name = $service_name": workload, "w.id = $workload_id": workload},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{workload}, nil
			case strings.Contains(cypher, "RETURN DISTINCT w.id as workload_id"):
				return []map[string]any{{"workload_id": "workload-deployable-source"}}, nil
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-deployable-source", "repo_name": "deployable-source"}}, nil
			default:
				return nil, nil
			}
		},
	}}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"deployable-source"}`))
	w := httptest.NewRecorder()
	h.TraceDeploymentChain(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	summary := querycontract.MapValue(body, "deployment_fact_summary")
	if summary["ecs_running_task_count"] != float64(1) || summary["ecs_matched_container"] != "deployable-source" || !slices.Equal(querycontract.StringSliceVal(summary, "ecs_matched_containers"), []string{"deployable-source"}) || store.arn != arn {
		t.Fatalf("ECS summary=%#v store ARN=%q", summary, store.arn)
	}
}

func TestFetchWorkloadECSTaskSummaryRejectsAmbiguousServiceAnchors(t *testing.T) {
	store := &fakeECSTaskStore{}
	h := &Handler{ECSTasks: store}
	resources := []map[string]any{
		{"resource_type": "aws_ecs_service", "arn": "arn:aws:ecs:us-east-1:000000000000:service/c/api"},
		{"resource_type": "aws_ecs_service", "arn": "arn:aws:ecs:us-east-1:000000000000:service/other/api"},
	}
	got, err := h.fetchWorkloadECSTaskSummary(context.Background(), "api", "repo-api", CloudResourceResult{Rows: resources, Limits: map[string]any{"truncated": false}}, querycontract.RepositoryAccessFilter{AllScopes: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != nil || got.Reason != "ambiguous_ecs_service_identity" || store.called != 0 {
		t.Fatalf("ambiguous anchor result=%#v store calls=%d", got, store.called)
	}
}

func TestFetchWorkloadECSTaskSummaryScopedWithoutGrantDoesNotRead(t *testing.T) {
	store := &fakeECSTaskStore{}
	h := &Handler{ECSTasks: store}
	resources := []map[string]any{{"resource_type": "aws_ecs_service", "arn": "arn:aws:ecs:us-east-1:000000000000:service/c/api"}}
	got, err := h.fetchWorkloadECSTaskSummary(context.Background(), "api", "repo-api", CloudResourceResult{Rows: resources, Limits: map[string]any{"truncated": false}}, querycontract.RepositoryAccessFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Count != nil || got.Reason != "scoped_caller_no_grants" || store.called != 0 {
		t.Fatalf("ungranted result=%#v store calls=%d", got, store.called)
	}
}
