// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCountRunCorrelationsReturnsRollups drives countRunCorrelations directly
// through Mount/ServeHTTP and asserts a basic 200 with the documented rollup
// shape. countRunCorrelations is Postgres-backed, not graph-backed: its
// repository_id filter resolves through
// cicdRunCorrelationAggregateFilterFromRequest, which passes a literal nil
// graph to resolveRepositorySelectorForRequestWithAccess, so there is no live
// graph read on this route to sweep with the unavailable/deadline sentinels
// the way the other four count handlers are (see
// graph_read_error_*_test.go). This test exists to satisfy
// scripts/verify-route-coverage.sh's CountRunCorrelations naming check; the
// route's richer behavior is already covered by
// TestCICDRunCorrelationAggregateCountReturnsRollups above.
func TestCountRunCorrelationsReturnsRollups(t *testing.T) {
	t.Parallel()

	store := &stubCICDRunCorrelationAggregateStore{
		count: CICDRunCorrelationAggregateCount{
			TotalCorrelations: 4,
			ByOutcome:         map[string]int{"exact": 4},
			ByEnvironment:     map[string]int{"production": 4},
			ByProvider:        map[string]int{"github_actions": 4},
		},
	}
	handler := &CICDHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/count", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	var body struct {
		TotalCorrelations int            `json:"total_correlations"`
		ByOutcome         map[string]int `json:"by_outcome"`
		ByEnvironment     map[string]int `json:"by_environment"`
		ByProvider        map[string]int `json:"by_provider"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalCorrelations != 4 {
		t.Fatalf("total_correlations = %d, want 4; body = %s", body.TotalCorrelations, w.Body.String())
	}
	if body.ByOutcome["exact"] != 4 {
		t.Fatalf("by_outcome[exact] = %d, want 4", body.ByOutcome["exact"])
	}
	if body.ByEnvironment["production"] != 4 {
		t.Fatalf("by_environment[production] = %d, want 4", body.ByEnvironment["production"])
	}
	if body.ByProvider["github_actions"] != 4 {
		t.Fatalf("by_provider[github_actions] = %d, want 4", body.ByProvider["github_actions"])
	}
}
