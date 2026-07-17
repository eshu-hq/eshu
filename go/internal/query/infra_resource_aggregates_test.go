// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubInfraResourceAggregateStore struct {
	count         InfraResourceAggregateCount
	countErr      error
	inventory     []InfraResourceInventoryRow
	inventoryErr  error
	lastFilter    InfraResourceAggregateFilter
	lastDimension InfraResourceInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubInfraResourceAggregateStore) CountInfraResources(
	_ context.Context,
	filter InfraResourceAggregateFilter,
) (InfraResourceAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return InfraResourceAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubInfraResourceAggregateStore) InfraResourceInventory(
	_ context.Context,
	filter InfraResourceAggregateFilter,
	dim InfraResourceInventoryDimension,
	limit int,
	offset int,
) ([]InfraResourceInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]InfraResourceInventoryRow(nil), s.inventory...), nil
}

// stubInfraGraphQuery records every Cypher + params sent to the graph.
type stubInfraGraphQuery struct {
	responses map[string][]map[string]any
	calls     []struct {
		Cypher string
		Params map[string]any
	}
	err error
}

func (s *stubInfraGraphQuery) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	s.calls = append(s.calls, struct {
		Cypher string
		Params map[string]any
	}{Cypher: cypher, Params: params})
	if s.err != nil {
		return nil, s.err
	}
	for k, rows := range s.responses {
		if strings.Contains(cypher, k) {
			return rows, nil
		}
	}
	return nil, nil
}

func (s *stubInfraGraphQuery) RunSingle(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, errors.New("RunSingle not used by infra resource aggregates")
}

func TestInfraResourceAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/infra/resources/count",
		"/api/v0/infra/resources/inventory",
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

func TestInfraResourceAggregateCountReturnsRollups(t *testing.T) {
	t.Parallel()

	store := &stubInfraResourceAggregateStore{
		count: InfraResourceAggregateCount{
			TotalResources: 250,
			ByProvider:     map[string]int{"aws": 150, "gcp": 80, "azure": 20},
			ByEnvironment:  map[string]int{"production": 120, "staging": 80, "development": 50},
			ByLabel:        map[string]int{"TerraformResource": 180, "K8sResource": 60, "HelmChart": 10},
		},
	}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/count?category=terraform&provider=aws", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.Category, "terraform"; got != want {
		t.Fatalf("Category = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Provider, "aws"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	var body struct {
		TotalResources int            `json:"total_resources"`
		ByProvider     map[string]int `json:"by_provider"`
		ByEnvironment  map[string]int `json:"by_environment"`
		ByLabel        map[string]int `json:"by_label"`
		Scope          map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalResources != 250 {
		t.Fatalf("total_resources = %d, want 250", body.TotalResources)
	}
	if body.ByProvider["aws"] != 150 {
		t.Fatalf("by_provider[aws] = %d, want 150", body.ByProvider["aws"])
	}
	if body.ByLabel["TerraformResource"] != 180 {
		t.Fatalf("by_label[TerraformResource] = %d, want 180", body.ByLabel["TerraformResource"])
	}
	if body.Scope["category"] != "terraform" {
		t.Fatalf("scope.category = %v, want terraform", body.Scope["category"])
	}
}

func TestInfraResourceAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubInfraResourceAggregateStore{
		inventory: []InfraResourceInventoryRow{
			{Dimension: InfraResourceInventoryByProvider, Value: "aws", Count: 150},
			{Dimension: InfraResourceInventoryByProvider, Value: "gcp", Count: 80},
			{Dimension: InfraResourceInventoryByProvider, Value: "azure", Count: 20},
		},
	}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?group_by=provider&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != InfraResourceInventoryByProvider {
		t.Fatalf("dimension = %q, want provider", store.lastDimension)
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
	if body.GroupBy != "provider" {
		t.Fatalf("group_by = %q, want provider", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 3 buckets, limit 10)")
	}
}

func TestInfraResourceAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]InfraResourceInventoryRow, 6)
	for i := range rows {
		rows[i] = InfraResourceInventoryRow{
			Dimension: InfraResourceInventoryByLabel,
			Value:     "TerraformResource",
			Count:     i,
		}
	}
	store := &stubInfraResourceAggregateStore{inventory: rows}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?group_by=label&limit=5", nil)
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

func TestInfraResourceAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubInfraResourceAggregateStore{}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?group_by=account_id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.invCalls != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestInfraResourceAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Aggregates: &stubInfraResourceAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestInfraResourceAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Aggregates: &stubInfraResourceAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestInfraResourceAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &InfraHandler{Aggregates: &stubInfraResourceAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestInfraResourceAggregateInventoryNullsNextOffsetAtCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]InfraResourceInventoryRow, 6)
	for i := range rows {
		rows[i] = InfraResourceInventoryRow{
			Dimension: InfraResourceInventoryByProvider,
			Value:     "aws",
			Count:     i,
		}
	}
	store := &stubInfraResourceAggregateStore{inventory: rows}
	handler := &InfraHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/infra/resources/inventory?group_by=provider&limit=5&offset=10000", nil)
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

func TestNextInfraResourceAggregateOffsetBound(t *testing.T) {
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
			got := nextInfraResourceAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextInfraResourceAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestInfraResourceInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []InfraResourceInventoryDimension{
		InfraResourceInventoryByProvider,
		InfraResourceInventoryByEnvironment,
		InfraResourceInventoryByResourceCategory,
		InfraResourceInventoryByResourceService,
		InfraResourceInventoryByLabel,
	}
	for _, dim := range cases {
		if _, err := infraResourceInventoryGroupExpression(dim, InfraResourceAggregateFilter{}); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := infraResourceInventoryGroupExpression("account_id", InfraResourceAggregateFilter{}); err == nil {
		t.Fatal("infraResourceInventoryGroupExpression must reject unknown dimensions to keep Cypher substitution safe")
	}
}

// TestGraphInfraResourceAggregateCountShapeNarrowsToCategoryLabels proves
// the production Reader narrows the label-set when a category filter is
// passed. Without this guard, a future refactor could silently widen the
// MATCH to all labels and lose the hot-path narrowing.
func TestGraphInfraResourceAggregateCountShapeNarrowsToCategoryLabels(t *testing.T) {
	t.Parallel()

	graph := &stubInfraGraphQuery{
		responses: map[string][]map[string]any{
			"RETURN bucket_count":        {{"bucket_count": int64(42)}},
			"WHEN n.provider IS NULL":    {{"bucket": "aws", "bucket_count": int64(40)}, {"bucket": "gcp", "bucket_count": int64(2)}},
			"WHEN n.environment IS NULL": {{"bucket": "production", "bucket_count": int64(30)}},
			"head(labels(n)) AS bucket":  {{"bucket": "TerraformResource", "bucket_count": int64(42)}},
		},
	}
	store := NewGraphInfraResourceAggregateStore(graph)
	count, err := store.CountInfraResources(context.Background(), InfraResourceAggregateFilter{Category: "terraform"})
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count.TotalResources != 42 {
		t.Fatalf("TotalResources = %d, want 42", count.TotalResources)
	}
	if count.ByProvider["aws"] != 40 {
		t.Fatalf("ByProvider[aws] = %d, want 40", count.ByProvider["aws"])
	}

	if len(graph.calls) < 1 {
		t.Fatal("graph.Run never called")
	}
	first := graph.calls[0].Cypher
	if !strings.Contains(first, "TerraformResource") {
		t.Fatalf("category=terraform should narrow to TerraformResource label set: %s", first)
	}
	if strings.Contains(first, "K8sResource") {
		t.Fatalf("category=terraform should NOT include K8sResource (different label-set): %s", first)
	}
}

// TestGraphInfraResourceAggregateInventoryRejectsUnsafeDimension is the
// substitution-safety guard for the inventory Cypher template.
func TestGraphInfraResourceAggregateInventoryRejectsUnsafeDimension(t *testing.T) {
	t.Parallel()

	graph := &stubInfraGraphQuery{}
	store := NewGraphInfraResourceAggregateStore(graph)
	_, err := store.InfraResourceInventory(
		context.Background(),
		InfraResourceAggregateFilter{},
		InfraResourceInventoryDimension("account_id"),
		100,
		0,
	)
	if err == nil {
		t.Fatal("expected error for unknown dimension; got nil")
	}
	if len(graph.calls) != 0 {
		t.Fatal("graph queried for unknown dimension; substitution must be guarded before any graph call")
	}
}
