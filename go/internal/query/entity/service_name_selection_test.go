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

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil/graph"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// sameNameWorkloadRows returns two workloads named "api": the first, by id
// order, belongs to ungranted repo-b and the second to granted repo-a.
func sameNameWorkloadRows() []map[string]any {
	return []map[string]any{
		{"id": "workload:api-1", "name": "api", "kind": "service", "repo_id": "repo-b", "defining": []any{"repo-b"}},
		{"id": "workload:api-2", "name": "api", "kind": "service", "repo_id": "repo-a", "defining": []any{"repo-a"}},
	}
}

// sameNameWorkloadGraph answers the service-context reads for two
// same-name workloads. RunSingle hands back the ungranted row for any
// name-keyed Workload read, the way an unordered `LIMIT 1` can on a real
// backend; Run answers the bounded candidate read with both rows.
func sameNameWorkloadGraph(t *testing.T, nameRows []map[string]any) graph.FakeGraphReader {
	t.Helper()
	return graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			if strings.Contains(cypher, "MATCH (w:Workload)") && strings.Contains(cypher, "w.name = $service_name") {
				return nameRows[0], nil
			}
			return nil, nil
		},
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			graph.AssertCypherHasNoBrokenAndOr(t, cypher)
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				for _, row := range nameRows {
					if querycontract.StringVal(row, "id") == querycontract.StringVal(params, "workload_id") {
						repoID := querycontract.StringVal(row, "repo_id")
						return []map[string]any{{"repo_id": repoID, "repo_name": repoID}}, nil
					}
				}
				return nil, nil
			case strings.Contains(cypher, "MATCH (w:Workload)") && strings.Contains(cypher, "w.name = $service_name"):
				return nameRows, nil
			default:
				return nil, nil
			}
		},
	}
}

func scopedRepoAContext() context.Context {
	return auth.ContextWithAuthContext(context.Background(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		AllowedRepositoryIDs: []string{"repo-a"},
	})
}

// TestGetServiceContextNameCollisionReturnsGrantedWorkload covers the #6786
// review P1: with two workloads named "api", a caller granted only repo-a
// must get repo-a's workload, not a 404 because an unordered single-row read
// happened to return repo-b's.
func TestGetServiceContextNameCollisionReturnsGrantedWorkload(t *testing.T) {
	t.Parallel()

	handler := &Handler{Neo4j: sameNameWorkloadGraph(t, sameNameWorkloadRows())}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/api/context", nil).WithContext(scopedRepoAContext())
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := querycontract.StringVal(body, "id"), "workload:api-2"; got != want {
		t.Fatalf("id = %q, want granted workload %q", got, want)
	}
	if got, want := querycontract.StringVal(body, "repo_id"), "repo-a"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
}

// TestGetServiceContextNameOverflowReturnsConflictWithoutCount covers the
// #6786 review finding on the overflow path: a bounded 409 with no count.
func TestGetServiceContextNameOverflowReturnsConflictWithoutCount(t *testing.T) {
	t.Parallel()

	rows := make([]map[string]any, querycontract.WorkloadSelectorCandidateBound+1)
	for i := range rows {
		rows[i] = map[string]any{"id": "workload:api", "name": "api", "kind": "service", "repo_id": "repo-a", "defining": []any{"repo-a"}}
	}
	handler := &Handler{Neo4j: sameNameWorkloadGraph(t, rows)}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/api/context", nil).WithContext(scopedRepoAContext())
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusConflict; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, "51") || !strings.Contains(body, "retry with a workload id") {
		t.Fatalf("body = %s, want fixed retry guidance and no candidate count", body)
	}
}

// TestFetchServiceWorkloadContextNameDenialThenIDAdmitCountsNoDenial covers
// the #6786 review finding on the name-then-id fallback: the name lookup
// finds only an ungranted workload, the id lookup finds a granted one. The
// request succeeds, so no grant_denied may be counted.
func TestFetchServiceWorkloadContextNameDenialThenIDAdmitCountsNoDenial(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			switch {
			case strings.Contains(cypher, "w.name = $service_name"):
				return map[string]any{"id": "workload:legacy", "name": "workload:api", "repo_id": "repo-b"}, nil
			case strings.Contains(cypher, "w.id = $service_name"):
				return map[string]any{"id": "workload:api", "name": "api", "repo_id": "repo-a"}, nil
			default:
				return nil, nil
			}
		},
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "<-[:DEFINES]-(r:Repository)"):
				if querycontract.StringVal(params, "workload_id") == "workload:api" {
					return []map[string]any{{"repo_id": "repo-a", "repo_name": "repo-a"}}, nil
				}
				return nil, nil
			case strings.Contains(cypher, "w.name = $service_name"):
				return []map[string]any{{"id": "workload:legacy", "name": "workload:api", "repo_id": "repo-b", "defining": []any{}}}, nil
			default:
				return nil, nil
			}
		},
	}
	handler := &Handler{Neo4j: graph, Instruments: instruments}

	got, err := handler.fetchServiceWorkloadContext(scopedRepoAContext(), "workload:api", "service_context")
	if err != nil {
		t.Fatalf("fetchServiceWorkloadContext() error = %v", err)
	}
	if got, want := querycontract.StringVal(got, "id"), "workload:api"; got != want {
		t.Fatalf("id = %q, want %q", got, want)
	}
	if points := scopedGrantDeniedDataPoints(t, reader); len(points) != 0 {
		t.Fatalf("%s data points = %+v, want none when the id lookup admitted a workload", queryScopedGrantDeniedMetric, points)
	}
}

// TestFetchServiceWorkloadContextDeniedByAllLookupsCountsOneDenial pins
// that a request denied by both the name and id lookups counts one denial.
func TestFetchServiceWorkloadContextDeniedByAllLookupsCountsOneDenial(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := graph.FakeGraphReader{
		RunSingleFn: func(_ context.Context, cypher string, _ map[string]any) (map[string]any, error) {
			switch {
			case strings.Contains(cypher, "w.name = $service_name"):
				return map[string]any{"id": "workload:legacy", "name": "workload:api", "repo_id": "repo-b"}, nil
			case strings.Contains(cypher, "w.id = $service_name"):
				return map[string]any{"id": "workload:api", "name": "api", "repo_id": "repo-c"}, nil
			default:
				return nil, nil
			}
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "w.name = $service_name") && !strings.Contains(cypher, "<-[:DEFINES]-") {
				return []map[string]any{{"id": "workload:legacy", "name": "workload:api", "repo_id": "repo-b", "defining": []any{}}}, nil
			}
			return nil, nil
		},
	}
	handler := &Handler{Neo4j: graph, Instruments: instruments}

	got, err := handler.fetchServiceWorkloadContext(scopedRepoAContext(), "workload:api", "service_context")
	if err != nil || got != nil {
		t.Fatalf("fetchServiceWorkloadContext() = %#v, %v, want not-found", got, err)
	}
	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 || points[0].Value != 1 {
		t.Fatalf("%s data points = %+v, want exactly one denial", queryScopedGrantDeniedMetric, points)
	}
	if got, want := attrString(t, points[0], telemetry.MetricDimensionReason), "grant_denied"; got != want {
		t.Fatalf("%s reason = %q, want %q", queryScopedGrantDeniedMetric, got, want)
	}
}
