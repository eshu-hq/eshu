// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeDecisionStore records the filters it was called with and answers with
// canned decisions, canned evidence, or a canned error.
type fakeDecisionStore struct {
	decisions []DecisionRow
	evidence  map[string][]EvidenceRow
	err       error
	evErr     error
	got       DecisionQueryFilter
	gotEv     []string
	calls     int
}

func (f *fakeDecisionStore) ListDecisions(_ context.Context, filter DecisionQueryFilter) ([]DecisionRow, error) {
	f.got = filter
	f.calls++
	return f.decisions, f.err
}

func (f *fakeDecisionStore) ListEvidence(_ context.Context, decisionID string) ([]EvidenceRow, error) {
	f.gotEv = append(f.gotEv, decisionID)
	if f.evErr != nil {
		return nil, f.evErr
	}
	return f.evidence[decisionID], nil
}

func serveDecisions(t *testing.T, body string, store DecisionStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/decisions/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ServeDecisionsQuery(rec, req, store, hook)
	return rec
}

func TestServeDecisionsQueryNilStoreIs503(t *testing.T) {
	rec := serveDecisions(t, `{}`, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServeDecisionsQueryBadJSONIs400(t *testing.T) {
	rec := serveDecisions(t, `{`, &fakeDecisionStore{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: want 400, got %d", rec.Code)
	}
}

func TestServeDecisionsQueryMissingSelectorsIs400(t *testing.T) {
	for _, body := range []string{`{}`, `{"repository_id":"r"}`, `{"source_run_id":"s"}`, `{"repository_id":"  ","source_run_id":"s"}`} {
		rec := serveDecisions(t, body, &fakeDecisionStore{}, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: want 400, got %d", body, rec.Code)
		}
	}
}

func TestServeDecisionsQueryDefaultLimitAndTrims(t *testing.T) {
	store := &fakeDecisionStore{}
	rec := serveDecisions(t, `{"repository_id":"  r1  ","source_run_id":" s1 "}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("query: want one store call, got %d", store.calls)
	}
	if store.got.Limit != 100 {
		t.Fatalf("query: want default limit 100, got %d", store.got.Limit)
	}
	if store.got.RepositoryID != "r1" || store.got.SourceRunID != "s1" {
		t.Fatalf("query: want trimmed selectors, got %+v", store.got)
	}
}

func TestServeDecisionsQuerySuccessShape(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	store := &fakeDecisionStore{decisions: []DecisionRow{
		{
			DecisionID: "d1", DecisionType: "merge", RepositoryID: "r1",
			SourceRunID: "s1", WorkItemID: "w1", Subject: "sub",
			ConfidenceScore: 0.9, ConfidenceReason: "why",
			ProvenanceSummary: map[string]any{"k": "v"}, CreatedAt: created,
		},
	}}
	rec := serveDecisions(t, `{"repository_id":"r1","source_run_id":"s1","limit":10}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["count"] != float64(1) {
		t.Fatalf("query: want count 1, got %v", body)
	}
	decisions, ok := body["decisions"].([]any)
	if !ok || len(decisions) != 1 {
		t.Fatalf("query: want 1 decision, got %v", body["decisions"])
	}
	// Row keys mirror the core decisionRowToMap map keys.
	first, _ := decisions[0].(map[string]any)
	for _, key := range []string{"decision_id", "decision_type", "repository_id", "source_run_id", "work_item_id", "subject", "confidence_score", "confidence_reason", "provenance_summary", "created_at"} {
		if _, present := first[key]; !present {
			t.Fatalf("query: want key %q, got %v", key, first)
		}
	}
	if first["created_at"] != "2026-03-04T05:06:07Z" {
		t.Fatalf("query: want RFC3339 created_at, got %v", first["created_at"])
	}
	if _, present := first["evidence"]; present {
		t.Fatalf("query: want no evidence key without include_evidence, got %v", first)
	}
}

func TestServeDecisionsQueryEvidenceJoin(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	fact := "f1"
	store := &fakeDecisionStore{
		decisions: []DecisionRow{{DecisionID: "d1", DecisionType: "merge", RepositoryID: "r1", SourceRunID: "s1", CreatedAt: created}},
		evidence: map[string][]EvidenceRow{
			"d1": {{EvidenceID: "e1", DecisionID: "d1", FactID: &fact, EvidenceKind: "file", Detail: map[string]any{"p": "x"}, CreatedAt: created}},
		},
	}
	rec := serveDecisions(t, `{"repository_id":"r1","source_run_id":"s1","include_evidence":true}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	decisions, _ := body["decisions"].([]any)
	first, _ := decisions[0].(map[string]any)
	ev, ok := first["evidence"].([]any)
	if !ok || len(ev) != 1 {
		t.Fatalf("query: want joined evidence, got %v", first["evidence"])
	}
	row, _ := ev[0].(map[string]any)
	if row["evidence_id"] != "e1" || row["fact_id"] != "f1" || row["evidence_kind"] != "file" {
		t.Fatalf("query: want evidence row fields, got %v", row)
	}
	if len(store.gotEv) != 1 || store.gotEv[0] != "d1" {
		t.Fatalf("query: want one ListEvidence call for d1, got %v", store.gotEv)
	}
}

func TestServeDecisionsQueryStoreErrorIsFixed500(t *testing.T) {
	store := &fakeDecisionStore{err: errors.New("sql: connection refused host=db-internal password=hunter2")}
	rec := serveDecisions(t, `{"repository_id":"r","source_run_id":"s"}`, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "projection decision query failed" {
		t.Fatalf("store error: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("store error: backend text leaked into body: %s", rec.Body.String())
	}
}

func TestServeDecisionsQueryEvidenceErrorIsFixed500(t *testing.T) {
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	store := &fakeDecisionStore{
		decisions: []DecisionRow{{DecisionID: "d1", CreatedAt: created}},
		evErr:     errors.New("sql: evidence timeout password=hunter2"),
	}
	rec := serveDecisions(t, `{"repository_id":"r","source_run_id":"s","include_evidence":true}`, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("evidence error: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "projection decision evidence query failed" {
		t.Fatalf("evidence error: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("evidence error: backend text leaked into body: %s", rec.Body.String())
	}
}

func TestServeDecisionsQueryClientCancelIs499(t *testing.T) {
	store := &fakeDecisionStore{err: context.Canceled}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/decisions/query", strings.NewReader(`{"repository_id":"r","source_run_id":"s"}`))
	rec := httptest.NewRecorder()
	ServeDecisionsQuery(rec, cancelContext(t, req), store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
}

func TestServeDecisionsQueryStoreErrorHookWins(t *testing.T) {
	store := &fakeDecisionStore{err: errors.New("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := serveDecisions(t, `{"repository_id":"r","source_run_id":"s"}`, store, hook)
	if !hooked {
		t.Fatal("store error: want the fence hook called")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error hook: want hook verdict 503, got %d", rec.Code)
	}
}
