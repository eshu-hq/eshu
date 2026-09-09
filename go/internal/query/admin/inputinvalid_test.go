// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	metricnoop "go.opentelemetry.io/otel/metric/noop"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestListInputInvalidFactsRequiresScopeGenerationLimitAndTimeout(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
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
	h := &Handler{Store: store}
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
		inputInvalidFactRows: []InputInvalidFact{
			{
				FactID: "fact-1", FactKind: "aws_resource", MissingField: "account_id",
				FailureClass: "input_invalid", Domain: "aws_resource_materialization",
				ScopeID: "scope-a", GenerationID: "gen-a", DecidedAt: now,
			},
			{FactID: "fact-2", DecidedAt: now.Add(-time.Minute)},
			{FactID: "fact-3", DecidedAt: now.Add(-2 * time.Minute)},
		},
	}
	h := &Handler{Store: store}
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
	h := &Handler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-a",
		"generation_id": "gen-a",
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
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
	if got, want := store.inputInvalidFactFilter.AllowedRepositoryIDs, []string{"repo-a"}; !slices.Equal(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.inputInvalidFactFilter.AllowedScopeIDs, []string{"scope-a"}; !slices.Equal(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

// TestListInputInvalidFactsScopedRepositoryOnlyGrantReachesStore proves the
// codex P2 fix on PR #5252 (issue #4630): a scoped token that requests a
// scope_id NOT in its combined allowed-IDs map must still reach the store
// (and thread AllowedRepositoryIDs/AllowedScopeIDs) rather than being
// rejected by an in-memory pre-check. Authorization for a repository grant
// against a scope_id it does not literally match (because the token grants
// the REPOSITORY identifier, not the raw ingestion scope_id) is delegated to
// the store's SQL join against ingestion_scopes
// (buildListReducerInputInvalidFactsQuery, proven in
// TestBuildListReducerInputInvalidFactsQuery_AuthorizesViaIngestionScopes and
// against real Postgres by TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant).
// Before the fix, this scope_id would never reach the store: the handler's
// own allowsRepositoryID(scopeID) pre-check compared scope_id against the
// combined allowed-IDs map directly and rejected it, always returning an
// empty page for a repository-scoped token reading its own quarantine rows.
func TestListInputInvalidFactsScopedRepositoryOnlyGrantReachesStore(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-b",
		"generation_id": "gen-a",
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:                 queryauth.AuthModeScoped,
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
		t.Fatalf("store calls = %d, want 1: authorization is delegated to the store's SQL join, not an in-memory pre-check", store.inputInvalidFactCalls)
	}
	if got, want := store.inputInvalidFactFilter.ScopeID, "scope-b"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := store.inputInvalidFactFilter.AllowedRepositoryIDs, []string{"repo-a"}; !slices.Equal(got, want) {
		t.Fatalf("AllowedRepositoryIDs = %#v, want %#v", got, want)
	}
	if got, want := store.inputInvalidFactFilter.AllowedScopeIDs, []string{"scope-a"}; !slices.Equal(got, want) {
		t.Fatalf("AllowedScopeIDs = %#v, want %#v", got, want)
	}
}

// TestListInputInvalidFactsScopedEmptyGrantSkipsStore proves the ONE
// remaining in-memory short-circuit: a scoped token with NO grants at all
// (repositoryAccessFilter.empty()) still skips the store entirely, mirroring
// Handler.listDeadLetters' access.empty() check.
func TestListInputInvalidFactsScopedEmptyGrantSkipsStore(t *testing.T) {
	store := &stubAdminStore{}
	h := &Handler{Store: store}
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodPost, "/api/v0/admin/input-invalid-facts/query", strings.NewReader(`{
		"scope_id": "scope-b",
		"generation_id": "gen-a",
		"limit": 10,
		"timeout_ms": 5000
	}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(queryauth.ContextWithAuthContext(req.Context(), queryauth.AuthContext{
		Mode:        queryauth.AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if store.inputInvalidFactCalls != 0 {
		t.Fatalf("store calls = %d, want 0 for a scoped token with no grants at all", store.inputInvalidFactCalls)
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
	h := &Handler{Store: store, Instruments: instruments}
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

// TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant is the
// real-Postgres proof for the codex P2 fix on PR #5252 (issue #4630): a
// repository-scoped token that grants ONLY the repository identifier
// (ingestion_scopes.source_key), never the raw ingestion scope_id, can read
// its own reducer_input_invalid_facts rows. Before the fix, this request
// always returned an empty page: the handler's in-memory
// access.allowsRepositoryID(scopeID) pre-check compared the requested raw
// scope_id against the combined allowed-IDs map, which contains repository
// identifiers, not scope ids, so it could never match and the store was
// never even called.
//
// Run with:
//
//	ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:<port>/eshu \
//	  go test ./internal/query -run TestAdminHandler_InputInvalidFactsQueryLiveRepositoryScopedGrant -count=1
