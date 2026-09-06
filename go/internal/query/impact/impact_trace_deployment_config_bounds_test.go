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

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestTraceDeploymentChainDisclosesOmittedConfigAnchorWithNoCandidateRows(t *testing.T) {
	t.Parallel()

	artifacts := make([]map[string]any, 0, impacttrace.ServiceCloudResourceDependencyLimit+1)
	for index := range impacttrace.ServiceCloudResourceDependencyLimit + 1 {
		artifacts = append(artifacts, map[string]any{
			"relationship_type": "READS_CONFIG_FROM",
			"matched_value":     fmt.Sprintf("/config/%03d/*", index),
		})
	}
	body := runConfigCandidateBoundTrace(t, map[string]any{"artifacts": artifacts})
	if candidates := querycontract.MapSliceValue(body, "uncorrelated_cloud_resources"); len(candidates) != 0 {
		t.Fatalf("uncorrelated_cloud_resources = %#v, want no rows for omitted late anchor", candidates)
	}
	if !querycontract.BoolVal(body, "uncorrelated_cloud_resources_truncated") {
		t.Fatal("uncorrelated_cloud_resources_truncated = false, want omitted anchor disclosed")
	}
}

func TestTraceDeploymentChainPropagatesTruncatedDeploymentEvidenceWithNoCandidateRows(t *testing.T) {
	t.Parallel()

	body := runConfigCandidateBoundTrace(t, map[string]any{"artifacts_truncated": true})
	if !querycontract.BoolVal(body, "uncorrelated_cloud_resources_truncated") {
		t.Fatal("uncorrelated_cloud_resources_truncated = false, want upstream truncation disclosed")
	}
}

func runConfigCandidateBoundTrace(
	t *testing.T,
	deploymentEvidence map[string]any,
) map[string]any {
	t.Helper()

	workload := map[string]any{
		"deployment_evidence": deploymentEvidence,
		"id":                  "workload:orders-api",
		"instances":           []any{},
		"kind":                "service",
		"name":                "orders-api",
		"repo_id":             "repo-orders",
		"repo_name":           "orders-api",
	}
	// Empty in-memory content: the SQL decoding layer stays covered by the root
	// content_reader tests; impact/ tests cannot import package query. See #6060.
	handler := &ImpactHandler{
		Neo4j: querytestutil.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"w.name = $service_name": workload,
				"w.id = $workload_id":    workload,
			},
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "INSTANCE_OF]-(i:WorkloadInstance)-[rel:USES]->(c:CloudResource)"),
					strings.Contains(cypher, "MATCH (c:CloudResource)"),
					strings.Contains(cypher, "MATCH (n:CloudResource)"):
					return nil, nil
				default:
					return nil, nil
				}
			},
		},
		Content: &querytestutil.FakePortContentStore{},
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
	return body
}
