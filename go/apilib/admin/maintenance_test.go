// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeMaintenanceStore scripts the three read-only inputs of
// GET /api/v0/admin/reindex/progress: the fleet watermark, the named
// scopes' per-repository watermarks, and each scope's full-generation
// state.
type fakeMaintenanceStore struct {
	watermark     time.Time
	watermarkErr  error
	watermarkGot  string
	watermarkHits int
	perRepo       map[string]time.Time
	perRepoErr    error
	perRepoGot    [][]string
	states        map[string]FullReconcileState
	stateErr      error
	stateCalls    []string
}

func (f *fakeMaintenanceStore) GetReindexState(_ context.Context, ingester string) (ReindexState, error) {
	f.watermarkGot = ingester
	f.watermarkHits++
	if f.watermarkErr != nil {
		return ReindexState{}, f.watermarkErr
	}
	return ReindexState{RequestedAt: f.watermark}, nil
}

func (f *fakeMaintenanceStore) RepositoryReindexRequestedAt(_ context.Context, scopeIDs []string) (map[string]time.Time, error) {
	f.perRepoGot = append(f.perRepoGot, append([]string(nil), scopeIDs...))
	if f.perRepoErr != nil {
		return nil, f.perRepoErr
	}
	out := make(map[string]time.Time, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		if requestedAt, ok := f.perRepo[scopeID]; ok {
			out[scopeID] = requestedAt
		}
	}
	return out, nil
}

func (f *fakeMaintenanceStore) FullReconcileState(_ context.Context, scopeID string) (FullReconcileState, error) {
	f.stateCalls = append(f.stateCalls, scopeID)
	if f.stateErr != nil {
		return FullReconcileState{}, f.stateErr
	}
	return f.states[scopeID], nil
}

func fakeProgressState(stored time.Time, generationID string) FullReconcileState {
	return FullReconcileState{
		HasProjectedFull:   true,
		LastStoredFullAt:   stored,
		ActiveGenerationID: generationID,
	}
}

func serveProgress(t *testing.T, query string, store MaintenanceStore, hook StoreErrorHandler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/reindex/progress"+query, nil)
	rec := httptest.NewRecorder()
	ServeReindexProgress(rec, req, store, hook)
	return rec
}

func TestServeReindexProgressNilStoreIs503(t *testing.T) {
	rec := serveProgress(t, "?scope_id=s", nil, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil store: want 503, got %d", rec.Code)
	}
	body := decodeJSONBody(t, rec)
	if body["error"] != "Service Unavailable" || body["detail"] != "admin store not configured" {
		t.Fatalf("nil store: want {error,detail} envelope, got %v", body)
	}
}

func TestServeReindexProgressBadScopeIs400(t *testing.T) {
	store := &fakeMaintenanceStore{states: map[string]FullReconcileState{}}
	for name, query := range map[string]string{
		"missing": "?",
		"blank":   "?scope_id=scope-a&scope_id=++",
	} {
		if rec := serveProgress(t, query, store, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, rec.Code)
		}
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("scope_id=scope-%d", i)
	}
	if rec := serveProgress(t, "?"+strings.Join(many, "&"), store, nil); rec.Code != http.StatusBadRequest {
		t.Errorf("101 scopes: want 400, got %d", rec.Code)
	}
}

func TestServeReindexProgressBadFleetOverrideIs400(t *testing.T) {
	store := &fakeMaintenanceStore{states: map[string]FullReconcileState{}}
	rec := serveProgress(t, "?scope_id=s&fleet_requested_at=not-a-time", store, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad override: want 400, got %d", rec.Code)
	}
}

