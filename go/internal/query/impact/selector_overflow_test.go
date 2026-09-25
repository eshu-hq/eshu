// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// overflowingNameReader answers the workload selector's name lookup with
// more rows than querycontract.WorkloadSelectorCandidateBound allows.
func overflowingNameReader() graph.FakeGraphReader {
	rows := make([]map[string]any, querycontract.WorkloadSelectorCandidateBound+1)
	for i := range rows {
		rows[i] = map[string]any{"id": "workload:orders", "name": "orders", "repo_id": "repo-b", "defining": []string{}}
	}
	return graph.FakeGraphReader{RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
		if strings.Contains(cypher, "w.name = $service_name") {
			return rows, nil
		}
		return nil, nil
	}}
}

// assertSelectorOverflowConflict checks the #6786 review contract for a
// selector that matched too many workloads: a bounded 409, not a 500, and
// no row count in the body.
func assertSelectorOverflowConflict(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if got, want := recorder.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "retry with a workload id") {
		t.Fatalf("body = %s, want the fixed retry guidance", body)
	}
	if strings.Contains(body, "51") || strings.Contains(body, "exceed bound") {
		t.Fatalf("body = %s, want no candidate count", body)
	}
}

func TestTraceDeploymentChainSelectorOverflowReturnsConflictWithoutCount(t *testing.T) {
	t.Parallel()

	handler := &Handler{Neo4j: overflowingNameReader()}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/trace-deployment-chain", strings.NewReader(`{"service_name":"orders"}`))
	recorder := httptest.NewRecorder()

	handler.TraceDeploymentChain(recorder, req)

	assertSelectorOverflowConflict(t, recorder)
}

func TestDeploymentConfigInfluenceSelectorOverflowReturnsConflictWithoutCount(t *testing.T) {
	t.Parallel()

	handler := &Handler{Neo4j: overflowingNameReader()}

	recorder := requestDeploymentConfigInfluence(t, handler, `{"service_name":"orders"}`)

	assertSelectorOverflowConflict(t, recorder)
}
