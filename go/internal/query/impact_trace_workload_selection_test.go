// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTraceDeploymentChainReturnsConflictForDuplicateWorkloadName(t *testing.T) {
	t.Parallel()

	call := 0
	reader := fakeGraphReader{runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
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

	handler.traceDeploymentChain(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusConflict, recorder.Body.String())
	}
}

func TestResolveTraceWorkloadSelectorRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		switch {
		case strings.Contains(cypher, "w.id = $service_name"):
			return nil, nil
		case strings.Contains(cypher, "w.name = $service_name") && strings.Contains(cypher, "SKIP 1"):
			if !strings.Contains(cypher, "ORDER BY w.id") {
				t.Fatalf("name selector query = %q, want deterministic ambiguity probe", cypher)
			}
			return map[string]any{"id": "workload:orders-b"}, nil
		case strings.Contains(cypher, "w.name = $service_name"):
			return map[string]any{"id": "workload:orders-a"}, nil
		default:
			t.Fatalf("unexpected query: %s", cypher)
			return nil, nil
		}
	}}

	_, err := resolveTraceWorkloadSelector(t.Context(), reader, "orders")
	if !errors.Is(err, errAmbiguousTraceWorkloadSelector) {
		t.Fatalf("resolveTraceWorkloadSelector() error = %v, want ambiguity", err)
	}
}

func TestResolveTraceWorkloadSelectorPreservesExactIDLookup(t *testing.T) {
	t.Parallel()

	reader := fakeGraphReader{runSingle: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
		if !strings.Contains(cypher, "w.id = $service_name") {
			t.Fatalf("first query = %q, want exact id lookup", cypher)
		}
		return map[string]any{"id": "workload:orders"}, nil
	}}

	got, err := resolveTraceWorkloadSelector(t.Context(), reader, "workload:orders")
	if err != nil || got != "workload:orders" {
		t.Fatalf("resolveTraceWorkloadSelector() = %q, %v, want exact workload id", got, err)
	}
}
