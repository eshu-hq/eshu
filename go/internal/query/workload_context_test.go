// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeWorkloadGraphReader dispatches Cypher queries based on content matching.
type fakeWorkloadGraphReader struct {
	runSingleByMatch map[string]map[string]any
	runByMatch       map[string][]map[string]any
	run              func(context.Context, string, map[string]any) ([]map[string]any, error)
	runSingle        func(context.Context, string, map[string]any) (map[string]any, error)
}

func (f fakeWorkloadGraphReader) Run(ctx context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	if f.run != nil {
		return f.run(ctx, cypher, params)
	}
	var (
		bestRows []map[string]any
		bestLen  int
	)
	for fragment, rows := range f.runByMatch {
		if strings.Contains(cypher, fragment) && len(fragment) > bestLen {
			bestRows = rows
			bestLen = len(fragment)
		}
	}
	return bestRows, nil
}

func (f fakeWorkloadGraphReader) RunSingle(ctx context.Context, cypher string, params map[string]any) (map[string]any, error) {
	if f.runSingle != nil {
		return f.runSingle(ctx, cypher, params)
	}
	var (
		bestRow map[string]any
		bestLen int
	)
	for fragment, row := range f.runSingleByMatch {
		if strings.Contains(cypher, fragment) && len(fragment) > bestLen {
			bestRow = row
			bestLen = len(fragment)
		}
	}
	return bestRow, nil
}

func TestGetWorkloadContextReturnsEnrichedResponse(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				// Base workload query
				"MATCH (w:Workload)": {
					"id":        "workload-1",
					"name":      "order-service",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "order-service",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				// Dependencies query
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {
					{
						"type":          "DEPENDS_ON",
						"target_name":   "auth-service",
						"target_id":     "repo-2",
						"evidence_type": "terraform_module_source",
					},
				},
				// Infrastructure query
				"K8sResource OR": {
					{
						"type":      "K8sResource",
						"name":      "order-deployment",
						"kind":      "Deployment",
						"file_path": "k8s/deployment.yaml",
					},
				},
				// Entry points query
				"fn.name IN": {
					{
						"name":          "main",
						"relative_path": "cmd/server/main.go",
						"language":      "go",
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-1/context", nil)
	req.SetPathValue("workload_id", "workload-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// Base workload fields present
	if got, want := resp["id"], "workload-1"; got != want {
		t.Fatalf("id = %v, want %v", got, want)
	}
	if got, want := resp["name"], "order-service"; got != want {
		t.Fatalf("name = %v, want %v", got, want)
	}
	if got, want := resp["repo_id"], "repo-1"; got != want {
		t.Fatalf("repo_id = %v, want %v", got, want)
	}

	// Instances present
	instances, ok := resp["instances"].([]any)
	if !ok {
		t.Fatalf("instances type = %T, want []any", resp["instances"])
	}
	if len(instances) != 1 {
		t.Fatalf("len(instances) = %d, want 1", len(instances))
	}

	// Dependencies from repo enrichment
	deps, ok := resp["dependencies"].([]any)
	if !ok {
		t.Fatalf("dependencies type = %T, want []any", resp["dependencies"])
	}
	if len(deps) != 1 {
		t.Fatalf("len(dependencies) = %d, want 1", len(deps))
	}
	dep0, ok := deps[0].(map[string]any)
	if !ok {
		t.Fatalf("dependencies[0] type = %T", deps[0])
	}
	if got, want := dep0["type"], "DEPENDS_ON"; got != want {
		t.Fatalf("dependencies[0].type = %v, want %v", got, want)
	}

	// Infrastructure from repo enrichment
	infra, ok := resp["infrastructure"].([]any)
	if !ok {
		t.Fatalf("infrastructure type = %T, want []any", resp["infrastructure"])
	}
	if len(infra) != 1 {
		t.Fatalf("len(infrastructure) = %d, want 1", len(infra))
	}

	if _, exists := resp["entry_points"]; exists {
		t.Fatalf("entry_points = %#v, want omitted for workload context", resp["entry_points"])
	}
}

func TestFetchWorkloadContextLogsStageTimings(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &EntityHandler{
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (w:Workload)": {
					"id":   "workload-1",
					"name": "checkout-service",
					"kind": "service",
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEFINES": {
					{
						"repo_id":   "repo-1",
						"repo_name": "checkout-service",
					},
				},
				"INSTANCE_OF":                         {},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
			},
		},
	}

	got, err := handler.fetchWorkloadContext(
		t.Context(),
		"w.id = $workload_id",
		map[string]any{"workload_id": "workload-1"},
	)
	if err != nil {
		t.Fatalf("fetchWorkloadContext() error = %v, want nil", err)
	}
	if got == nil {
		t.Fatal("fetchWorkloadContext() = nil, want context")
	}

	logText := logs.String()
	for _, want := range []string{
		`"event_name":"service_query.stage_started"`,
		`"event_name":"service_query.stage_completed"`,
		`"operation":"workload_context"`,
		`"stage":"workload_lookup"`,
		`"stage":"repository_lookup"`,
		`"duration_seconds"`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s; logs = %s", want, logText)
		}
	}
}

