// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// stubReindexProgressReader scripts the three read-only inputs of
// GET /api/v0/admin/reindex/progress: the fleet watermark, the named scopes'
// per-repository watermarks, and each scope's full-generation state.
type stubReindexProgressReader struct {
	fleetRequestedAt time.Time
	fleetErr         error
	fleetCalls       int
	perRepo          map[string]time.Time
	perRepoErr       error
	perRepoCalls     [][]string
	states           map[string]scope.FullReconcileState
	stateErr         error
	stateCalls       []string
}

func (s *stubReindexProgressReader) GetReindexState(_ context.Context, ingester string) (runtime.ReindexRequest, error) {
	s.fleetCalls++
	if s.fleetErr != nil {
		return runtime.ReindexRequest{}, s.fleetErr
	}
	return runtime.ReindexRequest{Ingester: ingester, State: runtime.RequestStatePending, RequestedAt: s.fleetRequestedAt}, nil
}

func (s *stubReindexProgressReader) RepositoryReindexRequestedAt(_ context.Context, scopeIDs []string) (map[string]time.Time, error) {
	s.perRepoCalls = append(s.perRepoCalls, append([]string(nil), scopeIDs...))
	if s.perRepoErr != nil {
		return nil, s.perRepoErr
	}
	out := make(map[string]time.Time, len(scopeIDs))
	for _, scopeID := range scopeIDs {
		if requestedAt, ok := s.perRepo[scopeID]; ok {
			out[scopeID] = requestedAt
		}
	}
	return out, nil
}

func (s *stubReindexProgressReader) FullReconcileState(_ context.Context, scopeID string) (scope.FullReconcileState, error) {
	s.stateCalls = append(s.stateCalls, scopeID)
	if s.stateErr != nil {
		return scope.FullReconcileState{}, s.stateErr
	}
	return s.states[scopeID], nil
}

func progressState(stored time.Time, generationID string) scope.FullReconcileState {
	return scope.FullReconcileState{
		HasProjectedFull:    true,
		LastStoredFullAt:    stored,
		LastProjectedFullAt: stored,
		HasLatestFull:       true,
		LatestFullAt:        stored,
		ActiveGenerationID:  generationID,
	}
}

func getProgress(t *testing.T, h *Handler, query string) (int, map[string]any) {
	t.Helper()
	mux := newAdminMux(h)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/reindex/progress"+query, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w.Code, decodeBody(t, w)
}

