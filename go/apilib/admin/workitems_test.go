// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeWorkItemStore records the filter it was called with and answers with
// canned rows or a canned error.
type fakeWorkItemStore struct {
	items []WorkItem
	err   error
	got   WorkItemFilter
	calls int
}

func (f *fakeWorkItemStore) ListWorkItems(_ context.Context, filter WorkItemFilter) ([]WorkItem, error) {
	f.got = filter
	f.calls++
	return f.items, f.err
}

// serveWorkItems posts body to the work-items query handler with the given
// store and hook.
func serveWorkItems(t *testing.T, body string, store WorkItemStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/work-items/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ServeWorkItemsQuery(rec, req, store, hook)
	return rec
}

// decodeWorkItems decodes a work-items response body into a field map.
func decodeWorkItems(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("work-items body is not JSON: %v", err)
	}
	return out
}

func TestServeWorkItemsQueryNilStoreIs503(t *testing.T) {
	rec := serveWorkItems(t, `{}`, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeWorkItems(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServeWorkItemsQueryBadJSONIs400(t *testing.T) {
	rec := serveWorkItems(t, `{`, &fakeWorkItemStore{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: want 400, got %d", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Fatal("bad JSON: want an error body")
	}
}

func TestServeWorkItemsQueryDefaultLimitAndTrims(t *testing.T) {
	store := &fakeWorkItemStore{}
	rec := serveWorkItems(t, `{"scope_id":"  s1  ","stage":" st ","failure_class":" fc "}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("query: want one store call, got %d", store.calls)
	}
	if store.got.Limit != 100 {
		t.Fatalf("query: want default limit 100, got %d", store.got.Limit)
	}
	if store.got.ScopeID != "s1" || store.got.Stage != "st" || store.got.FailureClass != "fc" {
		t.Fatalf("query: want trimmed selectors, got %+v", store.got)
	}
}

func TestServeWorkItemsQuerySuccessShape(t *testing.T) {
	lease := "owner-1"
	failure := "build_failed"
	created := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	updated := created.Add(time.Hour)
	visible := created.Add(2 * time.Hour)
	store := &fakeWorkItemStore{items: []WorkItem{
		{
			WorkItemID: "w1", ScopeID: "s1", GenerationID: "g1",
			Stage: "build", Domain: "d", Status: "pending", AttemptCount: 3,
			LeaseOwner: &lease, FailureClass: &failure,
			CreatedAt: created, UpdatedAt: updated, VisibleAt: &visible,
		},
		{
			WorkItemID: "w2", ScopeID: "s1", GenerationID: "g1",
			Stage: "build", Domain: "d", Status: "failed", AttemptCount: 1,
			CreatedAt: created, UpdatedAt: updated,
		},
	}}
	rec := serveWorkItems(t, `{"limit":10}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeWorkItems(t, rec)
	if body["count"] != float64(2) {
		t.Fatalf("query: want count 2, got %v", body)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("query: want 2 items, got %v", body["items"])
	}
	first, _ := items[0].(map[string]any)
	if first["work_item_id"] != "w1" || first["lease_owner"] != "owner-1" || first["failure_class"] != "build_failed" {
		t.Fatalf("query: want row fields with pointers, got %v", first)
	}
	if first["created_at"] != "2026-03-04T05:06:07Z" || first["updated_at"] != "2026-03-04T06:06:07Z" || first["visible_at"] != "2026-03-04T07:06:07Z" {
		t.Fatalf("query: want RFC3339 timestamps, got %v", first)
	}
	second, _ := items[1].(map[string]any)
	for _, key := range []string{"lease_owner", "failure_class", "failure_message", "operator_note", "prior_failure", "visible_at"} {
		if _, present := second[key]; present {
			t.Fatalf("query: want nil pointer %q omitted, got %v", key, second)
		}
	}
}

func TestServeWorkItemsQueryStoreErrorIsFixed500(t *testing.T) {
	store := &fakeWorkItemStore{err: errors.New("sql: connection refused host=db-internal password=hunter2")}
	rec := serveWorkItems(t, `{}`, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: want 500, got %d", rec.Code)
	}
	body := decodeWorkItems(t, rec)
	if body["detail"] != "work-item query failed" {
		t.Fatalf("store error: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("store error: backend text leaked into body: %s", rec.Body.String())
	}
}

func TestServeWorkItemsQueryClientCancelIs499(t *testing.T) {
	store := &fakeWorkItemStore{err: context.Canceled}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/work-items/query", strings.NewReader(`{}`))
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	ServeWorkItemsQuery(rec, req, store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
	body := decodeWorkItems(t, rec)
	if body["detail"] != "work-item query failed" {
		t.Fatalf("client cancel: want fixed message, got %v", body)
	}
}

func TestServeWorkItemsQueryStoreErrorHookWins(t *testing.T) {
	store := &fakeWorkItemStore{err: errors.New("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := serveWorkItems(t, `{}`, store, hook)
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