func TestServeReindexProgressDefaultWatermarkTrimsAndDedupes(t *testing.T) {
	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	store := &fakeMaintenanceStore{
		watermark: fleet,
		perRepo:   map[string]time.Time{},
		states: map[string]FullReconcileState{
			"scope-a": fakeProgressState(fleet, "gen-a"),
		},
	}
	rec := serveProgress(t, "?scope_id=++scope-a++&scope_id=scope-a", store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if store.watermarkHits != 1 || store.watermarkGot != "repository" {
		t.Fatalf("query: want one live watermark read for ingester repository, got %d for %q", store.watermarkHits, store.watermarkGot)
	}
	if len(store.stateCalls) != 1 || store.stateCalls[0] != "scope-a" {
		t.Fatalf("query: want one deduplicated state read, got %v", store.stateCalls)
	}
	if len(store.perRepoGot) != 1 || len(store.perRepoGot[0]) != 1 || store.perRepoGot[0][0] != "scope-a" {
		t.Fatalf("query: want one watermark read with the trimmed scope, got %v", store.perRepoGot)
	}
}

func TestServeReindexProgressEmptyWatermarkShape(t *testing.T) {
	stored := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	store := &fakeMaintenanceStore{
		perRepo: map[string]time.Time{},
		states: map[string]FullReconcileState{
			"scope-a": fakeProgressState(stored, "gen-a"),
		},
	}
	rec := serveProgress(t, "?scope_id=scope-a", store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["fleet_requested_at"] != nil {
		t.Fatalf("query: want null fleet_requested_at with no watermark, got %v", body["fleet_requested_at"])
	}
	if body["status"] != "complete" {
		t.Fatalf("query: want complete, got %v", body["status"])
	}
	scopes, ok := body["scopes"].([]any)
	if !ok || len(scopes) != 1 {
		t.Fatalf("query: want 1 scope sample, never null, got %v", body["scopes"])
	}
	row, _ := scopes[0].(map[string]any)
	for _, key := range []string{"scope_id", "state", "requested_at", "last_stored_full_at", "active_generation_id"} {
		if _, present := row[key]; !present {
			t.Fatalf("query: want key %q, got %v", key, row)
		}
	}
	if row["state"] != "satisfied" || row["requested_at"] != nil {
		t.Fatalf("query: want satisfied with a null requested_at, got %v", row)
	}
	if row["last_stored_full_at"] != "2026-10-07T12:00:00Z" {
		t.Fatalf("query: want stored full echoed, got %v", row["last_stored_full_at"])
	}
}

func TestServeReindexProgressInProgressShape(t *testing.T) {
	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	store := &fakeMaintenanceStore{
		watermark: fleet,
		perRepo:   map[string]time.Time{},
		states: map[string]FullReconcileState{
			"scope-done":    fakeProgressState(fleet, "gen-done"),
			"scope-pending": fakeProgressState(fleet.Add(-time.Hour), "gen-pending"),
			"scope-fresh":   {},
		},
	}
	rec := serveProgress(t, "?scope_id=scope-done&scope_id=scope-pending&scope_id=scope-fresh", store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["status"] != "incomplete" {
		t.Fatalf("query: want incomplete, got %v", body["status"])
	}
	if body["fleet_requested_at"] != "2026-10-07T12:00:00Z" {
		t.Fatalf("query: want the live watermark echoed, got %v", body["fleet_requested_at"])
	}
	counts, _ := body["counts"].(map[string]any)
	for key, want := range map[string]float64{"satisfied": 1, "pending": 1, "never_seen": 1} {
		if counts[key] != want {
			t.Fatalf("query: want counts.%s %v, got %v", key, want, counts)
		}
	}
	scopes, _ := body["scopes"].([]any)
	if len(scopes) != 3 {
		t.Fatalf("query: want 3 samples, got %v", body["scopes"])
	}
	first, _ := scopes[0].(map[string]any)
	if first["scope_id"] != "scope-pending" || first["state"] != "pending" {
		t.Fatalf("query: want the pending scope first, got %v", first)
	}
	if body["truncated"] != false {
		t.Fatalf("query: want truncated false, got %v", body["truncated"])
	}
}

func TestServeReindexProgressTruncatesSamples(t *testing.T) {
	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	store := &fakeMaintenanceStore{
		watermark: fleet,
		perRepo:   map[string]time.Time{},
		states:    map[string]FullReconcileState{},
	}
	params := make([]string, 0, 12)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("scope-done-%02d", i)
		store.states[id] = fakeProgressState(fleet, "gen-"+id)
		params = append(params, "scope_id="+id)
	}
	store.states["scope-pending"] = fakeProgressState(fleet.Add(-time.Hour), "gen-pending")
	store.states["scope-fresh"] = FullReconcileState{}
	params = append(params, "scope_id=scope-pending", "scope_id=scope-fresh")

	rec := serveProgress(t, "?"+strings.Join(params, "&"), store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	scopes, ok := body["scopes"].([]any)
	if !ok || len(scopes) != 10 {
		t.Fatalf("query: want exactly 10 samples, got %v", body["scopes"])
	}
	if body["truncated"] != true {
		t.Fatalf("query: want truncated true, got %v", body["truncated"])
	}
	counts, _ := body["counts"].(map[string]any)
	if counts["satisfied"] != float64(10) || counts["pending"] != float64(1) || counts["never_seen"] != float64(1) {
		t.Fatalf("query: want counts over every scope, got %v", counts)
	}
}

func TestServeReindexProgressFleetOverrideSkipsLiveRead(t *testing.T) {
	live := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	store := &fakeMaintenanceStore{
		watermark: live,
		perRepo:   map[string]time.Time{},
		states: map[string]FullReconcileState{
			"scope-a": fakeProgressState(live, "gen-a"),
		},
	}
	rec := serveProgress(t, "?scope_id=scope-a&fleet_requested_at=2026-10-07T13:00:00Z", store, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	body := decodeJSONBody(t, rec)
	if body["fleet_requested_at"] != "2026-10-07T13:00:00Z" {
		t.Fatalf("query: want the override echoed, got %v", body["fleet_requested_at"])
	}
	if body["status"] != "incomplete" {
		t.Fatalf("query: want incomplete, got %v", body["status"])
	}
	if store.watermarkHits != 0 {
		t.Fatalf("query: want no live watermark read, got %d", store.watermarkHits)
	}
}

func TestServeReindexProgressStoreErrorIsFixed500(t *testing.T) {
	for name, store := range map[string]*fakeMaintenanceStore{
		"fleet read":   {watermarkErr: fmt.Errorf("pq: password=hunter2 host=db.internal.example")},
		"watermarks":   {perRepoErr: fmt.Errorf("pq: password=hunter2 host=db.internal.example")},
		"scope states": {stateErr: fmt.Errorf("pq: password=hunter2 host=db.internal.example")},
	} {
		rec := serveProgress(t, "?scope_id=scope-a", store, nil)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: want 500, got %d", name, rec.Code)
			continue
		}
		body := decodeJSONBody(t, rec)
		if body["detail"] != "reindex progress query failed" {
			t.Errorf("%s: want fixed message, got %v", name, body)
		}
		for _, fragment := range []string{"hunter2", "db.internal.example", "pq:"} {
			if strings.Contains(rec.Body.String(), fragment) {
				t.Errorf("%s: body leaked backend text %q", name, fragment)
			}
		}
	}
}

func TestServeReindexProgressClientCancelIs499(t *testing.T) {
	store := &fakeMaintenanceStore{watermarkErr: context.Canceled}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/reindex/progress?scope_id=s", nil)
	rec := httptest.NewRecorder()
	ServeReindexProgress(rec, cancelContext(t, req), store, nil)
	if rec.Code != 499 {
		t.Fatalf("client cancel: want 499, got %d", rec.Code)
	}
}

func TestServeReindexProgressStoreErrorHookWins(t *testing.T) {
	store := &fakeMaintenanceStore{watermarkErr: fmt.Errorf("stale reader")}
	hooked := false
	hook := func(w http.ResponseWriter, r *http.Request, err error) bool {
		hooked = true
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"verdict":"fence"}` + "\n"))
		return true
	}
	rec := serveProgress(t, "?scope_id=s", store, hook)
	if !hooked {
		t.Fatal("store error: want the fence hook called")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("store error hook: want hook verdict 503, got %d", rec.Code)
	}
}
