// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTraceDeploymentChainOmitsLimitsForUnprobedContextCloudResources(t *testing.T) {
	t.Parallel()

	workload := map[string]any{
		"id":        "workload:orders-api",
		"name":      "orders-api",
		"kind":      "service",
		"repo_id":   "repo-orders",
		"repo_name": "orders-api",
		"instances": []any{},
		"cloud_resources": []map[string]any{{
			"id":       "cloud-resource:orders-api",
			"name":     "orders-api-config",
			"kind":     "ssm_parameter",
			"provider": "aws",
		}},
	}
	handler := &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-orders", "repo_name": "orders-api"}}, nil
				case strings.Contains(cypher, "MATCH (workload:Workload {id: $workload_id})"):
					return []map[string]any{workload["cloud_resources"].([]map[string]any)[0]}, nil
				default:
					return nil, nil
				}
			},
		},
		Content: fakePortContentStore{},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api"}`),
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
	if got, want := len(mapSliceValue(body, "cloud_resources")), 1; got != want {
		t.Fatalf("cloud_resources len = %d, want %d; body = %#v", got, want, body)
	}
	if limits := mapValue(body, "cloud_resource_limits"); len(limits) != 0 {
		t.Fatalf("cloud_resource_limits = %#v, want omitted because context rows were not sentinel-probed", limits)
	}
}

func TestTraceDeploymentChainPreservesExactEmptyCloudResourceLimits(t *testing.T) {
	t.Parallel()

	workload := map[string]any{
		"id":        "workload:orders-api",
		"name":      "orders-api",
		"kind":      "service",
		"repo_id":   "repo-orders",
		"repo_name": "orders-api",
		"instances": []any{},
	}
	handler := &ImpactHandler{
		Neo4j: fakeWorkloadGraphReader{
			runSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			run: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return nil, nil
			},
		},
		Content: fakePortContentStore{},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api"}`),
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
	limits := mapValue(body, "cloud_resource_limits")
	if got, want := IntVal(limits, "limit"), serviceStoryItemLimit; got != want {
		t.Fatalf("cloud_resource_limits.limit = %d, want %d; limits = %#v", got, want, limits)
	}
	if got := IntVal(limits, "returned_count"); got != 0 {
		t.Fatalf("cloud_resource_limits.returned_count = %d, want 0", got)
	}
	if BoolVal(limits, "truncated") || BoolVal(limits, "observed_count_is_lower_bound") {
		t.Fatalf("cloud_resource_limits = %#v, want exact empty coverage", limits)
	}
}
