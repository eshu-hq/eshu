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

// fakeInputInvalidStore records the filter it was called with and answers
// with canned rows or a canned error.
type fakeInputInvalidStore struct {
	items []InputInvalidFact
	err   error
	got   InputInvalidFactListFilter
	calls int
}

func (f *fakeInputInvalidStore) ListReducerInputInvalidFacts(_ context.Context, filter InputInvalidFactListFilter) ([]InputInvalidFact, error) {
	f.got = filter
	f.calls++
	return f.items, f.err
}

func serveInputInvalid(t *testing.T, body string, store InputInvalidStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ServeInputInvalidFactsQuery(rec, req, store, hook)
	return rec
}

func TestServeInputInvalidFactsQueryNilStoreIs503(t *testing.T) {
	rec := serveInputInvalid(t, `{}`, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServeInputInvalidFactsQueryBadJSONIs400(t *testing.T) {
	rec := serveInputInvalid(t, `{`, &fakeInputInvalidStore{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: want 400, got %d", rec.Code)
	}
}

func TestServeInputInvalidFactsQueryMissingSelectorsIs400(t *testing.T) {
	for _, body := range []string{`{}`, `{"scope_id":"s"}`, `{"generation_id":"g"}`} {
		rec := serveInputInvalid(t, body, &fakeInputInvalidStore{}, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: want 400, got %d", body, rec.Code)
		}
	}
}

func TestServeInputInvalidFactsQueryDefaultLimitAndTrims(t *testing.T) {
	store := &fakeInputInvalidStore{}
	rec := serveInputInvalid(t, `{"scope_id":"  s1  ","generation_id":" g1 ","domain":" d ","fact_kind":" k "}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("query: want one store call, got %d", store.calls)
	}
	if store.got.Limit != 100 {
		t.Fatalf("query: want default limit 100, got %d", store.got.Limit)
	}
	if store.got.ScopeID != "s1" || store.got.GenerationID != "g1" || store.got.Domain != "d" || store.got.FactKind != "k" {
		t.Fatalf("query: want trimmed selectors, got %+v", store.got)
	}
}

func TestServeInputInvalidFactsQuerySuccessShape(t *testing.T) {
	decided := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	store := &fakeInputInvalidStore{items: []InputInvalidFact{
		{
			FactID: "f1", FactKind: "run", MissingField: "sha",
			FailureClass: "input_invalid", Domain: "d",
			ScopeID: "s1", GenerationID: "g1", DecidedAt: decided,
		},
	}}
	rec := serveInputInvalid(t, `{"scope_id":"s1","generation_id":"g1","limit":10}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["count"] != float64(1) {
		t.Fatalf("query: want count 1, got %v", body)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("query: want 1 item, got %v", body["items"])
	}
	// Row keys mirror the core InputInvalidFact JSON tags.
	first, _ := items[0].(map[string]any)
	for _, key := range []string{"fact_id", "fact_kind", "missing_field", "failure_class", "domain", "scope_id", "generation_id", "decided_at"} {
		if _, present := first[key]; !present {
			t.Fatalf("query: want key %q, got %v", key, first)
		}
	}
	if first["decided_at"] != "2026-03-04T05:06:07Z" {
		t.Fatalf("query: want RFC3339 decided_at, got %v", first["decided_at"])
	}
}

func TestServeInputInvalidFactsQueryStoreErrorIsFixed500(t *testing.T) {
	store := &fakeInputInvalidStore{err: errors.New("sql: connection refused host=db-internal password=hunter2")}
	rec := serveInputInvalid(t, `{"scope_id":"s","generation_id":"g"}`, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "input-invalid-facts query failed" {
		t.Fatalf("store error: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("store error: backend text leaked into body: %s", rec.Body.String())
	}
}

func TestServeInputInvalidFactsQueryClientCancelIs499(t *testing.T) {
	store := &fakeInputInvalidStore{err: context.Canceled}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{"scope_id":"s","generation_id":"g"}`))
	rec := httptest.NewRecorder()
	ServeInputInvalidFactsQuery(rec, cancelContext(t, req), store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
}

func TestServeInputInvalidFactsQueryStoreErrorHookWins(t *testing.T) {
	store := &fakeInputInvalidStore{err: errors.New("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := serveInputInvalid(t, `{"scope_id":"s","generation_id":"g"}`, store, hook)
	if !hooked {
		t.Fatal("store error: want the fence hook called")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error hook: want hook verdict 503, got %d", rec.Code)
	}
}
