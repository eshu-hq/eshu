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
)

// fakeDeltaUnionStore answers the synchronous completeness check with a
// canned report or a canned error, recording the scope filters it saw.
type fakeDeltaUnionStore struct {
	report DeltaUnionCompletenessReport
	err    error
	got    [][]string
	calls  int
}

func (f *fakeDeltaUnionStore) CheckDeltaUnionCompleteness(_ context.Context, scopeIDs []string) (DeltaUnionCompletenessReport, error) {
	f.got = append(f.got, append([]string(nil), scopeIDs...))
	f.calls++
	return f.report, f.err
}

func completeDeltaUnionReport() DeltaUnionCompletenessReport {
	return DeltaUnionCompletenessReport{
		Repos: []DeltaUnionRepoReport{
			{
				RepoID:      "repo-1",
				ScopeID:     "scope-1",
				Outcome:     DeltaUnionRepoComplete,
				Base:        DeltaUnionGeneration{GenerationID: "gen-base", Status: "superseded"},
				Active:      DeltaUnionGeneration{GenerationID: "gen-delta", Status: "active", IsDelta: true},
				GraphFiles:  2,
				ActiveFiles: 2,
				Annotation:  "graph holds exactly the ACTIVE set",
			},
		},
	}
}

func serveCompleteness(t *testing.T, query string, store DeltaUnionStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness"+query, nil)
	rec := httptest.NewRecorder()
	ServeDeltaUnionCompleteness(rec, req, store, hook)
	return rec
}

