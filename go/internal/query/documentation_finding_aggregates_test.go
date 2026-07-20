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

type stubDocumentationFindingAggregateStore struct {
	count         DocumentationFindingAggregateCount
	countErr      error
	inventory     []DocumentationFindingInventoryRow
	inventoryErr  error
	lastFilter    DocumentationFindingAggregateFilter
	lastDimension DocumentationFindingInventoryDimension
	lastLimit     int
	lastOffset    int
	countCalls    int
	invCalls      int
}

func (s *stubDocumentationFindingAggregateStore) CountDocumentationFindings(
	_ context.Context,
	filter DocumentationFindingAggregateFilter,
) (DocumentationFindingAggregateCount, error) {
	s.countCalls++
	s.lastFilter = filter
	if s.countErr != nil {
		return DocumentationFindingAggregateCount{}, s.countErr
	}
	return s.count, nil
}

func (s *stubDocumentationFindingAggregateStore) DocumentationFindingInventory(
	_ context.Context,
	filter DocumentationFindingAggregateFilter,
	dim DocumentationFindingInventoryDimension,
	limit int,
	offset int,
) ([]DocumentationFindingInventoryRow, error) {
	s.invCalls++
	s.lastFilter = filter
	s.lastDimension = dim
	s.lastLimit = limit
	s.lastOffset = offset
	if s.inventoryErr != nil {
		return nil, s.inventoryErr
	}
	return append([]DocumentationFindingInventoryRow(nil), s.inventory...), nil
}