// TestAdminHandler_ReindexProgressClassifiesScopes pins the #7620 satisfied,
// pending, and never-seen trio: a stored full at or after the effective
// watermark is satisfied, an older stored full is pending, and a scope with
// no activated full is never-seen and blocks completion.
func TestAdminHandler_ReindexProgressClassifiesScopes(t *testing.T) {
	t.Parallel()

	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	reader := &stubReindexProgressReader{
		fleetRequestedAt: fleet,
		perRepo:          map[string]time.Time{},
		states: map[string]scope.FullReconcileState{
			"scope-done":    progressState(fleet, "gen-done"),
			"scope-pending": progressState(fleet.Add(-time.Hour), "gen-pending"),
			"scope-fresh":   {},
		},
	}
	h := &Handler{ReindexProgress: reader}

	code, got := getProgress(t, h, "?scope_id=scope-done&scope_id=scope-pending&scope_id=scope-fresh")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %v", code, got)
	}
	if got["status"] != "incomplete" {
		t.Errorf("status = %v, want incomplete: a pending and a never-seen scope block completion", got["status"])
	}
	if got["fleet_requested_at"] != "2026-10-07T12:00:00Z" {
		t.Errorf("fleet_requested_at = %v, want the live fleet watermark echoed", got["fleet_requested_at"])
	}
	counts, _ := got["counts"].(map[string]any)
	for key, want := range map[string]float64{"satisfied": 1, "pending": 1, "never_seen": 1} {
		if counts[key] != want {
			t.Errorf("counts.%s = %v, want %v", key, counts[key], want)
		}
	}
	scopes, _ := got["scopes"].([]any)
	if len(scopes) != 3 {
		t.Fatalf("scopes has %d entries, want 3", len(scopes))
	}
	byID := make(map[string]map[string]any, len(scopes))
	for _, entry := range scopes {
		row, _ := entry.(map[string]any)
		byID[row["scope_id"].(string)] = row
	}
	if row := byID["scope-done"]; row["state"] != "satisfied" ||
		row["requested_at"] != "2026-10-07T12:00:00Z" ||
		row["last_stored_full_at"] != "2026-10-07T12:00:00Z" ||
		row["active_generation_id"] != "gen-done" {
		t.Errorf("scope-done row = %v, want satisfied with echoed watermarks", row)
	}
	if row := byID["scope-pending"]; row["state"] != "pending" ||
		row["requested_at"] != "2026-10-07T12:00:00Z" ||
		row["last_stored_full_at"] != "2026-10-07T11:00:00Z" {
		t.Errorf("scope-pending row = %v, want pending against the fleet watermark", row)
	}
	if row := byID["scope-fresh"]; row["state"] != "never_seen" ||
		row["last_stored_full_at"] != nil ||
		row["active_generation_id"] != "" {
		t.Errorf("scope-fresh row = %v, want never_seen with a null stored full", row)
	}
	if got["truncated"] != false {
		t.Errorf("truncated = %v, want false when every scope is listed", got["truncated"])
	}
	if reader.fleetCalls != 1 {
		t.Errorf("fleet reads = %d, want 1: the live watermark is read once", reader.fleetCalls)
	}
	if len(reader.perRepoCalls) != 1 || len(reader.perRepoCalls[0]) != 3 {
		t.Errorf("per-repository reads = %v, want one call with the 3 scopes", reader.perRepoCalls)
	}
}

// TestAdminHandler_ReindexProgressEffectiveWatermarkIsGreatest pins the
// GREATEST subsumption the collector applies
// (reindexWatermarkFor, selection_baseline.go): a per-repository watermark
// newer than the fleet wins for its scope, while an older one stays subsumed
// and the scope is compared against the fleet.
func TestAdminHandler_ReindexProgressEffectiveWatermarkIsGreatest(t *testing.T) {
	t.Parallel()

	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	newer := fleet.Add(time.Hour)
	older := fleet.Add(-time.Hour)
	reader := &stubReindexProgressReader{
		fleetRequestedAt: fleet,
		perRepo: map[string]time.Time{
			// Newer than the fleet: wins, but the stored full is older still.
			"scope-named": newer,
			// Older than the fleet: subsumed; the stored full answers the fleet.
			"scope-subsumed": older,
		},
		states: map[string]scope.FullReconcileState{
			"scope-named":    progressState(fleet.Add(30*time.Minute), "gen-named"),
			"scope-subsumed": progressState(fleet, "gen-subsumed"),
		},
	}
	h := &Handler{ReindexProgress: reader}

	code, got := getProgress(t, h, "?scope_id=scope-named&scope_id=scope-subsumed")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %v", code, got)
	}
	scopes, _ := got["scopes"].([]any)
	byID := make(map[string]map[string]any, len(scopes))
	for _, entry := range scopes {
		row, _ := entry.(map[string]any)
		byID[row["scope_id"].(string)] = row
	}
	if row := byID["scope-named"]; row["state"] != "pending" || row["requested_at"] != "2026-10-07T13:00:00Z" {
		t.Errorf("scope-named row = %v, want pending against its newer per-repository watermark", row)
	}
	if row := byID["scope-subsumed"]; row["state"] != "satisfied" || row["requested_at"] != "2026-10-07T12:00:00Z" {
		t.Errorf("scope-subsumed row = %v, want satisfied against the subsuming fleet watermark", row)
	}
}

