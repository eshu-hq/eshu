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

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
	"github.com/eshu-hq/eshu/go/internal/query/repository"
)

// getServiceContextReadModelPartialReasons drives the real mounted GET
// /api/v0/services/{name}/context route with no materialized graph Workload
// node, forcing FetchServiceReadModelWorkloadContext's read-model fallback
// (workload_context.go around lines 280-285). dependenciesErr, when set,
// fails that fallback's repository.QueryRepoDependencies call.
func getServiceContextReadModelPartialReasons(t *testing.T, dependenciesErr error) map[string]any {
	t.Helper()

	content := querytestutil.FakePortContentStore{
		Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-1", Name: "order-service"}},
		Summary: querycontract.RepositoryReadModelSummary{
			Available:     true,
			WorkloadNames: []string{"order-service"},
		},
	}
	// No RunSingleByMatch/RunByMatch entries at all: every graph lookup
	// fetchServiceWorkloadContext tries (w.name = $service_name, then
	// w.id = $service_name) returns nil, which forces the fallback into
	// FetchServiceReadModelWorkloadContext in the first place (mirrors
	// TestGetServiceContextReadModelFallbackDisclosesInfrastructureTruncated).
	reader := querytestutil.FakeWorkloadGraphReader{
		RunFn: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
			if strings.Contains(cypher, "AS target_name,") {
				if dependenciesErr != nil {
					return nil, dependenciesErr
				}
				return []map[string]any{}, nil
			}
			return nil, nil
		},
	}
	handler := &Handler{Neo4j: reader, Content: content}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/services/order-service/context", nil)
	req.SetPathValue("service_name", "order-service")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := querycontract.StringVal(body, "materialization_status"), "identity_only"; got != want {
		t.Fatalf(
			"materialization_status = %q, want %q (the request must actually reach the read-model fallback, not the graph-backed path)",
			got, want,
		)
	}
	return body
}

// TestGetServiceContextReadModelFallbackReportsRelationshipsReadDegraded is
// the #6810 regression for the read-model-only repository.QueryRepoDependencies
// caller in FetchServiceReadModelWorkloadContext (workload_context.go around
// lines 280-285): a failed dependencies read on the fallback path must also
// surface as relationships_read_degraded in partial_reasons.
func TestGetServiceContextReadModelFallbackReportsRelationshipsReadDegraded(t *testing.T) {
	t.Parallel()

	body := getServiceContextReadModelPartialReasons(t, errors.New("graph query exceeded its deadline"))
	reasons := querytestutil.RequireStringAnySlice(t, body, "partial_reasons")
	if !querytestutil.AnySliceContains(reasons, repository.RelationshipsReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want %q", reasons, repository.RelationshipsReadDegradedReason)
	}
}

// TestGetServiceContextReadModelFallbackHealthyDependenciesReadAddsNoReason is
// the other half: a healthy, empty dependencies read on the read-model
// fallback path must not add the reason.
func TestGetServiceContextReadModelFallbackHealthyDependenciesReadAddsNoReason(t *testing.T) {
	t.Parallel()

	body := getServiceContextReadModelPartialReasons(t, nil)
	reasons := querytestutil.RequireStringAnySlice(t, body, "partial_reasons")
	if querytestutil.AnySliceContains(reasons, repository.RelationshipsReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want no %q for a healthy empty read", reasons, repository.RelationshipsReadDegradedReason)
	}
}
