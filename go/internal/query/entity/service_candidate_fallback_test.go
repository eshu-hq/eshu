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

	candidates := []map[string]any{
		{"id": "workload:api-1", "name": "api", "kind": "service", "repo_id": "repo-x", "defining": []any{"repo-a"}},
		{"id": "workload:api-2", "name": "api", "kind": "service", "repo_id": "repo-x", "defining": []any{"repo-a"}},
	}
	graph := querytestutil.FakeGraphReader{
		RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
			querytestutil.AssertCypherHasNoBrokenAndOr(t, cypher)
			switch {
			case strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)"):
				if querycontract.StringVal(params, "workload_id") == "workload:api-2" {
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
	handler := &Handler{Neo4j: graph}
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
}
