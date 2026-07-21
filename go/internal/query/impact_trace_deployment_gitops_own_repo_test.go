// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
				if strings.Contains(cypher, "RETURN DISTINCT w.id as workload_id") {
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
				if strings.Contains(cypher, "RETURN DISTINCT w.id as workload_id") {
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

// TestTraceDeploymentChainOwnRepoWorkloadCountProbeErrorFailsClosedToNoTrust
// is the P2-1 regression test for #5471 review round 4: the doc comment on
// countWorkloadsDefinedByRepo's call site
// (impact_trace_deployment_controllers.go) claims a query error "fails
// closed to ownRepoWorkloadCount == 0, never trusted", but nothing proved
// it -- an inverted guard (e.g. treating err != nil as "trust" or leaving
// count uninitialized to some non-zero value) would silently re-open the
// same own-repo leak round 3 closed, with no test catching it.
//
// This models widget-config: the traced workload's own repo hosts exactly
// ONE GitOps controller, named after the app it deploys ("widget-source",
// not "widget-config") -- the same shape as the real deployable-config
// fixture, where the service-name-token match can never pass on its own.
// If the workload-count probe's error were NOT failing closed, this single
// indexed controller would be the exact case that gets wrongly trusted (the
// round-2 gate's countControllerEntitiesInRepo==1 case). The fake graph
// reader returns an error specifically for the
// `DEFINES]->(w:Workload)` count query, forcing
// countWorkloadsDefinedByRepo down its error path; every other query
// (workload resolution, the reverse DEFINES repo lookup) still succeeds
// normally. RED intent: without the `err == nil` guard at the call site,
// the count would come from garbage/zero-value handling that could
// coincide with 1 and wrongly fire trust; GREEN: the error path leaves
// ownRepoWorkloadCount at its zero-value default (0), trustOwnRepo stays
// false, the token match (which cannot pass) rejects the controller, and
// nothing leaks.
func TestTraceDeploymentChainOwnRepoWorkloadCountProbeErrorFailsClosedToNoTrust(t *testing.T) {
	t.Parallel()

	workload := map[string]any{
		"id":        "workload:widget-config",
		"name":      "widget-config",
		"kind":      "service",
		"repo_id":   "repository:widget-config",
		"repo_name": "widget-config",
		"instances": []any{},
	}
	entities := []EntityContent{
		{
			EntityID:     "argocd-app-1",
			RepoID:       "repository:widget-config",
			RelativePath: "application.yaml",
			EntityType:   "ArgoCDApplication",
			EntityName:   "widget-source",
			Metadata: map[string]any{
				"source_repo": "https://github.com/acme/widget-source",
				"source_path": "k8s",
			},
		},
		{
			EntityID:     "widget-deploy",
			RepoID:       "repository:widget-config",
			RelativePath: "k8s/deployment.yaml",
			EntityType:   "K8sResource",
			EntityName:   "widget-source",
			Metadata: map[string]any{
				"kind":             "Deployment",
				"container_images": []any{"registry.example/widget-source:1.0.0"},
			},
		},
	}
	workloadCountProbeErr := errors.New("nornicdb: injected workload-count probe failure")
	handler := &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, "DEFINES]-(r:Repository)") {
					return []map[string]any{{
						"repo_id":   "repository:widget-config",
						"repo_name": "widget-config",
					}}, nil
				}
				if strings.Contains(cypher, "RETURN DISTINCT w.id as workload_id") {
					// A single row is returned ALONGSIDE the error -- a
					// driver can surface a partial/stale result on a
					// stream interruption. If the caller looked at
					// len(rows) without checking err first, this row
					// would make the count look like exactly 1 and
					// wrongly fire own-repo trust; the fail-closed
					// contract requires the error alone to zero the
					// count regardless of what rows accompany it.
					return []map[string]any{{"workload_id": "workload:widget-config"}}, workloadCountProbeErr
				}
				return nil, nil
			},
		},
		Content: &recordingDeploymentSourceGitOpsContentStore{rows: entities},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"widget-config"}`),
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
	if slicesContains(imageRefs, "registry.example/widget-source:1.0.0") {
		t.Fatalf("image_refs = %#v, the sole own-repo controller must not be trusted when the workload-count probe errors", imageRefs)
	}

	k8sResources := mapSliceValue(body, "k8s_resources")
	if len(k8sResources) != 0 {
		t.Fatalf("k8s_resources = %#v, want none: the own-repo controller must not be selected when the workload-count probe errors", k8sResources)
	}

	summary := mapValue(body, "deployment_fact_summary")
	if got, want := StringVal(summary, "deployment_truth_tier"), "runtime_confirmed"; got == want {
		t.Fatalf("deployment_fact_summary.deployment_truth_tier = %q, widget-config must not be promoted using an untrusted own-repo controller", got)
	}
}
