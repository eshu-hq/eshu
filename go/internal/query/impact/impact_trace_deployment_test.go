// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestTraceDeploymentChainReturnsConflictForDuplicateWorkloadName(t *testing.T) {
	t.Parallel()

	call := 0
	reader := querytestutil.FakeGraphReader{RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		if strings.Contains(cypher, "w.id = $service_name") {
			return nil, nil
		}
		if strings.Contains(cypher, "w.name = $service_name") {
			call++
			return map[string]any{"id": "workload:orders-" + string(rune('a'+call-1))}, nil
		}
		return nil, nil
	}}
	handler := &ImpactHandler{Neo4j: reader}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"orders"}`))
	recorder := httptest.NewRecorder()

	handler.TraceDeploymentChain(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestTraceDeploymentChainClampsAbsurdMaxDepthInsteadOfRejecting(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		maxDepth       int
		wantTraceLimit int
	}{
		{
			name:           "negative max_depth 200s and falls through to the operator-safe default limit",
			maxDepth:       -1,
			wantTraceLimit: querycontract.DefaultIndirectEvidenceSearchLimit,
		},
		{
			name:           "overflow-inducing max_depth 200s and resolves to the saturated package-cap limit",
			maxDepth:       922337203685477581,
			wantTraceLimit: querycontract.MaxIndirectEvidenceSearchLimit,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			workload := map[string]any{
				"id":        "workload:orders-api",
				"instances": []any{},
				"kind":      "service",
				"name":      "orders-api",
				"repo_id":   "repo-orders",
				"repo_name": "orders-api",
			}
			// Empty in-memory content: the SQL decoding layer stays covered by the
			// root content_reader tests; impact/ tests cannot import package
			// query. See #6060.
			var sawProvisioningQuery bool
			var gotLimit any
			handler := &ImpactHandler{
				Neo4j: querytestutil.FakeWorkloadGraphReader{
					RunSingleByMatch: map[string]map[string]any{
						"w.name = $service_name": workload,
						"w.id = $workload_id":    workload,
					},
					RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
						if strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM") {
							sawProvisioningQuery = true
							gotLimit = params["limit"]
							return nil, nil
						}
						if strings.Contains(cypher, "DEFINES]-(r:Repository)") {
							return []map[string]any{{"repo_id": "repo-orders", "repo_name": "orders-api"}}, nil
						}
						return nil, nil
					},
				},
				Content: &querytestutil.FakePortContentStore{},
			}

			body := fmt.Sprintf(`{"service_name":"orders-api","max_depth":%d}`, tc.maxDepth)
			req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(body))
			recorder := httptest.NewRecorder()

			handler.TraceDeploymentChain(recorder, req)

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			if !sawProvisioningQuery {
				t.Fatal("provisioning candidates query never ran; cannot observe the clamped max_depth")
			}
			// #5720 round-2 P1-1: queryProvisioningRepositoryCandidates now
			// probes one row past the disclosed limit to detect truncation,
			// so the wire-visible bound is wantTraceLimit+1.
			if wantWireLimit := tc.wantTraceLimit + 1; gotLimit != wantWireLimit {
				t.Fatalf("provisioning candidates limit = %#v, want %d (max_depth=%d must clamp, not reject)", gotLimit, wantWireLimit, tc.maxDepth)
			}
		})
	}
}

func TestNormalizeTraceDeploymentChainMaxDepth(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		maxDepth int
		want     int
	}{
		{name: "negative clamps to zero", maxDepth: -1, want: 0},
		{name: "math.MinInt clamps to zero", maxDepth: math.MinInt, want: 0},
		{name: "zero passes through unchanged", maxDepth: 0, want: 0},
		{name: "at the limit passes through unchanged", maxDepth: 1000, want: traceDeploymentChainMaxDepthLimit},
		{name: "just above the limit clamps down", maxDepth: 1001, want: traceDeploymentChainMaxDepthLimit},
		{name: "overflow-scale value clamps down", maxDepth: 922337203685477581, want: traceDeploymentChainMaxDepthLimit},
		{name: "math.MaxInt clamps down", maxDepth: math.MaxInt, want: traceDeploymentChainMaxDepthLimit},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeTraceDeploymentChainMaxDepth(tc.maxDepth); got != tc.want {
				t.Fatalf("normalizeTraceDeploymentChainMaxDepth(%d) = %d, want %d", tc.maxDepth, got, tc.want)
			}
		})
	}
}
