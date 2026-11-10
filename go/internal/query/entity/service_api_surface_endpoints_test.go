// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// endpointListReader fakes the whole read chain behind
// GetServiceAPISurfaceEndpoints: workload resolution, the repository endpoint
// count and paged detail, and the provisioning-candidate fan-out the
// enrichment discloses. Unmatched queries answer empty so the test fails on
// the response assertions, not on fake coverage.
func endpointListReader(endpointCount, candidateCount int) graph.FakeWorkloadGraphReader {
	endpoints := make([]map[string]any, 0, endpointCount)
	for i := range endpointCount {
		endpoints = append(endpoints, map[string]any{
			"endpoint_id": fmt.Sprintf("endpoint-%03d", i),
			"path":        fmt.Sprintf("/v1/resource-%03d", i),
			"methods":     []any{"GET"},
		})
	}
	candidates := make([]map[string]any, 0, candidateCount)
	for i := range candidateCount {
		candidates = append(candidates, map[string]any{
			"repo_id":             fmt.Sprintf("repository:consumer-%02d", i),
			"repo_name":           fmt.Sprintf("consumer-%02d", i),
			"relationship_type":   "USES_MODULE",
			"relationship_reason": "terraform_module_source_path",
		})
	}
	workload := map[string]any{
		"id":      "workload:paged-api",
		"name":    "paged-api",
		"kind":    "service",
		"repo_id": "repo-paged",
	}
	return graph.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "endpoint.id AS endpoint_id"):
				limit, _ := params["limit"].(int)
				offset, _ := params["offset"].(int)
				if offset < 0 {
					offset = 0
				}
				if offset > len(endpoints) {
					offset = len(endpoints)
				}
				end := len(endpoints)
				if limit > 0 && offset+limit < end {
					end = offset + limit
				}
				return append([]map[string]any(nil), endpoints[offset:end]...), nil
			case strings.Contains(cypher, "count(endpoint) AS endpoint_count"):
				return []map[string]any{{"endpoint_count": len(endpoints)}}, nil
			case strings.Contains(cypher, "PROVISIONS_DEPENDENCY_FOR|DEPLOYS_FROM|USES_MODULE|DISCOVERS_CONFIG_IN|READS_CONFIG_FROM"):
				return candidates, nil
			case strings.Contains(cypher, "ORDER BY repo_id, id"):
				return []map[string]any{{
					"id":          "workload:paged-api",
					"name":        "paged-api",
					"kind":        "service",
					"repo_id":     "repo-paged",
					"environment": "",
				}}, nil
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-paged", "repo_name": "paged-api"}}, nil
			default:
				return nil, nil
			}
		},
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (w:Workload) WHERE w.id = $workload_id") {
				return workload, nil
			}
			return nil, nil
		},
	}
}

func serveServiceEndpoints(t *testing.T, reader graph.FakeWorkloadGraphReader, target string) (int, map[string]any) {
	t.Helper()
	handler := &Handler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.SetPathValue("service_name", "paged-api")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("GET %s: unmarshal: %v (body %s)", target, err, w.Body.String())
	}
	return w.Code, resp
}

// TestGetServiceAPISurfaceEndpointsPagesThroughHandler is the #10477 route
// coverage for the dedicated endpoint list: the real handler resolves the
// service, pages the ordered set at the read, and terminates the pages.
func TestGetServiceAPISurfaceEndpointsPagesThroughHandler(t *testing.T) {
	t.Parallel()

	reader := endpointListReader(60, 0)
	var got int
	offset := 0
	for {
		code, resp := serveServiceEndpoints(t, reader, fmt.Sprintf("/api/v0/services/paged-api/api-surface/endpoints?limit=25&offset=%d", offset))
		if code != http.StatusOK {
			t.Fatalf("GET endpoints offset=%d: status = %d, want 200 (body %v)", offset, code, resp)
		}
		if total, _ := resp["endpoint_count"].(float64); total != 60 {
			t.Fatalf("offset %d: endpoint_count = %v, want 60", offset, resp["endpoint_count"])
		}
		rows, _ := resp["endpoints"].([]any)
		got += len(rows)
		next, present := resp["next_offset"]
		if !present || next == nil {
			break
		}
		offset = int(next.(float64))
		if got > 60 {
			t.Fatal("paging did not terminate")
		}
	}
	if got != 60 {
		t.Fatalf("paged endpoints = %d rows, want 60", got)
	}
}

// TestGetServiceAPISurfaceEndpointsRejectsBadPaging pins the 400s for the
// handler's own limit/offset validation, before any graph read runs.
func TestGetServiceAPISurfaceEndpointsRejectsBadPaging(t *testing.T) {
	t.Parallel()

	reader := endpointListReader(60, 0)
	for _, target := range []string{
		"/api/v0/services/paged-api/api-surface/endpoints?limit=0",
		"/api/v0/services/paged-api/api-surface/endpoints?limit=-2",
		"/api/v0/services/paged-api/api-surface/endpoints?limit=nope",
		"/api/v0/services/paged-api/api-surface/endpoints?offset=-1",
		"/api/v0/services/paged-api/api-surface/endpoints?offset=nope",
	} {
		if code, resp := serveServiceEndpoints(t, reader, target); code != http.StatusBadRequest {
			t.Fatalf("GET %s: status = %d, want 400 (body %v)", target, code, resp)
		}
	}
}