// TestAdminHandler_ReindexProgressReprojectionAckDoesNotSatisfy pins ruling
// a9-125 on the progress surface: a re-projection Ack moves
// LastProjectedFullAt but never LastStoredFullAt, so a scope whose newest
// activated full was ingested before the watermark stays pending.
func TestAdminHandler_ReindexProgressReprojectionAckDoesNotSatisfy(t *testing.T) {
	t.Parallel()

	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	reader := &stubReindexProgressReader{
		fleetRequestedAt: fleet,
		perRepo:          map[string]time.Time{},
		states: map[string]scope.FullReconcileState{
			"scope-reprojected": {
				HasProjectedFull:    true,
				LastStoredFullAt:    fleet.Add(-time.Hour),
				LastProjectedFullAt: fleet.Add(time.Hour),
				HasLatestFull:       true,
				ActiveGenerationID:  "gen-reprojected",
			},
		},
	}
	h := &Handler{ReindexProgress: reader}

	code, got := getProgress(t, h, "?scope_id=scope-reprojected")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %v", code, got)
	}
	if got["status"] != "incomplete" {
		t.Errorf("status = %v, want incomplete: a re-projection Ack must not answer the watermark", got["status"])
	}
	scopes, _ := got["scopes"].([]any)
	if len(scopes) != 1 {
		t.Fatalf("scopes has %d entries, want 1", len(scopes))
	}
	row, _ := scopes[0].(map[string]any)
	if row["state"] != "pending" || row["last_stored_full_at"] != "2026-10-07T11:00:00Z" {
		t.Errorf("row = %v, want pending against the stored full, not the Ack", row)
	}
}

// TestAdminHandler_ReindexProgressCompletes pins the complete answer: every
// named scope stored a full at or after its effective watermark.
func TestAdminHandler_ReindexProgressCompletes(t *testing.T) {
	t.Parallel()

	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	reader := &stubReindexProgressReader{
		fleetRequestedAt: fleet,
		perRepo:          map[string]time.Time{},
		states: map[string]scope.FullReconcileState{
			"scope-a": progressState(fleet, "gen-a"),
			"scope-b": progressState(fleet.Add(time.Hour), "gen-b"),
		},
	}
	h := &Handler{ReindexProgress: reader}

	code, got := getProgress(t, h, "?scope_id=scope-a&scope_id=scope-b")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %v", code, got)
	}
	if got["status"] != "complete" {
		t.Errorf("status = %v, want complete", got["status"])
	}
	counts, _ := got["counts"].(map[string]any)
	if counts["satisfied"] != float64(2) || counts["pending"] != float64(0) || counts["never_seen"] != float64(0) {
		t.Errorf("counts = %v, want all satisfied", counts)
	}
}

// TestAdminHandler_ReindexProgressFleetOverride pins the optional
// fleet_requested_at override: it replaces the live watermark for every
// scope's comparison and is echoed back, without reading live state.
func TestAdminHandler_ReindexProgressFleetOverride(t *testing.T) {
	t.Parallel()

	live := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	reader := &stubReindexProgressReader{
		fleetRequestedAt: live,
		perRepo:          map[string]time.Time{},
		states: map[string]scope.FullReconcileState{
			"scope-a": progressState(live, "gen-a"),
		},
	}
	h := &Handler{ReindexProgress: reader}

	code, got := getProgress(t, h, "?scope_id=scope-a&fleet_requested_at=2026-10-07T13:00:00Z")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %v", code, got)
	}
	if got["fleet_requested_at"] != "2026-10-07T13:00:00Z" {
		t.Errorf("fleet_requested_at = %v, want the override echoed", got["fleet_requested_at"])
	}
	if got["status"] != "incomplete" {
		t.Errorf("status = %v, want incomplete: the stored full predates the override", got["status"])
	}
	if reader.fleetCalls != 0 {
		t.Errorf("fleet reads = %d, want 0: the override replaces the live read", reader.fleetCalls)
	}

	if code, _ := getProgress(t, h, "?scope_id=scope-a&fleet_requested_at=not-a-time"); code != http.StatusBadRequest {
		t.Errorf("bad override status = %d, want 400", code)
	}
}

