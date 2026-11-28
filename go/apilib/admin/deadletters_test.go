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

// fakeDeadLetterStore records the filter it was called with and answers with
// canned rows or a canned error.
type fakeDeadLetterStore struct {
	items []DeadLetterWorkItem
	err   error
	got   DeadLetterListFilter
	calls int
}

func (f *fakeDeadLetterStore) ListDeadLetterWorkItems(_ context.Context, filter DeadLetterListFilter) ([]DeadLetterWorkItem, error) {
	f.got = filter
	f.calls++
	return f.items, f.err
}

func serveDeadLetters(t *testing.T, body string, store DeadLetterStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/dead-letters/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ServeDeadLettersQuery(rec, req, store, hook)
	return rec
}

func TestServeDeadLettersQueryNilStoreIs503(t *testing.T) {
	rec := serveDeadLetters(t, `{}`, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServeDeadLettersQueryBadJSONIs400(t *testing.T) {
	rec := serveDeadLetters(t, `{`, &fakeDeadLetterStore{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: want 400, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("bad JSON: want an error body")
	}
}

func TestServeDeadLettersQueryDefaultLimitAndTrims(t *testing.T) {
	store := &fakeDeadLetterStore{}
	rec := serveDeadLetters(t, `{"failure_class":" fc ","domain":" d ","scope_id":" s ","collector_kind":" ck "}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("query: want one store call, got %d", store.calls)
	}
	if store.got.Limit != 100 {
		t.Fatalf("query: want default limit 100, got %d", store.got.Limit)
	}
	if store.got.FailureClass != "fc" || store.got.Domain != "d" || store.got.ScopeID != "s" || store.got.CollectorKind != "ck" {
		t.Fatalf("query: want trimmed selectors, got %+v", store.got)
	}
}

func TestServeDeadLettersQuerySuccessShape(t *testing.T) {
	failure := "build_failed"
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	updated := created.Add(time.Hour)
	visible := created.Add(2 * time.Hour)
	store := &fakeDeadLetterStore{items: []DeadLetterWorkItem{
		{
			WorkItemID: "w1", ScopeID: "s1", GenerationID: "g1",
			Stage: "build", Domain: "d", CollectorKind: "ck",
			AttemptCount: 3, FailureClass: &failure,
			CreatedAt: created, UpdatedAt: updated, VisibleAt: &visible,
		},
		{
			WorkItemID: "w2", ScopeID: "s1", GenerationID: "g1",
			Stage: "build", Domain: "d", CollectorKind: "ck",
			AttemptCount: 1, CreatedAt: created, UpdatedAt: updated,
		},
	}}
	rec := serveDeadLetters(t, `{"limit":10}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["count"] != float64(2) {
		t.Fatalf("query: want count 2, got %v", body)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("query: want 2 items, got %v", body["items"])
	}
	// Row shape mirrors the core deadLetterItemsToSlice map keys.
	first, _ := items[0].(map[string]any)
	if first["work_item_id"] != "w1" || first["collector_kind"] != "ck" || first["failure_class"] != "build_failed" {
		t.Fatalf("query: want row fields with pointers, got %v", first)
	}
	if first["created_at"] != "2026-03-04T05:06:07Z" || first["updated_at"] != "2026-03-04T06:06:07Z" || first["visible_at"] != "2026-03-04T07:06:07Z" {
		t.Fatalf("query: want RFC3339 timestamps, got %v", first)
	}
	second, _ := items[1].(map[string]any)
	for _, key := range []string{"failure_class", "visible_at"} {
		if _, present := second[key]; present {
			t.Fatalf("query: want nil pointer %q omitted, got %v", key, second)
		}
	}
}

// windowDeadLetterStore answers with canned rows narrowed by the same
// updated_at range predicate the core store applies: updated_at >=
// UpdatedAfter (inclusive) and updated_at < UpdatedBefore (exclusive).
type windowDeadLetterStore struct {
	items []DeadLetterWorkItem
	got   DeadLetterListFilter
	calls int
}

func (f *windowDeadLetterStore) ListDeadLetterWorkItems(_ context.Context, filter DeadLetterListFilter) ([]DeadLetterWorkItem, error) {
	f.got = filter
	f.calls++
	var kept []DeadLetterWorkItem
	for _, item := range f.items {
		if filter.UpdatedAfter != nil && item.UpdatedAt.Before(*filter.UpdatedAfter) {
			continue
		}
		if filter.UpdatedBefore != nil && !item.UpdatedAt.Before(*filter.UpdatedBefore) {
			continue
		}
		kept = append(kept, item)
	}
	return kept, nil
}

func windowRow(id string, updated time.Time) DeadLetterWorkItem {
	return DeadLetterWorkItem{
		WorkItemID: id, ScopeID: "s1", GenerationID: "g1",
		Stage: "build", Domain: "d", CollectorKind: "ck",
		AttemptCount: 1, UpdatedAt: updated,
	}
}

func TestServeDeadLettersQueryWindowFiltersRows(t *testing.T) {
	after := time.Date(2026, 7, 6, 13, 0, 0, 0, time.UTC)
	before := time.Date(2026, 7, 6, 14, 0, 0, 0, time.UTC)
	store := &windowDeadLetterStore{items: []DeadLetterWorkItem{
		windowRow("before-window", after.Add(-time.Second)),
		windowRow("at-after", after),
		windowRow("mid-window", after.Add(30*time.Minute)),
		windowRow("at-before", before),
		windowRow("after-window", before.Add(time.Second)),
	}}
	rec := serveDeadLetters(t, `{"updated_after":"2026-07-06T13:00:00Z","updated_before":"2026-07-06T14:00:00Z","limit":10}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("windowed query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("windowed query: want one store call, got %d", store.calls)
	}
	if store.got.UpdatedAfter == nil || !store.got.UpdatedAfter.Equal(after) {
		t.Fatalf("updated_after filter = %v, want %s", store.got.UpdatedAfter, after.Format(time.RFC3339))
	}
	if store.got.UpdatedBefore == nil || !store.got.UpdatedBefore.Equal(before) {
		t.Fatalf("updated_before filter = %v, want %s", store.got.UpdatedBefore, before.Format(time.RFC3339))
	}
	body := decodeJSONBody(t, rec)
	if body["count"] != float64(2) {
		t.Fatalf("windowed query: want count 2 (at-after, mid-window), got %v", body)
	}
	items, _ := body["items"].([]any)
	got := []string{items[0].(map[string]any)["work_item_id"].(string), items[1].(map[string]any)["work_item_id"].(string)}
	if got[0] != "at-after" || got[1] != "mid-window" {
		t.Fatalf("windowed query: want [at-after mid-window] (>= after, < before), got %v", got)
	}
}

func TestServeDeadLettersQueryWindowOrderingIs400(t *testing.T) {
	for _, body := range []string{
		`{"updated_after":"2026-07-06T14:00:00Z","updated_before":"2026-07-06T13:00:00Z"}`,
		`{"updated_after":"2026-07-06T13:00:00Z","updated_before":"2026-07-06T13:00:00Z"}`,
	} {
		store := &fakeDeadLetterStore{}
		rec := serveDeadLetters(t, body, store, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("ordering %s: want 400, got %d (%s)", body, rec.Code, rec.Body.String())
		}
		if got := decodeJSONBody(t, rec)["detail"]; got != "updated_after must be before updated_before" {
			t.Fatalf("ordering %s: want core-identical detail, got %v", body, got)
		}
		if store.calls != 0 {
			t.Fatalf("ordering %s: want no store call, got %d", body, store.calls)
		}
	}
}

func TestServeDeadLettersQueryWindowMalformedIs400(t *testing.T) {
	for _, tc := range []struct{ body, detail string }{
		{`{"updated_after":"not-a-time"}`, "updated_after must be RFC3339"},
		{`{"updated_before":"07/06/2026"}`, "updated_before must be RFC3339"},
	} {
		store := &fakeDeadLetterStore{}
		rec := serveDeadLetters(t, tc.body, store, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("malformed %s: want 400, got %d (%s)", tc.body, rec.Code, rec.Body.String())
		}
		if got := decodeJSONBody(t, rec)["detail"]; got != tc.detail {
			t.Fatalf("malformed %s: want detail %q, got %v", tc.body, tc.detail, got)
		}
		if store.calls != 0 {
			t.Fatalf("malformed %s: want no store call, got %d", tc.body, store.calls)
		}
	}
}

func TestServeDeadLettersQueryNoWindowPassesNil(t *testing.T) {
	store := &fakeDeadLetterStore{}
	rec := serveDeadLetters(t, `{"limit":10}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("unwindowed query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.got.UpdatedAfter != nil || store.got.UpdatedBefore != nil {
		t.Fatalf("unwindowed query: want nil window bounds, got %+v", store.got)
	}
}

func TestServeDeadLettersQueryStoreErrorIsFixed500(t *testing.T) {
	store := &fakeDeadLetterStore{err: errors.New("sql: connection refused host=db-internal password=hunter2")}
	rec := serveDeadLetters(t, `{}`, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "dead-letter query failed" {
		t.Fatalf("store error: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("store error: backend text leaked into body: %s", rec.Body.String())
	}
}

func TestServeDeadLettersQueryClientCancelIs499(t *testing.T) {
	store := &fakeDeadLetterStore{err: context.Canceled}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/dead-letters/query", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ServeDeadLettersQuery(rec, cancelContext(t, req), store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "dead-letter query failed" {
		t.Fatalf("client cancel: want fixed message, got %v", body)
	}
}

func TestServeDeadLettersQueryStoreErrorHookWins(t *testing.T) {
	store := &fakeDeadLetterStore{err: errors.New("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := serveDeadLetters(t, `{}`, store, hook)
	if !hooked {
		t.Fatal("store error: want the fence hook called")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error hook: want hook verdict 503, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "fence") {
		t.Fatalf("store error hook: want hook body kept, got %s", rec.Body.String())
	}
}
