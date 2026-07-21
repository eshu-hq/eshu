// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchDeploymentSourceGitOpsCapsControllersAndDisclosesLowerBound(t *testing.T) {
	t.Parallel()

	rows := make([]EntityContent, 0, serviceStoryItemLimit+1)
	for index := range serviceStoryItemLimit + 1 {
		rows = append(rows, deploymentSourceControllerFixture(index))
	}
	store := &recordingDeploymentSourceGitOpsContentStore{rows: rows}
	handler := &ImpactHandler{Content: store}

	controllers, _, _, lowerBound, err := handler.fetchDeploymentSourceGitOps(
		t.Context(),
		"payments-api",
		"",
		[]map[string]any{{"repo_id": "repository:deploy"}},
	)
	if err != nil {
		t.Fatalf("fetchDeploymentSourceGitOps() error = %v", err)
	}
	if got, want := len(controllers), serviceStoryItemLimit; got != want {
		t.Fatalf("controller count = %d, want bounded count %d", got, want)
	}
	if !lowerBound {
		t.Fatal("fetchDeploymentSourceGitOps() lowerBound = false, want controller-cap disclosure")
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
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
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

	handler.traceDeploymentChain(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}
	limits := mapValue(body, "k8s_resource_limits")
	if !BoolVal(limits, "truncated") || !BoolVal(limits, "deployment_source_observed_count_is_lower_bound") {
		t.Fatalf("k8s_resource_limits = %#v, want GitOps lower-bound truncation", limits)
	}
	if got, want := len(mapSliceValue(body, "k8s_resources")), serviceStoryItemLimit; got != want {
		t.Fatalf("k8s_resources count = %d, want %d", got, want)
	}
	imageRefs := StringSliceVal(body, "image_refs")
	if got, want := len(imageRefs), serviceStoryItemLimit; got != want {
		t.Fatalf("image_refs count = %d, want images from returned K8s rows only (%d)", got, want)
	}
	if slicesContains(imageRefs, "registry.example/payments:50") {
		t.Fatalf("image_refs = %#v, omitted K8s row image must not escape the response bound", imageRefs)
	}
	controllerLimits := mapValue(mapValue(body, "controller_overview"), "entity_limits")
	if !BoolVal(controllerLimits, "truncated") || !BoolVal(controllerLimits, "observed_count_is_lower_bound") {
		t.Fatalf("controller_overview.entity_limits = %#v, want source-scan lower-bound disclosure", controllerLimits)
	}
}

