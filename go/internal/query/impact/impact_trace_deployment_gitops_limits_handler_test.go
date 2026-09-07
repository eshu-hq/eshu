// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestFetchDeploymentSourceGitOpsCapsControllersAndDisclosesLowerBound(t *testing.T) {
	t.Parallel()

	rows := make([]querycontract.EntityContent, 0, querycontract.ServiceStoryItemLimit+1)
	for index := range querycontract.ServiceStoryItemLimit + 1 {
		rows = append(rows, deploymentSourceControllerFixture(index))
	}
	store := &recordingDeploymentSourceGitOpsContentStore{rows: rows}
	handler := &ImpactHandler{Content: store}

	controllers, _, _, lowerBound, err := handler.FetchDeploymentSourceGitOps(
		t.Context(),
		"payments-api",
		"",
		[]map[string]any{{"repo_id": "repository:deploy"}},
	)
	if err != nil {
		t.Fatalf("FetchDeploymentSourceGitOps() error = %v", err)
	}
	if got, want := len(controllers), querycontract.ServiceStoryItemLimit; got != want {
		t.Fatalf("controller count = %d, want bounded count %d", got, want)
	}
	if !lowerBound {
		t.Fatal("FetchDeploymentSourceGitOps() lowerBound = false, want controller-cap disclosure")
	}
}

func TestTraceDeploymentChainPropagatesGitOpsBoundsAndExcludesOmittedImages(t *testing.T) {
	t.Parallel()

	workload := map[string]any{
		"id":        "workload:payments-api",
		"name":      "payments-api",
		"kind":      "service",
		"repo_id":   "repository:service",
		"repo_name": "payments-api",
		"instances": []any{},
	}
	rows := deploymentSourceGitOpsSaturatedRows()
	handler := &ImpactHandler{
		Neo4j: querytestutil.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "rel:DEPLOYMENT_SOURCE"):
					return []map[string]any{{
						"instance_id": "instance:payments-api",
						"repo_id":     "repository:deploy",
						"repo_name":   "deploy-config",
						"confidence":  0.95,
						"reason":      "canonical_instance_deployment_source",
					}}, nil
				default:
					return nil, nil
				}
			},
		},
		Content: &recordingDeploymentSourceGitOpsContentStore{rows: rows},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"payments-api"}`),
	)
	w := httptest.NewRecorder()

	handler.TraceDeploymentChain(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}
	limits := querycontract.MapValue(body, "k8s_resource_limits")
	if !querycontract.BoolVal(limits, "truncated") || !querycontract.BoolVal(limits, "deployment_source_observed_count_is_lower_bound") {
		t.Fatalf("k8s_resource_limits = %#v, want GitOps lower-bound truncation", limits)
	}
	if got, want := len(querycontract.MapSliceValue(body, "k8s_resources")), querycontract.ServiceStoryItemLimit; got != want {
		t.Fatalf("k8s_resources count = %d, want %d", got, want)
	}
	imageRefs := querycontract.StringSliceVal(body, "image_refs")
	if got, want := len(imageRefs), querycontract.ServiceStoryItemLimit; got != want {
		t.Fatalf("image_refs count = %d, want images from returned K8s rows only (%d)", got, want)
	}
	if slicesContains(imageRefs, "registry.example/payments:50") {
		t.Fatalf("image_refs = %#v, omitted K8s row image must not escape the response bound", imageRefs)
	}
	controllerLimits := querycontract.MapValue(querycontract.MapValue(body, "controller_overview"), "entity_limits")
	if !querycontract.BoolVal(controllerLimits, "truncated") || !querycontract.BoolVal(controllerLimits, "observed_count_is_lower_bound") {
		t.Fatalf("controller_overview.entity_limits = %#v, want source-scan lower-bound disclosure", controllerLimits)
	}
}

func deploymentSourceControllerFixture(index int) querycontract.EntityContent {
	return querycontract.EntityContent{
		EntityID:     fmt.Sprintf("controller-%02d", index),
		RepoID:       "repository:deploy",
		RelativePath: fmt.Sprintf("services/payments-api/argocd/%02d.yaml", index),
		EntityType:   "ArgoCDApplication",
		EntityName:   fmt.Sprintf("payments-api-%02d", index),
		Metadata: map[string]any{
			"source_path": "services/payments-api/overlays/prod",
		},
	}
}

func deploymentSourceGitOpsSaturatedRows() []querycontract.EntityContent {
	rows := make([]querycontract.EntityContent, 0, querycontract.RepositorySemanticEntityLimit+1)
	rows = append(rows, deploymentSourceControllerFixture(0))
	for index := range querycontract.ServiceStoryItemLimit + 1 {
		rows = append(rows, querycontract.EntityContent{
			EntityID:     fmt.Sprintf("deployment-%02d", index),
			RepoID:       "repository:deploy",
			RelativePath: fmt.Sprintf("services/payments-api/overlays/prod/%02d.yaml", index),
			EntityType:   "K8sResource",
			EntityName:   fmt.Sprintf("payments-api-%02d", index),
			Metadata: map[string]any{
				"kind":             "Deployment",
				"container_images": []any{fmt.Sprintf("registry.example/payments:%02d", index)},
			},
		})
	}
	for len(rows) < querycontract.RepositorySemanticEntityLimit+1 {
		index := len(rows)
		rows = append(rows, querycontract.EntityContent{
			EntityID:     fmt.Sprintf("filler-%04d", index),
			RepoID:       "repository:deploy",
			RelativePath: fmt.Sprintf("unrelated/%04d.txt", index),
			EntityType:   "Documentation",
			EntityName:   "unrelated",
		})
	}
	return rows
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
