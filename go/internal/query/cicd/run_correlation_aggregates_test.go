// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type stubRunCorrelationAggregateStore struct {
	count         RunCorrelationAggregateCount
	countErr      error
	inventory     []RunCorrelationInventoryRow
	inventoryErr  error
	lastFilter    RunCorrelationAggregateFilter
	lastDimension RunCorrelationInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubRunCorrelationAggregateStore) CountRunCorrelations(
	_ context.Context,
	filter RunCorrelationAggregateFilter,
) (RunCorrelationAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return RunCorrelationAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubRunCorrelationAggregateStore) RunCorrelationInventory(
	_ context.Context,
	filter RunCorrelationAggregateFilter,
	dim RunCorrelationInventoryDimension,
	limit int,
	offset int,
) ([]RunCorrelationInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]RunCorrelationInventoryRow(nil), s.inventory...), nil
}

func TestRunCorrelationAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &Handler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/ci-cd/run-correlations/count",
		"/api/v0/ci-cd/run-correlations/inventory",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusServiceUnavailable; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestRunCorrelationAggregateCountReturnsRollups(t *testing.T) {
	t.Parallel()

	store := &stubRunCorrelationAggregateStore{
		count: RunCorrelationAggregateCount{
			TotalCorrelations: 15,
			ByOutcome:         map[string]int{"exact": 10, "derived": 3, "ambiguous": 2},
			ByEnvironment:     map[string]int{"production": 6, "staging": 5, "development": 4},
			ByProvider:        map[string]int{"github_actions": 11, "gitlab": 4},
		},
	}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/count?repository_id=repo-A", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.RepositoryID, "repo-A"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	var body struct {
		TotalCorrelations int            `json:"total_correlations"`
		ByOutcome         map[string]int `json:"by_outcome"`
		ByEnvironment     map[string]int `json:"by_environment"`
		ByProvider        map[string]int `json:"by_provider"`
		Scope             map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalCorrelations != 15 {
		t.Fatalf("total_correlations = %d, want 15; body = %s", body.TotalCorrelations, w.Body.String())
	}
	if body.ByOutcome["exact"] != 10 {
		t.Fatalf("by_outcome[exact] = %d, want 10", body.ByOutcome["exact"])
	}
	if body.ByEnvironment["production"] != 6 {
		t.Fatalf("by_environment[production] = %d, want 6", body.ByEnvironment["production"])
	}
	if body.ByProvider["github_actions"] != 11 {
		t.Fatalf("by_provider[github_actions] = %d, want 11", body.ByProvider["github_actions"])
	}
	if body.Scope["repository_id"] != "repo-A" {
		t.Fatalf("scope.repository_id = %v, want repo-A", body.Scope["repository_id"])
	}
}

func TestRunCorrelationAggregateCountPassesImageRefFilter(t *testing.T) {
	t.Parallel()

	store := &stubRunCorrelationAggregateStore{}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations/count?image_ref=registry.example.com/team/api:prod",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.ImageRef, "registry.example.com/team/api:prod"; got != want {
		t.Fatalf("ImageRef = %q, want %q", got, want)
	}
}

func TestRunCorrelationAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubRunCorrelationAggregateStore{
		inventory: []RunCorrelationInventoryRow{
			{Dimension: RunCorrelationInventoryByOutcome, Value: "exact", Count: 30},
			{Dimension: RunCorrelationInventoryByOutcome, Value: "derived", Count: 8},
			{Dimension: RunCorrelationInventoryByOutcome, Value: "ambiguous", Count: 2},
		},
	}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/inventory?group_by=outcome&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != RunCorrelationInventoryByOutcome {
		t.Fatalf("dimension = %q, want outcome", store.lastDimension)
	}
	if store.lastLimit != 11 {
		t.Fatalf("internal limit = %d, want 11 (caller limit + 1)", store.lastLimit)
	}
	var body struct {
		Count     int    `json:"count"`
		GroupBy   string `json:"group_by"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.Count != 3 {
		t.Fatalf("count = %d, want 3", body.Count)
	}
	if body.GroupBy != "outcome" {
		t.Fatalf("group_by = %q, want outcome", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 3 buckets, limit 10)")
	}
}

func TestRunCorrelationAggregateInventoryPassesImageRefFilter(t *testing.T) {
	t.Parallel()

	store := &stubRunCorrelationAggregateStore{}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations/inventory?image_ref=registry.example.com/team/api:prod&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.ImageRef, "registry.example.com/team/api:prod"; got != want {
		t.Fatalf("ImageRef = %q, want %q", got, want)
	}
}

func TestRunCorrelationAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]RunCorrelationInventoryRow, 6)
	for i := range rows {
		rows[i] = RunCorrelationInventoryRow{
			Dimension: RunCorrelationInventoryByEnvironment,
			Value:     "env",
			Count:     i,
		}
	}
	store := &stubRunCorrelationAggregateStore{inventory: rows}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/inventory?group_by=environment&limit=5", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		Count      int  `json:"count"`
		Truncated  bool `json:"truncated"`
		NextOffset int  `json:"next_offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.Count != 5 {
		t.Fatalf("count = %d, want 5 (page trim)", body.Count)
	}
	if !body.Truncated {
		t.Fatalf("truncated = false, want true")
	}
	if body.NextOffset != 5 {
		t.Fatalf("next_offset = %d, want 5", body.NextOffset)
	}
}

func TestRunCorrelationAggregateRejectsUnknownOutcome(t *testing.T) {
	t.Parallel()

	store := &stubRunCorrelationAggregateStore{}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// `success` is a typo — the OpenAPI enum (and the new validator) accept
	// only exact / derived / ambiguous / unresolved / rejected. Both
	// aggregate endpoints must surface that as a 400.
	for _, target := range []string{
		"/api/v0/ci-cd/run-correlations/count?outcome=success",
		"/api/v0/ci-cd/run-correlations/inventory?outcome=success",
	} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
	if store.countCalls != 0 || store.invCalls != 0 {
		t.Fatalf("store called for unknown outcome (countCalls=%d invCalls=%d)",
			store.countCalls, store.invCalls)
	}
}

func TestRunCorrelationAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubRunCorrelationAggregateStore{}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/inventory?group_by=ecosystem", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.invCalls != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestRunCorrelationAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &Handler{Aggregates: &stubRunCorrelationAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestRunCorrelationAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &Handler{Aggregates: &stubRunCorrelationAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestRunCorrelationAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &Handler{Aggregates: &stubRunCorrelationAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestRunCorrelationAggregateInventoryNullsNextOffsetAtCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]RunCorrelationInventoryRow, 6)
	for i := range rows {
		rows[i] = RunCorrelationInventoryRow{
			Dimension: RunCorrelationInventoryByRepository,
			Value:     "repo",
			Count:     i,
		}
	}
	store := &stubRunCorrelationAggregateStore{inventory: rows}
	handler := &Handler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/ci-cd/run-correlations/inventory?group_by=repository_id&limit=5&offset=10000", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var body struct {
		Truncated  bool `json:"truncated"`
		NextOffset *int `json:"next_offset"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if !body.Truncated {
		t.Fatalf("truncated = false, want true")
	}
	if body.NextOffset != nil {
		t.Fatalf("next_offset = %d, want null when offset+limit exceeds documented max", *body.NextOffset)
	}
}

func TestNextRunCorrelationAggregateOffsetBound(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		offset    int
		limit     int
		truncated bool
		want      any
	}{
		{"not truncated returns nil", 0, 100, false, nil},
		{"normal next offset", 200, 100, true, 300},
		{"exactly at ceiling boundary returns ceiling", 9900, 100, true, 10000},
		{"would exceed ceiling returns nil", 9950, 100, true, nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := nextRunCorrelationAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextRunCorrelationAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestRunCorrelationInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []RunCorrelationInventoryDimension{
		RunCorrelationInventoryByOutcome,
		RunCorrelationInventoryByEnvironment,
		RunCorrelationInventoryByRepository,
		RunCorrelationInventoryByProvider,
	}
	for _, dim := range cases {
		if _, err := cicdRunCorrelationInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := cicdRunCorrelationInventoryGroupExpression("ecosystem"); err == nil {
		t.Fatal("cicdRunCorrelationInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}