func TestServeDeltaUnionCompletenessNilStoreIs503(t *testing.T) {
	rec := serveCompleteness(t, "?scope=scope-1", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServeDeltaUnionCompletenessMissingScopeIs400(t *testing.T) {
	store := &fakeDeltaUnionStore{}
	for _, query := range []string{"", "?scope=++"} {
		rec := serveCompleteness(t, query, store, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("query %q: want 400, got %d", query, rec.Code)
		}
		body := decodeJSONBody(t, rec)
		if body["detail"] != "delta union completeness requires at least one explicit scope filter: pass at least one ?scope= query value" {
			t.Fatalf("query %q: want scope-required detail, got %v", query, body)
		}
	}
	if store.calls != 0 {
		t.Fatalf("missing scope: want no store call, got %d", store.calls)
	}
}

func TestServeDeltaUnionCompletenessTrimsScopes(t *testing.T) {
	store := &fakeDeltaUnionStore{report: completeDeltaUnionReport()}
	rec := serveCompleteness(t, "?scope=++scope-1++&scope=scope-1", store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if len(store.got) != 1 || len(store.got[0]) != 2 || store.got[0][0] != "scope-1" {
		t.Fatalf("query: want trimmed scopes passed through, got %v", store.got)
	}
}

func TestServeDeltaUnionCompletenessOverCapIsNarrowFilter500(t *testing.T) {
	store := &fakeDeltaUnionStore{}
	query := "?scope=" + strings.Repeat("s&scope=", 500) + "s"
	rec := serveCompleteness(t, query, store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("over cap: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "delta union completeness covers 501 scopes, past the 500 cap: narrow the scope filter" {
		t.Fatalf("over cap: want narrow-filter detail, got %v", body)
	}
	if store.calls != 0 {
		t.Fatalf("over cap: want no store call, got %d", store.calls)
	}
}

func TestServeDeltaUnionCompletenessStoreCapErrorIsNarrowFilter500(t *testing.T) {
	store := &fakeDeltaUnionStore{err: &DeltaUnionCapError{Count: 502, Limit: 500, Unit: "repositories"}}
	rec := serveCompleteness(t, "?scope=scope-1", store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("cap error: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "delta union completeness covers 502 repositories, past the 500 cap: narrow the scope filter" {
		t.Fatalf("cap error: want narrow-filter detail, got %v", body)
	}
}

func TestServeDeltaUnionCompletenessSuccessShape(t *testing.T) {
	store := &fakeDeltaUnionStore{report: completeDeltaUnionReport()}
	rec := serveCompleteness(t, "?scope=scope-1", store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	repos, ok := body["repos"].([]any)
	if !ok || len(repos) != 1 {
		t.Fatalf("query: want 1 repo, got %v", body["repos"])
	}
	if body["mismatches"] != float64(0) {
		t.Fatalf("query: want mismatches 0, got %v", body["mismatches"])
	}
	first, _ := repos[0].(map[string]any)
	for _, key := range []string{"repo_id", "scope_id", "outcome", "base", "active", "graph_files", "active_files", "annotation"} {
		if _, present := first[key]; !present {
			t.Fatalf("query: want key %q, got %v", key, first)
		}
	}
	if first["outcome"] != "complete" {
		t.Fatalf("query: want outcome complete, got %v", first["outcome"])
	}
	// Generation blocks keep the core capitalized keys: the recovery type
	// carries no JSON tags.
	active, _ := first["active"].(map[string]any)
	if active["GenerationID"] != "gen-delta" || active["Status"] != "active" || active["IsDelta"] != true {
		t.Fatalf("query: want core generation keys, got %v", active)
	}
	for _, key := range []string{"missing", "extra", "drifted"} {
		if _, present := first[key]; present {
			t.Fatalf("query: want key %q omitted when empty, got %v", key, first)
		}
	}
}

func TestServeDeltaUnionCompletenessMismatchShape(t *testing.T) {
	store := &fakeDeltaUnionStore{report: DeltaUnionCompletenessReport{
		Repos: []DeltaUnionRepoReport{
			{
				RepoID: "repo-1", ScopeID: "scope-1", Outcome: DeltaUnionRepoMismatch,
				Base:        DeltaUnionGeneration{GenerationID: "gen-base", Status: "superseded"},
				Active:      DeltaUnionGeneration{GenerationID: "gen-delta", Status: "active", IsDelta: true},
				GraphFiles:  3,
				ActiveFiles: 2,
				Missing:     []string{"x.go"},
				Extra:       []string{"w.go"},
				Drifted:     []string{"keep.go"},
				Annotation:  "re-parse scope-1 generations gen-delta over gen-base",
			},
		},
		Mismatches: 1,
	}}
	rec := serveCompleteness(t, "?scope=scope-1", store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["mismatches"] != float64(1) {
		t.Fatalf("query: want mismatches 1, got %v", body["mismatches"])
	}
	repos, _ := body["repos"].([]any)
	first, _ := repos[0].(map[string]any)
	if first["outcome"] != "mismatch" {
		t.Fatalf("query: want outcome mismatch, got %v", first["outcome"])
	}
	for key, want := range map[string]string{"missing": "x.go", "extra": "w.go", "drifted": "keep.go"} {
		items, _ := first[key].([]any)
		if len(items) != 1 || items[0] != want {
			t.Fatalf("query: want %s [%s], got %v", key, want, first[key])
		}
	}
}

func TestServeDeltaUnionCompletenessGraphDownIsFixed500(t *testing.T) {
	store := &fakeDeltaUnionStore{err: errors.New("graph: connection refused host=db-graph.internal password=hunter2")}
	rec := serveCompleteness(t, "?scope=scope-1", store, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("graph down: want 500, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["detail"] != "delta-union completeness check failed" {
		t.Fatalf("graph down: want fixed message, got %v", body)
	}
	if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), `"complete"`) {
		t.Fatalf("graph down: body must hide backend text and never claim complete: %s", rec.Body.String())
	}
}

func TestServeDeltaUnionCompletenessClientCancelIs499(t *testing.T) {
	store := &fakeDeltaUnionStore{err: context.Canceled}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness?scope=s", nil)
	rec := httptest.NewRecorder()
	ServeDeltaUnionCompleteness(rec, cancelContext(t, req), store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
}

func TestServeDeltaUnionCompletenessStoreErrorHookWins(t *testing.T) {
	store := &fakeDeltaUnionStore{err: errors.New("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := serveCompleteness(t, "?scope=scope-1", store, hook)
	if !hooked {
		t.Fatal("store error: want the fence hook called")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error hook: want hook verdict 503, got %d", rec.Code)
	}
}
