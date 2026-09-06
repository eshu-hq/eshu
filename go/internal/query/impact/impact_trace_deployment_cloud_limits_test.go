// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
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
		Neo4j: querytestutil.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-orders", "repo_name": "orders-api"}}, nil
				case strings.Contains(cypher, "MATCH (repo:Repository)-[:DEFINES]->(workload:Workload {id: $workload_id})"):
					return []map[string]any{workload["cloud_resources"].([]map[string]any)[0]}, nil
				default:
					return nil, nil
				}
			},
		},
		Content: querytestutil.FakePortContentStore{},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api"}`),
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
	if got, want := len(querycontract.MapSliceValue(body, "cloud_resources")), 1; got != want {
		t.Fatalf("cloud_resources len = %d, want %d; body = %#v", got, want, body)
	}
	if limits := querycontract.MapValue(body, "cloud_resource_limits"); len(limits) != 0 {
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
		Neo4j: querytestutil.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			RunFn: func(_ context.Context, _ string, _ map[string]any) ([]map[string]any, error) {
				return nil, nil
			},
		},
		Content: querytestutil.FakePortContentStore{},
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/impact/trace-deployment-chain",
		strings.NewReader(`{"service_name":"orders-api"}`),
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
	limits := querycontract.MapValue(body, "cloud_resource_limits")
	if got, want := querycontract.IntVal(limits, "limit"), querycontract.ServiceStoryItemLimit; got != want {
		t.Fatalf("cloud_resource_limits.limit = %d, want %d; limits = %#v", got, want, limits)
	}
	if got := querycontract.IntVal(limits, "returned_count"); got != 0 {
		t.Fatalf("cloud_resource_limits.returned_count = %d, want 0", got)
	}
	if querycontract.BoolVal(limits, "truncated") || querycontract.BoolVal(limits, "observed_count_is_lower_bound") {
		t.Fatalf("cloud_resource_limits = %#v, want exact empty coverage", limits)
	}
}