func TestDocumentationFindingAggregateRoutesReturn503WhenStoreMissing(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/documentation/findings/count",
		"/api/v0/documentation/findings/inventory",
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

func TestDocumentationFindingAggregateCountReturnsRollups(t *testing.T) {
	t.Parallel()

	store := &stubDocumentationFindingAggregateStore{
		count: DocumentationFindingAggregateCount{
			TotalFindings:    20,
			ByStatus:         map[string]int{"active": 14, "stale": 4, "withdrawn": 2},
			ByTruthLevel:     map[string]int{"exact": 10, "derived": 8, "fallback": 2},
			ByFreshnessState: map[string]int{"fresh": 15, "stale": 5},
		},
	}
	handler := &DocumentationHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/count?source_id=confluence-space-A", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.countCalls != 1 {
		t.Fatalf("Count called %d times, want 1", store.countCalls)
	}
	if got, want := store.lastFilter.SourceID, "confluence-space-A"; got != want {
		t.Fatalf("SourceID = %q, want %q", got, want)
	}
	var body struct {
		TotalFindings    int            `json:"total_findings"`
		ByStatus         map[string]int `json:"by_status"`
		ByTruthLevel     map[string]int `json:"by_truth_level"`
		ByFreshnessState map[string]int `json:"by_freshness_state"`
		Scope            map[string]any `json:"scope"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v; body = %s", err, w.Body.String())
	}
	if body.TotalFindings != 20 {
		t.Fatalf("total_findings = %d, want 20", body.TotalFindings)
	}
	if body.ByStatus["active"] != 14 {
		t.Fatalf("by_status[active] = %d, want 14", body.ByStatus["active"])
	}
	if body.ByTruthLevel["exact"] != 10 {
		t.Fatalf("by_truth_level[exact] = %d, want 10", body.ByTruthLevel["exact"])
	}
	if body.ByFreshnessState["fresh"] != 15 {
		t.Fatalf("by_freshness_state[fresh] = %d, want 15", body.ByFreshnessState["fresh"])
	}
	if body.Scope["source_id"] != "confluence-space-A" {
		t.Fatalf("scope.source_id = %v, want confluence-space-A", body.Scope["source_id"])
	}
}

func TestDocumentationFindingAggregateInventoryReturnsBuckets(t *testing.T) {
	t.Parallel()

	store := &stubDocumentationFindingAggregateStore{
		inventory: []DocumentationFindingInventoryRow{
			{Dimension: DocumentationFindingInventoryByStatus, Value: "active", Count: 12},
			{Dimension: DocumentationFindingInventoryByStatus, Value: "stale", Count: 3},
		},
	}
	handler := &DocumentationHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/inventory?group_by=status&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastDimension != DocumentationFindingInventoryByStatus {
		t.Fatalf("dimension = %q, want status", store.lastDimension)
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
	if body.Count != 2 {
		t.Fatalf("count = %d, want 2", body.Count)
	}
	if body.GroupBy != "status" {
		t.Fatalf("group_by = %q, want status", body.GroupBy)
	}
	if body.Truncated {
		t.Fatalf("truncated = true, want false (only 2 buckets, limit 10)")
	}
}

func TestDocumentationFindingAggregateInventoryReportsTruncated(t *testing.T) {
	t.Parallel()

	rows := make([]DocumentationFindingInventoryRow, 6)
	for i := range rows {
		rows[i] = DocumentationFindingInventoryRow{
			Dimension: DocumentationFindingInventoryBySourceID,
			Value:     "src",
			Count:     i,
		}
	}
	store := &stubDocumentationFindingAggregateStore{inventory: rows}
	handler := &DocumentationHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/inventory?group_by=source_id&limit=5", nil)
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

func TestDocumentationFindingAggregateInventoryRejectsUnknownDimension(t *testing.T) {
	t.Parallel()

	store := &stubDocumentationFindingAggregateStore{}
	handler := &DocumentationHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/inventory?group_by=document_id", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.invCalls != 0 {
		t.Fatalf("store called for unknown dimension")
	}
}

func TestDocumentationFindingAggregateInventoryRejectsOversizedLimit(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{Aggregates: &stubDocumentationFindingAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/inventory?limit=9999", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestDocumentationFindingAggregateInventoryRejectsNegativeOffset(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{Aggregates: &stubDocumentationFindingAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/inventory?offset=-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestDocumentationFindingAggregateInventoryRejectsOversizedOffset(t *testing.T) {
	t.Parallel()

	handler := &DocumentationHandler{Aggregates: &stubDocumentationFindingAggregateStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/inventory?offset=10001", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestDocumentationFindingAggregateInventoryNullsNextOffsetAtCeiling(t *testing.T) {
	t.Parallel()

	rows := make([]DocumentationFindingInventoryRow, 6)
	for i := range rows {
		rows[i] = DocumentationFindingInventoryRow{
			Dimension: DocumentationFindingInventoryByStatus,
			Value:     "active",
			Count:     i,
		}
	}
	store := &stubDocumentationFindingAggregateStore{inventory: rows}
	handler := &DocumentationHandler{Aggregates: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings/inventory?group_by=status&limit=5&offset=10000", nil)
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

func TestNextDocumentationFindingAggregateOffsetBound(t *testing.T) {
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
			got := nextDocumentationFindingAggregateOffset(tc.offset, tc.limit, tc.truncated)
			if got != tc.want {
				t.Fatalf("nextDocumentationFindingAggregateOffset(%d, %d, %v) = %v, want %v",
					tc.offset, tc.limit, tc.truncated, got, tc.want)
			}
		})
	}
}

func TestDocumentationFindingInventoryGroupExpressionEnumIsClosed(t *testing.T) {
	t.Parallel()

	cases := []DocumentationFindingInventoryDimension{
		DocumentationFindingInventoryByStatus,
		DocumentationFindingInventoryByTruthLevel,
		DocumentationFindingInventoryByFreshnessState,
		DocumentationFindingInventoryByFindingType,
		DocumentationFindingInventoryBySourceID,
	}
	for _, dim := range cases {
		if _, err := documentationFindingInventoryGroupExpression(dim); err != nil {
			t.Fatalf("dimension %q must be supported: %v", dim, err)
		}
	}
	if _, err := documentationFindingInventoryGroupExpression("document_id"); err == nil {
		t.Fatal("documentationFindingInventoryGroupExpression must reject unknown dimensions to keep SQL substitution safe")
	}
}

// TestDocumentationFindingAggregateBackendErrorsDoNotLeakDetails proves the
// aggregate routes match the rest of the documentation handler family on
// backend failure: stable internal-error envelope, no raw Postgres / query
// string in the body. Carrying over `err.Error()` would (a) drift from the
// existing documentation error contract and (b) expose the database query
// shape to callers.
func TestDocumentationFindingAggregateBackendErrorsDoNotLeakDetails(t *testing.T) {
	t.Parallel()

	leaky := errors.New("pq: relation \"fact_records_secret\" does not exist (SQLSTATE 42P01)")
	for _, target := range []string{
		"/api/v0/documentation/findings/count",
		"/api/v0/documentation/findings/inventory?group_by=status",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			store := &stubDocumentationFindingAggregateStore{
				countErr:     leaky,
				inventoryErr: leaky,
			}
			handler := &DocumentationHandler{Aggregates: store}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusInternalServerError; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			body := w.Body.String()
			if strings.Contains(body, "pq:") || strings.Contains(body, "SQLSTATE") || strings.Contains(body, "fact_records_secret") {
				t.Fatalf("response leaks raw Postgres/query details: %s", body)
			}
		})
	}
}

// TestDocumentationFindingAggregateSQLIncludesPermissionPredicates is the
// regression guard against a future refactor accidentally dropping the
// aggregate-only SQL permission predicates. Unlike the list endpoint, which
// retrieves protected rows and discloses them honestly with content withheld,
// the aggregate must exclude those rows or it would leak protected counts.
func TestDocumentationFindingAggregateSQLIncludesPermissionPredicates(t *testing.T) {
	t.Parallel()

	groupExpr, err := documentationFindingInventoryGroupExpression(DocumentationFindingInventoryByStatus)
	if err != nil {
		t.Fatalf("documentationFindingInventoryGroupExpression() error = %v", err)
	}
	countSQL, _ := buildDocumentationFindingAggregateTotalSQL(DocumentationFindingAggregateFilter{})
	groupSQL, _ := buildDocumentationFindingAggregateGroupSQL(DocumentationFindingAggregateFilter{}, groupExpr)
	inventorySQL, _ := buildDocumentationFindingInventorySQL(DocumentationFindingAggregateFilter{}, groupExpr, 10, 0)
	for name, q := range map[string]string{
		"count":     countSQL,
		"group":     groupSQL,
		"inventory": inventorySQL,
	} {
		name, q := name, q
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, predicate := range []string{
				"viewer_can_read_source",
				"source_acl_evaluated",
				"permission_decision",
			} {
				if !strings.Contains(q, predicate) {
					t.Fatalf("%s query missing permission predicate %q; would leak counts from documents the caller cannot read: %s", name, predicate, q)
				}
			}
		})
	}
}