// TestAdminHandler_ReindexProgressBoundsScopeIDs pins the request bounds: at
// least one scope_id, at most 100 like a repository-scoped request, none
// blank, with duplicates collapsed before anything is read.
func TestAdminHandler_ReindexProgressBoundsScopeIDs(t *testing.T) {
	t.Parallel()

	reader := &stubReindexProgressReader{
		perRepo: map[string]time.Time{},
		states:  map[string]scope.FullReconcileState{"scope-a": {}},
	}
	h := &Handler{ReindexProgress: reader}

	for name, query := range map[string]string{
		"missing": "?",
		"blank":   "?scope_id=scope-a&scope_id=++",
	} {
		if code, _ := getProgress(t, h, query); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", name, code)
		}
	}
	many := make([]string, 101)
	for i := range many {
		many[i] = fmt.Sprintf("scope_id=scope-%d", i)
	}
	if code, _ := getProgress(t, h, "?"+strings.Join(many, "&")); code != http.StatusBadRequest {
		t.Errorf("101 scopes: status = %d, want 400", code)
	}

	reader2 := &stubReindexProgressReader{
		perRepo: map[string]time.Time{},
		states:  map[string]scope.FullReconcileState{"scope-a": {}},
	}
	code, got := getProgress(t, &Handler{ReindexProgress: reader2}, "?scope_id=scope-a&scope_id=scope-a")
	if code != http.StatusOK {
		t.Fatalf("duplicates: status = %d, want 200; body: %v", code, got)
	}
	counts, _ := got["counts"].(map[string]any)
	if total := counts["satisfied"].(float64) + counts["pending"].(float64) + counts["never_seen"].(float64); total != 1 {
		t.Errorf("duplicate scope counted %v times, want 1", total)
	}
	if len(reader2.stateCalls) != 1 {
		t.Errorf("state reads = %v, want one read for the deduplicated scope", reader2.stateCalls)
	}
}

// TestAdminHandler_ReindexProgressTruncatesSamples pins the per-scope detail
// cap: at most 10 samples, never null, with unfinished scopes first and
// truncated set when scopes were left out.
func TestAdminHandler_ReindexProgressTruncatesSamples(t *testing.T) {
	t.Parallel()

	fleet := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	reader := &stubReindexProgressReader{
		fleetRequestedAt: fleet,
		perRepo:          map[string]time.Time{},
		states:           map[string]scope.FullReconcileState{},
	}
	params := make([]string, 0, 12)
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("scope-done-%02d", i)
		reader.states[id] = progressState(fleet, "gen-"+id)
		params = append(params, "scope_id="+id)
	}
	reader.states["scope-pending"] = progressState(fleet.Add(-time.Hour), "gen-pending")
	reader.states["scope-fresh"] = scope.FullReconcileState{}
	params = append(params, "scope_id=scope-pending", "scope_id=scope-fresh")
	h := &Handler{ReindexProgress: reader}

	code, got := getProgress(t, h, "?"+strings.Join(params, "&"))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %v", code, got)
	}
	if got["status"] != "incomplete" {
		t.Errorf("status = %v, want incomplete", got["status"])
	}
	scopes, ok := got["scopes"].([]any)
	if !ok || len(scopes) != 10 {
		t.Fatalf("scopes has %v entries, want exactly 10 samples", len(scopes))
	}
	first, _ := scopes[0].(map[string]any)
	second, _ := scopes[1].(map[string]any)
	if first["scope_id"] != "scope-pending" || first["state"] != "pending" {
		t.Errorf("scopes[0] = %v, want the pending scope first", first)
	}
	if second["scope_id"] != "scope-fresh" || second["state"] != "never_seen" {
		t.Errorf("scopes[1] = %v, want the never-seen scope second", second)
	}
	if got["truncated"] != true {
		t.Errorf("truncated = %v, want true when 12 scopes collapse to 10 samples", got["truncated"])
	}
	counts, _ := got["counts"].(map[string]any)
	if counts["satisfied"] != float64(10) || counts["pending"] != float64(1) || counts["never_seen"] != float64(1) {
		t.Errorf("counts = %v, want every evaluated scope counted, not only the samples", counts)
	}
}