func TestGetWorkloadContextReturnsNotFoundForMissingWorkload(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{},
			runByMatch:       map[string][]map[string]any{},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/unknown/context", nil)
	req.SetPathValue("workload_id", "unknown")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestGetWorkloadContextSkipsEnrichmentWhenNoRepoID(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"MATCH (w:Workload)": {
					"id":        "workload-orphan",
					"name":      "orphan-service",
					"kind":      "Deployment",
					"repo_id":   "",
					"repo_name": "",
					"instances": []any{},
				},
			},
			runByMatch: map[string][]map[string]any{},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-orphan/context", nil)
	req.SetPathValue("workload_id", "workload-orphan")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	// No enrichment sections when repo_id is empty
	if _, exists := resp["dependencies"]; exists {
		t.Fatal("dependencies should not be present when repo_id is empty")
	}
	if _, exists := resp["infrastructure"]; exists {
		t.Fatal("infrastructure should not be present when repo_id is empty")
	}
	if _, exists := resp["entry_points"]; exists {
		t.Fatal("entry_points should not be present when repo_id is empty")
	}
}

func TestGetServiceContextAcceptsQualifiedWorkloadID(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $service_name": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/workload:service-edge-api/context", nil)
	req.SetPathValue("service_name", "workload:service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got, want := resp["id"], "workload:service-edge-api"; got != want {
		t.Fatalf("id = %v, want %v", got, want)
	}
	if got, want := resp["name"], "service-edge-api"; got != want {
		t.Fatalf("name = %v, want %v", got, want)
	}
}

func TestGetServiceStoryAcceptsPlainServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				"w.name = $service_name": {
					{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "Deployment",
						"repo_id": "repo-1",
					},
				},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/story", nil)
	req.SetPathValue("service_name", "service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got, want := resp["service_name"], "service-edge-api"; got != want {
		t.Fatalf("service_name = %v, want %v", got, want)
	}
	if got, ok := resp["story"].(string); !ok || !strings.Contains(got, "Workload service-edge-api") {
		t.Fatalf("story = %#v, want narrative for service-edge-api", resp["story"])
	}
}

func TestGetServiceStoryAcceptsQualifiedWorkloadIDAndNormalizesServiceName(t *testing.T) {
	t.Parallel()

	handler := &EntityHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id":        "workload:service-edge-api",
					"name":      "service-edge-api",
					"kind":      "Deployment",
					"repo_id":   "repo-1",
					"repo_name": "service-edge-api",
					"instances": []any{
						map[string]any{
							"instance_id":   "inst-1",
							"platform_name": "eks-prod",
							"platform_kind": "EKS",
							"environment":   "production",
						},
					},
				},
			},
			runByMatch: map[string][]map[string]any{
				"w.id = $service_name": {
					{
						"id":      "workload:service-edge-api",
						"name":    "service-edge-api",
						"kind":    "Deployment",
						"repo_id": "repo-1",
					},
				},
				"DEPENDS_ON|USES_MODULE|DEPLOYS_FROM": {},
				"K8sResource OR":                      {},
				"fn.name IN":                          {},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/workload:service-edge-api/story", nil)
	req.SetPathValue("service_name", "workload:service-edge-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got, want := resp["service_name"], "service-edge-api"; got != want {
		t.Fatalf("service_name = %v, want %v", got, want)
	}
	if got, ok := resp["story"].(string); !ok || !strings.Contains(got, "Workload service-edge-api") {
		t.Fatalf("story = %#v, want narrative for service-edge-api", resp["story"])
	}
}