// TestTraceDeploymentChainOwnRepoAppOfAposMonorepoDoesNotLeakOtherServiceEvidence
// is the full-trace-path regression test for #5471 review round 2 P0: the
// traced workload's own repo (ctx["repo_id"] -> workloadRepoID) is an
// app-of-apps GitOps monorepo hosting BOTH "sample-service-api"'s and
// "payments-api"'s ArgoCD Applications and K8s manifests -- mirroring the
// repo-helm fixture, but reached through the real HTTP handler instead of
// the selectRelevantDeploymentSourceControllers unit test. Tracing
// "sample-service-api" must return ONLY its own k8s_resources/image_refs;
// payments-api's Deployment and image ref must never appear in the
// response, or a wrong workload's live-cluster evidence could promote
// sample-service-api's deployment truth tier.
func TestTraceDeploymentChainOwnRepoAppOfAposMonorepoDoesNotLeakOtherServiceEvidence(t *testing.T) {
	t.Parallel()

	workload := map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repository:app-of-apps",
		"repo_name": "app-of-apps",
		"instances": []any{},
	}
	entities := []EntityContent{
		{
			EntityID:     "app-1",
			RepoID:       "repository:app-of-apps",
			RelativePath: "services/sample-service-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"source_path": "services/sample-service-api/overlays/prod",
			},
		},
		{
			EntityID:     "sample-deploy",
			RepoID:       "repository:app-of-apps",
			RelativePath: "services/sample-service-api/overlays/prod/deployment.yaml",
			EntityType:   "K8sResource",
			EntityName:   "sample-service-api",
			Metadata: map[string]any{
				"kind":             "Deployment",
				"container_images": []any{"registry.example/sample-service-api:1.2.3"},
			},
		},
		{
			EntityID:     "payments-app",
			RepoID:       "repository:app-of-apps",
			RelativePath: "services/payments-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"source_path": "services/payments-api/overlays/prod",
			},
		},
		{
			EntityID:     "payments-deploy",
			RepoID:       "repository:app-of-apps",
			RelativePath: "services/payments-api/overlays/prod/deployment.yaml",
			EntityType:   "K8sResource",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"kind":             "Deployment",
				"container_images": []any{"registry.example/payments-api:9.9.9"},
			},
		},
	}
	handler := &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "DEFINES]-(r:Repository)") {
					return []map[string]any{{
						"repo_id":   "repository:app-of-apps",
						"repo_name": "app-of-apps",
					}}, nil
				}
				if strings.Contains(cypher, "DEFINES]->(w:Workload)") {
					// The shared app-of-apps repo DEFINES two workloads
					// (sample-service-api, payments-api). ownRepoWorkloadCount
					// must come out to 2, not 1 -- own-repo trust must NOT
					// apply here; only the service-name-token match may
					// select a controller.
					return []map[string]any{
						{"workload_id": "workload:sample-service-api"},
						{"workload_id": "workload:payments-api"},
					}, nil
				}
				return nil, nil
			},
		},
		Content: &recordingDeploymentSourceGitOpsContentStore{rows: entities},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"sample-service-api"}`),
	)
	w := httptest.NewRecorder()

	handler.traceDeploymentChain(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}

	imageRefs := StringSliceVal(body, "image_refs")
	if !slicesContains(imageRefs, "registry.example/sample-service-api:1.2.3") {
		t.Fatalf("image_refs = %#v, want sample-service-api's own image_ref present", imageRefs)
	}
	if slicesContains(imageRefs, "registry.example/payments-api:9.9.9") {
		t.Fatalf("image_refs = %#v, payments-api's image_ref leaked from the shared app-of-apps repo", imageRefs)
	}

	k8sResources := mapSliceValue(body, "k8s_resources")
	for _, resource := range k8sResources {
		if StringVal(resource, "entity_name") == "payments-api" {
			t.Fatalf("k8s_resources = %#v, payments-api's Deployment leaked from the shared app-of-apps repo", k8sResources)
		}
	}
}

// TestTraceDeploymentChainOwnRepoPartialControllerDiscoveryDoesNotLeakOtherWorkloadEvidence
// is the full-trace-path regression test for #5471 review round 3 P0: the
// round-2 fix gated own-repo trust on countControllerEntitiesInRepo == 1,
// which conflated controller-entity uniqueness with workload OWNERSHIP
// uniqueness. This models the reachable leak the round-3 review named: the
// traced workload's own repo (ctx["repo_id"] -> workloadRepoID) DEFINES TWO
// workloads (sample-service-api, payments-api), but ONLY payments-api's
// ArgoCD Application/Deployment have been indexed so far -- ordinary partial
// discovery, nothing requires both workloads' controllers to be indexed
// atomically. Under a controller-count gate this looks like "exactly one
// controller in my own repo" and would be wrongly trusted; under the
// workload-count gate (ownRepoWorkloadCount=2, from the DEFINES query) it
// must NOT be trusted, so payments-api's Deployment/image_ref must never
// appear in sample-service-api's trace and sample-service-api must not be
// wrongly promoted to runtime_confirmed on payments-api's evidence.
func TestTraceDeploymentChainOwnRepoPartialControllerDiscoveryDoesNotLeakOtherWorkloadEvidence(t *testing.T) {
	t.Parallel()

	workload := map[string]any{
		"id":        "workload:sample-service-api",
		"name":      "sample-service-api",
		"kind":      "service",
		"repo_id":   "repository:app-of-apps",
		"repo_name": "app-of-apps",
		"instances": []any{},
	}
	// Only payments-api's controller and Deployment are indexed;
	// sample-service-api's own controller has not been discovered yet.
	entities := []EntityContent{
		{
			EntityID:     "payments-app",
			RepoID:       "repository:app-of-apps",
			RelativePath: "services/payments-api/argocd/application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"source_path": "services/payments-api/overlays/prod",
			},
		},
		{
			EntityID:     "payments-deploy",
			RepoID:       "repository:app-of-apps",
			RelativePath: "services/payments-api/overlays/prod/deployment.yaml",
			EntityType:   "K8sResource",
			EntityName:   "payments-api",
			Metadata: map[string]any{
				"kind":             "Deployment",
				"container_images": []any{"registry.example/payments-api:9.9.9"},
			},
		},
	}
	handler := &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "DEFINES]-(r:Repository)") {
					return []map[string]any{{
						"repo_id":   "repository:app-of-apps",
						"repo_name": "app-of-apps",
					}}, nil
				}
				if strings.Contains(cypher, "DEFINES]->(w:Workload)") {
					return []map[string]any{
						{"workload_id": "workload:sample-service-api"},
						{"workload_id": "workload:payments-api"},
					}, nil
				}
				return nil, nil
			},
		},
		Content: &recordingDeploymentSourceGitOpsContentStore{rows: entities},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"sample-service-api"}`),
	)
	w := httptest.NewRecorder()

	handler.traceDeploymentChain(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("traceDeploymentChain status = %d, body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode trace response: %v", err)
	}

	imageRefs := StringSliceVal(body, "image_refs")
	if slicesContains(imageRefs, "registry.example/payments-api:9.9.9") {
		t.Fatalf("image_refs = %#v, payments-api's image_ref leaked into sample-service-api's trace merely because it was the only controller indexed in their shared repo", imageRefs)
	}

	k8sResources := mapSliceValue(body, "k8s_resources")
	for _, resource := range k8sResources {
		if StringVal(resource, "entity_name") == "payments-api" {
			t.Fatalf("k8s_resources = %#v, payments-api's Deployment leaked into sample-service-api's trace", k8sResources)
		}
	}

	summary := mapValue(body, "deployment_fact_summary")
	if got, want := StringVal(summary, "deployment_truth_tier"), "runtime_confirmed"; got == want {
		t.Fatalf("deployment_fact_summary.deployment_truth_tier = %q, sample-service-api must not be wrongly promoted using payments-api's evidence", got)
	}
}

func deploymentSourceControllerFixture(index int) EntityContent {
	return EntityContent{
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

func deploymentSourceGitOpsSaturatedRows() []EntityContent {
	rows := make([]EntityContent, 0, repositorySemanticEntityLimit+1)
	rows = append(rows, deploymentSourceControllerFixture(0))
	for index := range serviceStoryItemLimit + 1 {
		rows = append(rows, EntityContent{
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
	for len(rows) < repositorySemanticEntityLimit+1 {
		index := len(rows)
		rows = append(rows, EntityContent{
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
