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

// fakePoisonedLinkStore records the filter it was called with and answers
// with canned rows or a canned error.
type fakePoisonedLinkStore struct {
	items []ChangedSincePoisonedLink
	err   error
	got   ChangedSincePoisonedLinkFilter
	calls int
}

func (f *fakePoisonedLinkStore) ListChangedSincePoisonedLinks(_ context.Context, filter ChangedSincePoisonedLinkFilter) ([]ChangedSincePoisonedLink, error) {
	f.got = filter
	f.calls++
	return f.items, f.err
}

func servePoisonedLinks(t *testing.T, body string, store PoisonedLinkStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/changed-since/poisoned-links/query", strings.NewReader(body))
	rec := httptest.NewRecorder()
	ServePoisonedLinksQuery(rec, req, store, hook)
	return rec
}

func TestServePoisonedLinksQueryNilStoreIs503(t *testing.T) {
	rec := servePoisonedLinks(t, `{}`, nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServePoisonedLinksQueryBadJSONIs400(t *testing.T) {
	rec := servePoisonedLinks(t, `{`, &fakePoisonedLinkStore{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad JSON: want 400, got %d", rec.Code)
	}
}

func TestServePoisonedLinksQueryBadStatusIs400(t *testing.T) {
	rec := servePoisonedLinks(t, `{"status":"exploded"}`, &fakePoisonedLinkStore{}, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad status: want 400, got %d", rec.Code)
	}
}

func TestServePoisonedLinksQueryDefaultLimitAndTrims(t *testing.T) {
	store := &fakePoisonedLinkStore{}
	rec := servePoisonedLinks(t, `{"status":" poisoned ","scope_id":" s1 ","cursor":" c1 "}`, store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.calls != 1 {
		t.Fatalf("query: want one store call, got %d", store.calls)
	}
	if store.got.Limit != 100 {
		t.Fatalf("query: want default limit 100, got %d", store.got.Limit)
	}
	if store.got.Status != "poisoned" || store.got.ScopeID != "s1" || store.got.Cursor != "c1" {
		t.Fatalf("query: want trimmed selectors, got %+v", store.got)
	}
}

func TestServePoisonedLinksQuerySuccessShape(t *testing.T) {
	class := "link_poisoned"
	updated := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	next := updated.Add(time.Minute)
	store := &fakePoisonedLinkStore{items: []ChangedSincePoisonedLink{
		{
			ScopeID: "s1", Status: "retrying", ActivationSeq: 9,
			AttemptCount: 2, LastFailureClass: &class,
			NextAttemptAt: &next, UpdatedAt: updated,
		},
		{
			ScopeID: "s2", Status: "poisoned", ActivationSeq: 3,
			AttemptCount: 5, UpdatedAt: updated,
		},
	}}
	rec := servePoisonedLinks(t, `{"limit":10}`, store, nil)
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
	// Row keys mirror the core ChangedSincePoisonedLink JSON tags.
	first, _ := items[0].(map[string]any)
	if first["scope_id"] != "s1" || first["status"] != "retrying" || first["activation_seq"] != float64(9) || first["attempt_count"] != float64(2) {
		t.Fatalf("query: want row fields, got %v", first)
	}
	if first["last_failure_class"] != "link_poisoned" || first["updated_at"] != "2026-03-04T05:06:07Z" {
		t.Fatalf("query: want pointer fields and RFC3339, got %v", first)
	}
	if _, present := first["poisoned_at"]; present {
		t.Fatalf("query: want nil poisoned_at omitted, got %v", first)
	}
	second, _ := items[1].(map[string]any)
	for _, key := range []string{"last_failure_class", "next_attempt_at", "poisoned_at"} {
		if _, present := second[key]; present {
			t.Fatalf("query: want nil pointer %q omitted, got %v", key, second)
		}
	}
}

func TestServePoisonedLinksQueryStoreErrorIsFixed500(t *testing.T) {
	store := &fakePoisonedLinkStore{err: errors.New("sql: connection refused host=db-internal password=hunter2")}
	rec := servePoisonedLinks(t, `{}`, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store error: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "changed-since poisoned-links query failed" {
		t.Fatalf("store error: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("store error: backend text leaked into body: %s", rec.Body.String())
	}
}

func TestServePoisonedLinksQueryClientCancelIs499(t *testing.T) {
	store := &fakePoisonedLinkStore{err: context.Canceled}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/changed-since/poisoned-links/query", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	ServePoisonedLinksQuery(rec, cancelContext(t, req), store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
}

func TestServePoisonedLinksQueryStoreErrorHookWins(t *testing.T) {
	store := &fakePoisonedLinkStore{err: errors.New("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := servePoisonedLinks(t, `{}`, store, hook)
	if !hooked {
		t.Fatal("store error: want the fence hook called")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error hook: want hook verdict 503, got %d", rec.Code)
	}
}