// TestAdminHandler_ReindexProgressNoWatermark pins the never-requested case:
// with no fleet watermark and no per-repository rows the effective watermark
// is zero, echoed as null, and scopes with a stored full are satisfied.
func TestAdminHandler_ReindexProgressNoWatermark(t *testing.T) {
	t.Parallel()

	stored := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	reader := &stubReindexProgressReader{
		perRepo: map[string]time.Time{},
		states: map[string]scope.FullReconcileState{
			"scope-a": progressState(stored, "gen-a"),
		},
	}
	h := &Handler{ReindexProgress: reader}

	code, got := getProgress(t, h, "?scope_id=scope-a")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %v", code, got)
	}
	if got["fleet_requested_at"] != nil {
		t.Errorf("fleet_requested_at = %v, want null with no fleet watermark", got["fleet_requested_at"])
	}
	if got["status"] != "complete" {
		t.Errorf("status = %v, want complete: a stored full answers a zero watermark", got["status"])
	}
	scopes, _ := got["scopes"].([]any)
	if len(scopes) != 1 {
		t.Fatalf("scopes has %d entries, want 1, never null", len(scopes))
	}
	row, _ := scopes[0].(map[string]any)
	if row["state"] != "satisfied" || row["requested_at"] != nil {
		t.Errorf("row = %v, want satisfied with a null requested_at", row)
	}
}

// TestAdminHandler_ReindexProgressFailures pins the failure contract: an
// unwired reader is a 503, a backend fault is a 500 with the fixed message
// and no backend text, and a client cancel is a 499.
func TestAdminHandler_ReindexProgressFailures(t *testing.T) {
	t.Parallel()

	if code, _ := getProgress(t, &Handler{}, "?scope_id=scope-a"); code != http.StatusServiceUnavailable {
		t.Errorf("unwired status = %d, want 503", code)
	}
	for name, reader := range map[string]*stubReindexProgressReader{
		"fleet read":   {fleetErr: errors.New("pq: password=hunter2 host=db.internal.example")},
		"watermarks":   {perRepoErr: errors.New("pq: password=hunter2 host=db.internal.example")},
		"scope states": {stateErr: errors.New("pq: password=hunter2 host=db.internal.example")},
	} {
		mux := newAdminMux(&Handler{ReindexProgress: reader})
		req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/reindex/progress?scope_id=scope-a", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500", name, w.Code)
		}
		body := decodeBody(t, w)
		detail, _ := body["detail"].(string)
		if detail != reindexProgressFailedMessage {
			t.Errorf("%s: detail = %q, want the fixed %q", name, detail, reindexProgressFailedMessage)
		}
		for _, fragment := range []string{"hunter2", "db.internal.example", "pq:"} {
			if strings.Contains(w.Body.String(), fragment) {
				t.Errorf("%s: body leaked backend error text %q", name, fragment)
			}
		}
	}

	canceled := &stubReindexProgressReader{
		fleetErr: fmt.Errorf("canceled read: %w", context.Canceled),
	}
	mux := newAdminMux(&Handler{ReindexProgress: canceled})
	ctx, stop := context.WithCancel(context.Background())
	stop()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/reindex/progress?scope_id=scope-a", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != querycontract.StatusClientClosedRequest {
		t.Errorf("cancel status = %d, want 499", w.Code)
	}
}
