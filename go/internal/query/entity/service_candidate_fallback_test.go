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
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestGetServiceContextTriesNextAdmittedCandidate covers #6801 review
// F-R5-4. Two same-name workloads are admitted by the candidate read's
// DEFINES ids. The scoped DEFINES re-check finds no granted repository for
// the lowest id (the two reads disagree, i.e. backend drift) but does for
// the next one. The lookup must return the next admitted workload instead of
// ending in 404 after the first rejection.
func TestGetServiceContextTriesNextAdmittedCandidate(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	graph := fallbackCandidateGraph(t, "workload:api-2")
	handler := &Handler{Neo4j: graph, Instruments: instruments}
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
		t.Fatalf("id = %q, want the next admitted workload %q", got, want)
	}
	if got, want := querycontract.StringVal(body, "repo_id"), "repo-a"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	if points := scopedGrantDeniedDataPoints(t, reader); len(points) != 0 {
		t.Fatalf("grant_denied data points = %+v, want none when a later candidate is admitted", points)
	}
}

// TestGetServiceContextAllCandidatesRejectedCountsOneDenial pins the other
// side of #6801 review F-R9-2: when the DEFINES re-check rejects every
// admitted candidate, the request is a 404 and exactly one grant_denied.
func TestGetServiceContextAllCandidatesRejectedCountsOneDenial(t *testing.T) {
	t.Parallel()

	instruments, reader := newTestInstruments(t)
	handler := &Handler{Neo4j: fallbackCandidateGraph(t, ""), Instruments: instruments}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/api/context", nil).WithContext(scopedRepoAContext())
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, req)

	if got, want := recorder.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, recorder.Body.String())
	}
	points := scopedGrantDeniedDataPoints(t, reader)
	if len(points) != 1 || points[0].Value != 1 {
		t.Fatalf("grant_denied data points = %+v, want exactly one count", points)
	}
}

// fallbackCandidateGraph answers the service-context reads for two
// same-name workloads that the candidate read admits through DEFINES ids.
// The scoped DEFINES re-check resolves repo-a only for resolvableID ("" for
// none), simulating the two reads disagreeing.
func fallbackCandidateGraph(t *testing.T, resolvableID string) querytestutil.FakeGraphReader {
	t.Helper()
	candidates := []map[string]any{
		{"id": "workload:api-1", "name": "api", "kind": "service", "repo_id": "repo-x", "defining": []any{"repo-a"}},
		{"id": "workload:api-2", "name": "api", "kind": "service", "repo_id": "repo-x", "defining": []any{"repo-a"}},
	}
	return querytestutil.FakeGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			querytestutil.AssertCypherHasNoBrokenAndOr(t, cypher)
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				if resolvableID != "" && querycontract.StringVal(params, "workload_id") == resolvableID {
					return []map[string]any{{"repo_id": "repo-a", "repo_name": "repo-a"}}, nil
				}
				return nil, nil
			case strings.Contains(cypher, "MATCH (w:Workload)") && strings.Contains(cypher, "w.name = $service_name"):
				return candidates, nil
			default:
				return nil, nil
			}
		},
	}
}
