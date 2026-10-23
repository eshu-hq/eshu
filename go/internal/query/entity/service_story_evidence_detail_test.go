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

func serviceStoryDetailHandler() (*Handler, *int) {
	reads := 0
	return &Handler{
		Neo4j: graph.FakeWorkloadGraphReader{
			RunSingleByMatch: map[string]map[string]any{
				"w.id = $workload_id": {
					"id": "workload:checkout-service", "name": "checkout-service", "kind": "service",
					"repo_id": "repo-checkout-service", "repo_name": "checkout-service",
				},
			},
			RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				reads++
				if strings.Contains(cypher, "w.name = $service_name") {
					return []map[string]any{{"id": "workload:checkout-service", "name": "checkout-service", "kind": "service", "repo_id": "repo-checkout-service"}}, nil
				}
				return nil, nil
			},
		},
		Profile: querycontract.ProfileProduction,
	}, &reads
}

func getServiceStoryDetail(t *testing.T, handler *Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/checkout-service/story"+query, nil)
	req.SetPathValue("service_name", "checkout-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

// TestGetServiceStoryEvidenceDetailParameter proves the story route reads
// evidence_detail the way the context routes do (#8798): absent means full,
// handles is recorded on the body, and an unknown value is a 400 before any
// graph read runs.
func TestGetServiceStoryEvidenceDetailParameter(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ query, want string }{
		{"", "full"},
		{"?evidence_detail=full", "full"},
		{"?evidence_detail=handles", "handles"},
	} {
		handler, _ := serviceStoryDetailHandler()
		w := getServiceStoryDetail(t, handler, tc.query)
		if w.Code != http.StatusOK {
			t.Fatalf("%q: status = %d, want 200; body = %s", tc.query, w.Code, w.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%q: json.Unmarshal: %v", tc.query, err)
		}
		if got := querycontract.StringVal(body, "evidence_detail"); got != tc.want {
			t.Fatalf("%q: evidence_detail = %q, want %q", tc.query, got, tc.want)
		}
	}

	handler, reads := serviceStoryDetailHandler()
	w := getServiceStoryDetail(t, handler, "?evidence_detail=compact")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("unknown evidence_detail status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if *reads != 0 {
		t.Fatalf("graph reads = %d before the evidence_detail 400, want 0", *reads)
	}
}
