// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// TestGetServiceContextCarriesResultLimits proves the service context route
// emits the same result_limits drilldown block as the workload context route,
// including the hostname and entrypoint totals, so the shared WorkloadContext
// schema promise holds on both (#7169).
func TestGetServiceContextCarriesResultLimits(t *testing.T) {
	t.Parallel()

	handler := &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, "collect(DISTINCT dr.id) as defining"):
					return []map[string]any{{
						"id":        "workload:service-edge-api",
						"name":      "service-edge-api",
						"kind":      "Deployment",
						"repo_id":   "repo-1",
						"repo_name": "service-edge-api",
						"instances": []any{},
					}}, nil
				case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
					return []map[string]any{{"repo_id": "repo-1", "repo_name": "service-edge-api"}}, nil
				default:
					return nil, nil
				}
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/service-edge-api/context", nil)
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
	limits := querycontract.MapValue(resp, "result_limits")
	if limits == nil {
		t.Fatalf("result_limits missing from service context response: %s", w.Body.String())
	}
	if got, want := querycontract.IntVal(limits, "limit"), querycontract.ContextStoryItemLimit; got != want {
		t.Fatalf("result_limits.limit = %d, want %d", got, want)
	}
	for _, key := range []string{"hostname_count", "entrypoint_count"} {
		if _, ok := limits[key]; !ok {
			t.Fatalf("result_limits.%s missing: %#v", key, limits)
		}
	}
}
