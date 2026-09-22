// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// workloadDependenciesGraphReader answers the base workload lookup and the
// owning-repository DEFINES lookup the same way
// TestGetWorkloadContextReturnsEnrichedResponse does, and lets a test decide
// whether the repository-dependencies read (workload_context.go's first
// repository.QueryRepoDependencies call, around line 155) succeeds or fails.
func workloadDependenciesGraphReader(dependenciesErr error) querytestutil.FakeWorkloadGraphReader {
	return querytestutil.FakeWorkloadGraphReader{
		RunSingleByMatch: map[string]map[string]any{
			"MATCH (w:Workload)": {
				"id":        "workload-1",
				"name":      "order-service",
				"kind":      "Deployment",
				"repo_id":   "repo-1",
				"repo_name": "order-service",
				"instances": []any{},
			},
		},
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				return []map[string]any{{"repo_id": "repo-1", "repo_name": "order-service"}}, nil
			case strings.Contains(cypher, "AS target_name,"):
				if dependenciesErr != nil {
					return nil, dependenciesErr
				}
				return []map[string]any{}, nil
			default:
				return nil, nil
			}
		},
	}
}

// getWorkloadContextPartialReasons drives the real mounted GET
// /api/v0/workloads/{id}/context route and returns the decoded
// partial_reasons slice.
func getWorkloadContextPartialReasons(t *testing.T, dependenciesErr error) []any {
	t.Helper()

	handler := &Handler{Neo4j: workloadDependenciesGraphReader(dependenciesErr)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/workloads/workload-1/context", nil)
	req.SetPathValue("workload_id", "workload-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return querytestutil.RequireStringAnySlice(t, body, "partial_reasons")
}

// TestGetWorkloadContextReportsRelationshipsReadDegraded is the #6810
// regression for the first repository.QueryRepoDependencies caller in
// workload_context.go's graph-backed fetchWorkloadContextDecision: a failed
// dependencies read must surface as relationships_read_degraded in
// partial_reasons instead of rendering an authoritative empty dependencies
// list.
func TestGetWorkloadContextReportsRelationshipsReadDegraded(t *testing.T) {
	t.Parallel()

	reasons := getWorkloadContextPartialReasons(t, errors.New("graph query exceeded its deadline"))
	if !querytestutil.AnySliceContains(reasons, repository.RelationshipsReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want %q", reasons, repository.RelationshipsReadDegradedReason)
	}
}

// TestGetWorkloadContextHealthyDependenciesReadAddsNoReason is the other
// half: an empty-but-successful dependencies read is a true empty answer and
// must not add a reason.
func TestGetWorkloadContextHealthyDependenciesReadAddsNoReason(t *testing.T) {
	t.Parallel()

	reasons := getWorkloadContextPartialReasons(t, nil)
	if querytestutil.AnySliceContains(reasons, repository.RelationshipsReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want no %q for a healthy empty read", reasons, repository.RelationshipsReadDegradedReason)
	}
}
