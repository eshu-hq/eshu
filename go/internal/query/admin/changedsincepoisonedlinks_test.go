// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestListChangedSincePoisonedLinksRequiresLimitAndTimeout(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
	mux := newAdminMux(h)

	for _, body := range []map[string]any{
		{"timeout_ms": 5000},
		{"limit": 10},
	} {
		w := postJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d for body %#v; body: %s", w.Code, http.StatusBadRequest, body, w.Body.String())
		}
	}
}

func TestListChangedSincePoisonedLinksRejectsUnknownStatus(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"status":     "wedged",
		"limit":      10,
		"timeout_ms": 5000,
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestListChangedSincePoisonedLinksEmpty(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"limit":      10,
		"timeout_ms": 5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if got["schema_version"] != changedSincePoisonedLinkSchemaVersion {
		t.Fatalf("schema_version = %v, want %v", got["schema_version"], changedSincePoisonedLinkSchemaVersion)
	}
	if got["truncated"] != false {
		t.Fatalf("truncated = %v, want false", got["truncated"])
	}
	if got["count"].(float64) != 0 {
		t.Fatalf("count = %v, want 0", got["count"])
	}
	if _, ok := got["next_cursor"]; ok {
		t.Fatalf("next_cursor present on an untruncated page: %#v", got)
	}
}

func TestListChangedSincePoisonedLinksFiltersAndTruncates(t *testing.T) {
	now := time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC)
	poisonedAt := now.Add(-time.Minute)
	nextAttempt := now.Add(time.Minute)
	failureClass := "statement_timeout"
	store := &stubAdminStore{
		changedSincePoisonedLinkRows: []ChangedSincePoisonedLink{
			{
				ScopeID: "scope-a", Status: "poisoned", ActivationSeq: 42, AttemptCount: 5,
				LastFailureClass: &failureClass, PoisonedAt: &poisonedAt, UpdatedAt: now,
			},
			{
				ScopeID: "scope-b", Status: "retrying", ActivationSeq: 7, AttemptCount: 2,
				LastFailureClass: &failureClass, NextAttemptAt: &nextAttempt, UpdatedAt: now,
			},
		},
	}
	h := &Handler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"status":     " poisoned ",
		"scope_id":   " scope-a ",
		"cursor":     " scope-0 ",
		"limit":      1,
		"timeout_ms": 7500,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got, want := store.changedSincePoisonedLinkFilter.Limit, 2; got != want {
		t.Fatalf("store limit = %d, want %d for limit+1 truncation probe", got, want)
	}
	if got, want := store.changedSincePoisonedLinkFilter.Status, "poisoned"; got != want {
		t.Fatalf("status filter = %q, want %q", got, want)
	}
	if got, want := store.changedSincePoisonedLinkFilter.ScopeID, "scope-a"; got != want {
		t.Fatalf("scope_id filter = %q, want %q", got, want)
	}
	if got, want := store.changedSincePoisonedLinkFilter.Cursor, "scope-0"; got != want {
		t.Fatalf("cursor filter = %q, want %q", got, want)
	}
	if got, want := store.changedSincePoisonedLinkFilter.Timeout, 7500*time.Millisecond; got != want {
		t.Fatalf("timeout = %s, want %s", got, want)
	}

	got := decodeBody(t, w)
	if got["truncated"] != true {
		t.Fatalf("truncated = %v, want true", got["truncated"])
	}
	if got["count"].(float64) != 1 {
		t.Fatalf("count = %v, want 1", got["count"])
	}
	if got["next_cursor"] != "scope-a" {
		t.Fatalf("next_cursor = %v, want scope-a", got["next_cursor"])
	}
	items := got["items"].([]any)
	first := items[0].(map[string]any)
	if first["scope_id"] != "scope-a" || first["status"] != "poisoned" || first["last_failure_class"] != "statement_timeout" {
		t.Fatalf("first item = %#v, want scope-a poisoned statement_timeout", first)
	}
}

func TestListChangedSincePoisonedLinksScopedGrants(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/changed-since/poisoned-links/query", strings.NewReader(`{
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:                 auth.AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if store.changedSincePoisonedLinkCalls != 1 {
		t.Fatalf("store calls = %d, want 1 for a granted token", store.changedSincePoisonedLinkCalls)
	}
}

func TestListChangedSincePoisonedLinksScopedEmptyGrantSkipsStore(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/changed-since/poisoned-links/query", strings.NewReader(`{
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:        auth.AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if store.changedSincePoisonedLinkCalls != 0 {
		t.Fatalf("store calls = %d, want 0 for a scoped token with no grants at all", store.changedSincePoisonedLinkCalls)
	}
}

func TestListChangedSincePoisonedLinksRecordsTelemetry(t *testing.T) {
	meter := metricnoop.NewMeterProvider().Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	store := &stubAdminStore{}
	h := &Handler{Store: store, Instruments: instruments}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/changed-since/poisoned-links/query", map[string]any{
		"limit":      10,
		"timeout_ms": 5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}
