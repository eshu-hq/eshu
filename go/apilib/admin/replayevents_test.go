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

// fakeReplayEventStore records the filter it was called with and answers with
// canned rows or a canned error.
type fakeReplayEventStore struct {
	items []ReplayEvent
	err   error
	got   ReplayEventFilter
	calls int
}

func (f *fakeReplayEventStore) ListReplayEvents(_ context.Context, filter ReplayEventFilter) ([]ReplayEvent, error) {
	f.got = filter
	f.calls++
	return f.items, f.err
}

func serveReplayEvents(t *testing.T, body string, store ReplayEventStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/replay-events/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ServeReplayEventsQuery(rec, req, store, hook)
	return rec
}

func TestServeReplayEventsQueryNilStoreIs503(t *testing.T) {
	rec := serveReplayEvents(t, `{}`, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServeReplayEventsQueryBadJSONIs400(t *testing.T) {
	rec := serveReplayEvents(t, `{`, &fakeReplayEventStore{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: want 400, got %d", rec.Code)
	}
}

func TestServeReplayEventsQueryDefaultLimitAndTrims(t *testing.T) {
	store := &fakeReplayEventStore{}
	rec := serveReplayEvents(t, `{"scope_id":"  s1  ","work_item_id":" w1 ","failure_class":" fc "}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("query: want one store call, got %d", store.calls)
	}
	if store.got.Limit != 100 {
		t.Fatalf("query: want default limit 100, got %d", store.got.Limit)
	}
	if store.got.ScopeID != "s1" || store.got.WorkItemID != "w1" || store.got.FailureClass != "fc" {
		t.Fatalf("query: want trimmed selectors, got %+v", store.got)
	}
}

func TestServeReplayEventsQuerySuccessShape(t *testing.T) {
	class := "build_failed"
	note := "operator retry"
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	store := &fakeReplayEventStore{items: []ReplayEvent{
		{
			ReplayEventID: "e1", WorkItemID: "w1", ScopeID: "s1",
			GenerationID: "g1", FailureClass: &class, OperatorNote: &note,
			CreatedAt: created,
		},
		{
			ReplayEventID: "e2", WorkItemID: "w2", ScopeID: "s1",
			GenerationID: "g1", CreatedAt: created,
		},
	}}
	rec := serveReplayEvents(t, `{"limit":10}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["count"] != float64(2) {
		t.Fatalf("query: want count 2, got %v", body)
	}
	// Event keys mirror the core listReplayEvents entry keys.
	events, ok := body["events"].([]any)
	if !ok || len(events) != 2 {
		t.Fatalf("query: want 2 events, got %v", body["events"])
	}
	first, _ := events[0].(map[string]any)
	if first["replay_event_id"] != "e1" || first["failure_class"] != "build_failed" || first["operator_note"] != "operator retry" {
		t.Fatalf("query: want event fields with pointers, got %v", first)
	}
	if first["created_at"] != "2026-03-04T05:06:07Z" {
		t.Fatalf("query: want RFC3339 created_at, got %v", first)
	}
	second, _ := events[1].(map[string]any)
	for _, key := range []string{"failure_class", "operator_note"} {
		if _, present := second[key]; present {
			t.Fatalf("query: want nil pointer %q omitted, got %v", key, second)
		}
	}
}

func TestServeReplayEventsQueryStoreErrorIsFixed500(t *testing.T) {
	store := &fakeReplayEventStore{err: errors.New("sql: connection refused host=db-internal password=hunter2")}
	rec := serveReplayEvents(t, `{}`, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "replay-event query failed" {
		t.Fatalf("store error: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("store error: backend text leaked into body: %s", rec.Body.String())
	}
}

func TestServeReplayEventsQueryClientCancelIs499(t *testing.T) {
	store := &fakeReplayEventStore{err: context.Canceled}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/replay-events/query", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ServeReplayEventsQuery(rec, cancelContext(t, req), store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
}

func TestServeReplayEventsQueryStoreErrorHookWins(t *testing.T) {
	store := &fakeReplayEventStore{err: errors.New("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := serveReplayEvents(t, `{}`, store, hook)
	if !hooked {
		t.Fatal("store error: want the fence hook called")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error hook: want hook verdict 503, got %d", rec.Code)
	}
}
