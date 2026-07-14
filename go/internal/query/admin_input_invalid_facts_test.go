// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestListInputInvalidFactsRequiresScopeGenerationLimitAndTimeout(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	for _, body := range []map[string]any{
		{"generation_id": "gen-a", "limit": 10, "timeout_ms": 5000},
		{"scope_id": "scope-a", "limit": 10, "timeout_ms": 5000},
		{"scope_id": "scope-a", "generation_id": "gen-a", "timeout_ms": 5000},
		{"scope_id": "scope-a", "generation_id": "gen-a", "limit": 10},
	} {
		w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", body)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d for body %#v; body: %s", w.Code, http.StatusBadRequest, body, w.Body.String())
		}
	}
}

func TestListInputInvalidFactsEmpty(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", map[string]any{
		"scope_id":      "scope-a",
		"generation_id": "gen-a",
		"limit":         10,
		"timeout_ms":    5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	got := decodeBody(t, w)
	if got["schema_version"] != "eshu.admin.input_invalid_facts.v1" {
		t.Fatalf("schema_version = %v, want eshu.admin.input_invalid_facts.v1", got["schema_version"])
	}
	if got["truncated"] != false {
		t.Fatalf("truncated = %v, want false", got["truncated"])
	}
	if got["count"].(float64) != 0 {
		t.Fatalf("count = %v, want 0", got["count"])
	}
	if store.inputInvalidFactFilter.ScopeID != "scope-a" || store.inputInvalidFactFilter.GenerationID != "gen-a" {
		t.Fatalf("filter = %#v, want scope-a/gen-a", store.inputInvalidFactFilter)
	}
}

func TestListInputInvalidFactsFiltersAndTruncates(t *testing.T) {
	now := time.Date(2026, 7, 14, 13, 0, 0, 0, time.UTC)
	store := &stubAdminStore{
		inputInvalidFactRows: []AdminReducerInputInvalidFact{
			{
				FactID: "fact-1", FactKind: "aws_resource", MissingField: "account_id",
				FailureClass: "input_invalid", Domain: "aws_resource_materialization",
				ScopeID: "scope-a", GenerationID: "gen-a", DecidedAt: now,
			},
			{FactID: "fact-2", DecidedAt: now.Add(-time.Minute)},
			{FactID: "fact-3", DecidedAt: now.Add(-2 * time.Minute)},
		},
	}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", map[string]any{
		"scope_id":      " scope-a ",
		"generation_id": " gen-a ",
		"domain":        " aws_resource_materialization ",
		"fact_kind":     " aws_resource ",
		"limit":         2,
		"timeout_ms":    7500,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got, want := store.inputInvalidFactFilter.Limit, 3; got != want {
		t.Fatalf("store limit = %d, want %d for limit+1 truncation probe", got, want)
	}
	if got, want := store.inputInvalidFactFilter.Domain, "aws_resource_materialization"; got != want {
		t.Fatalf("domain filter = %q, want %q", got, want)
	}
	if got, want := store.inputInvalidFactFilter.FactKind, "aws_resource"; got != want {
		t.Fatalf("fact_kind filter = %q, want %q", got, want)
	}
	if got, want := store.inputInvalidFactFilter.Timeout, 7500*time.Millisecond; got != want {
		t.Fatalf("timeout = %s, want %s", got, want)
	}

	got := decodeBody(t, w)
	if got["truncated"] != true {
		t.Fatalf("truncated = %v, want true", got["truncated"])
	}
	if got["count"].(float64) != 2 {
		t.Fatalf("count = %v, want 2", got["count"])
	}
	items := got["items"].([]any)
	first := items[0].(map[string]any)
	if first["fact_id"] != "fact-1" || first["missing_field"] != "account_id" {
		t.Fatalf("first item = %#v, want fact-1 with missing_field account_id", first)
	}
}

func TestListInputInvalidFactsScopedGrants(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-a",
		"generation_id": "gen-a",
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
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
	if store.inputInvalidFactCalls != 1 {
		t.Fatalf("store calls = %d, want 1 for a granted scope_id", store.inputInvalidFactCalls)
	}
}

func TestListInputInvalidFactsScopedUngrantedScopeSkipsStore(t *testing.T) {
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-b",
		"generation_id": "gen-a",
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
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
	if store.inputInvalidFactCalls != 0 {
		t.Fatalf("store calls = %d, want 0 for an ungranted scope_id", store.inputInvalidFactCalls)
	}
	got := decodeBody(t, w)
	if got["count"].(float64) != 0 || got["truncated"] != false {
		t.Fatalf("response = %#v, want empty untruncated page", got)
	}
}

func TestListInputInvalidFactsRecordsTelemetry(t *testing.T) {
	meter := metricnoop.NewMeterProvider().Meter("test")
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	store := &stubAdminStore{}
	h := &AdminHandler{Store: store, Instruments: instruments}
	mux := newAdminMux(h)

	w := postJSON(mux, "/api/v0/admin/input-invalid-facts/query", map[string]any{
		"scope_id":      "scope-a",
		"generation_id": "gen-a",
		"limit":         10,
		"timeout_ms":    5000,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}
