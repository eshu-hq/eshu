// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

// fakeLiveActivityReader is a test double for LiveActivityReader. It records
// the limit it was called with so tests can assert the handler forwards the
// validated/defaulted limit through to the reader.
type fakeLiveActivityReader struct {
	rows        []statuspkg.LiveActivityRow
	truncated   bool
	err         error
	calledLimit int
	calledCount int
}

func (f *fakeLiveActivityReader) ReadLiveActivity(_ context.Context, limit int) ([]statuspkg.LiveActivityRow, bool, error) {
	f.calledLimit = limit
	f.calledCount++
	if f.err != nil {
		return nil, false, f.err
	}
	return f.rows, f.truncated, nil
}

func operationsRequest(t *testing.T, url string, reader *fakeLiveActivityReader, snapshot statuspkg.RawSnapshot) *httptest.ResponseRecorder {
	t.Helper()
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: snapshot},
		LiveActivity: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeOperationsPayload(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body=%s", err, rec.Body.String())
	}
	return payload
}

func liveActivityTestRow(workItemID, sourceKey, sourceDisplay, leaseOwner string, updatedAt time.Time) statuspkg.LiveActivityRow {
	return statuspkg.LiveActivityRow{
		WorkItemID:    workItemID,
		Stage:         "reducer",
		Status:        "claimed",
		Domain:        "workload_materialization",
		LeaseOwner:    leaseOwner,
		AttemptCount:  1,
		UpdatedAt:     updatedAt,
		CreatedAt:     updatedAt.Add(-time.Minute),
		ScopeKind:     "repository",
		CollectorKind: "github",
		SourceSystem:  "github.com",
		SourceKey:     sourceKey,
		SourceDisplay: sourceDisplay,
	}
}

// TestGetOperationsComposesHealthCollectorsQueueAndLiveActivity verifies the
// happy path: the handler composes the status snapshot's health/stage/domain/
// queue sections with the separately-fetched live_activity rows into one 200
// response.
func TestGetOperationsComposesHealthCollectorsQueueAndLiveActivity(t *testing.T) {
	t.Parallel()

	asOf := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	reader := &fakeLiveActivityReader{
		rows: []statuspkg.LiveActivityRow{
			liveActivityTestRow("wi-1", "repository:r_ea78e8bb", "eshu-hq/eshu", "reducer-worker-1", asOf.Add(-5*time.Second)),
		},
	}
	snapshot := statuspkg.RawSnapshot{
		AsOf:  asOf,
		Queue: statuspkg.QueueSnapshot{Total: 10, Outstanding: 3, InFlight: 1},
		StageCounts: []statuspkg.StageStatusCount{
			{Stage: "reducer", Status: "claimed", Count: 1},
		},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{Domain: "workload_materialization", Outstanding: 1, OldestAge: 5 * time.Second},
		},
	}

	rec := operationsRequest(t, "/api/v0/status/operations", reader, snapshot)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	payload := decodeOperationsPayload(t, rec)

	if payload["scoped"] != false {
		t.Fatalf("scoped = %#v, want false for an unscoped request", payload["scoped"])
	}
	if _, ok := payload["health"].(map[string]any); !ok {
		t.Fatalf("health missing: %#v", payload["health"])
	}
	queue, ok := payload["queue"].(map[string]any)
	if !ok || queue["outstanding"].(float64) != 3 {
		t.Fatalf("queue = %#v, want outstanding=3", payload["queue"])
	}
	domains, ok := payload["domain_backlogs"].([]any)
	if !ok || len(domains) != 1 {
		t.Fatalf("domain_backlogs = %#v, want 1 entry", payload["domain_backlogs"])
	}
	activity, ok := payload["live_activity"].([]any)
	if !ok || len(activity) != 1 {
		t.Fatalf("live_activity = %#v, want 1 row", payload["live_activity"])
	}
	row := activity[0].(map[string]any)
	if row["work_item_id"] != "wi-1" || row["source_key"] != "repository:r_ea78e8bb" ||
		row["source_display"] != "eshu-hq/eshu" || row["lease_owner"] != "reducer-worker-1" {
		t.Fatalf("live_activity[0] = %#v, want unredacted repo/worker identity for an unscoped caller", row)
	}
	if row["age_seconds"].(float64) < 4 || row["age_seconds"].(float64) > 6 {
		t.Fatalf("age_seconds = %v, want ~5", row["age_seconds"])
	}
	if payload["truncated"] != false {
		t.Fatalf("truncated = %#v, want false", payload["truncated"])
	}
	if payload["limit"].(float64) != operationsDefaultLimit {
		t.Fatalf("limit = %v, want default %d", payload["limit"], operationsDefaultLimit)
	}
	if reader.calledLimit != operationsDefaultLimit {
		t.Fatalf("reader called with limit = %d, want default %d", reader.calledLimit, operationsDefaultLimit)
	}
}

// TestGetOperationsScopedRedactsRepoAndWorkerIdentity verifies scoped tokens
// see the same aggregate sections and every live_activity field except
// source_key/source_display (repo identity, raw and human-readable) and
// lease_owner (worker identity), which are withheld.
func TestGetOperationsScopedRedactsRepoAndWorkerIdentity(t *testing.T) {
	t.Parallel()

	const secretSourceKey = "repository:r_secret_scoped"
	const secretRepo = "eshu-hq/secret-scoped-repo"
	const secretWorker = "reducer-worker-secret-scoped"
	asOf := time.Date(2026, 7, 12, 3, 0, 0, 0, time.UTC)
	reader := &fakeLiveActivityReader{
		rows: []statuspkg.LiveActivityRow{
			liveActivityTestRow("wi-1", secretSourceKey, secretRepo, secretWorker, asOf.Add(-5*time.Second)),
		},
	}
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{AsOf: asOf}},
		LiveActivity: reader,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	resolver := &fakeScopedTokenResolver{
		context: AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"},
		ok:      true,
	}
	authed := AuthMiddlewareWithScopedTokens("", resolver, mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/operations", nil)
	req.Header.Set("Authorization", "Bearer scoped-token")
	rec := httptest.NewRecorder()
	authed.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("scoped status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	body := rec.Body.String()
	for _, secret := range []string{secretSourceKey, secretRepo, secretWorker, "tenant-a", "workspace-a"} {
		if strings.Contains(body, secret) {
			t.Fatalf("scoped operations read model leaked %q: %s", secret, body)
		}
	}

	payload := decodeOperationsPayload(t, rec)
	if payload["scoped"] != true {
		t.Fatalf("scoped = %#v, want true", payload["scoped"])
	}
	activity := payload["live_activity"].([]any)
	if len(activity) != 1 {
		t.Fatalf("live_activity = %#v, want 1 row (scoped callers still see the row, minus identity)", activity)
	}
	row := activity[0].(map[string]any)
	if row["source_key"] != "" {
		t.Fatalf("scoped live_activity[0].source_key = %#v, want redacted empty string", row["source_key"])
	}
	if row["source_display"] != "" {
		t.Fatalf("scoped live_activity[0].source_display = %#v, want redacted empty string", row["source_display"])
	}
	if row["lease_owner"] != "" {
		t.Fatalf("scoped live_activity[0].lease_owner = %#v, want redacted empty string", row["lease_owner"])
	}
	// Non-identity fields stay visible for scoped callers.
	if row["work_item_id"] != "wi-1" || row["stage"] != "reducer" || row["domain"] != "workload_materialization" {
		t.Fatalf("scoped live_activity[0] over-redacted: %#v", row)
	}
}

// TestGetOperationsLimitValidation verifies the limit query parameter is
// clamped/validated: absent defaults, a valid value is forwarded to the
// reader, and an invalid value produces a 400 without ever calling the reader.
func TestGetOperationsLimitValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantStatus int
		wantLimit  int
		wantCalled bool
	}{
		{name: "absent defaults", query: "", wantStatus: http.StatusOK, wantLimit: operationsDefaultLimit, wantCalled: true},
		{name: "valid value forwarded", query: "?limit=25", wantStatus: http.StatusOK, wantLimit: 25, wantCalled: true},
		{name: "max value forwarded", query: "?limit=500", wantStatus: http.StatusOK, wantLimit: 500, wantCalled: true},
		{name: "zero rejected", query: "?limit=0", wantStatus: http.StatusBadRequest, wantCalled: false},
		{name: "negative rejected", query: "?limit=-1", wantStatus: http.StatusBadRequest, wantCalled: false},
		{name: "over max rejected", query: "?limit=501", wantStatus: http.StatusBadRequest, wantCalled: false},
		{name: "non-numeric rejected", query: "?limit=abc", wantStatus: http.StatusBadRequest, wantCalled: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reader := &fakeLiveActivityReader{}
			rec := operationsRequest(t, "/api/v0/status/operations"+tt.query, reader, statuspkg.RawSnapshot{})
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCalled && reader.calledLimit != tt.wantLimit {
				t.Fatalf("reader called with limit = %d, want %d", reader.calledLimit, tt.wantLimit)
			}
			if !tt.wantCalled && reader.calledCount != 0 {
				t.Fatalf("reader called %d times, want 0 (validation should short-circuit)", reader.calledCount)
			}
		})
	}
}

// TestGetOperationsEmptyLiveActivity verifies an idle pipeline (no in-flight
// work) renders live_activity as an empty array, not null, and truncated
// stays false.
func TestGetOperationsEmptyLiveActivity(t *testing.T) {
	t.Parallel()

	reader := &fakeLiveActivityReader{rows: nil, truncated: false}
	rec := operationsRequest(t, "/api/v0/status/operations", reader, statuspkg.RawSnapshot{})
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	payload := decodeOperationsPayload(t, rec)
	activity, ok := payload["live_activity"].([]any)
	if !ok {
		t.Fatalf("live_activity = %#v (%T), want a JSON array, not null", payload["live_activity"], payload["live_activity"])
	}
	if len(activity) != 0 {
		t.Fatalf("live_activity = %#v, want empty", activity)
	}
	if payload["truncated"] != false {
		t.Fatalf("truncated = %#v, want false", payload["truncated"])
	}
}

// TestGetOperationsLiveActivityReaderError verifies a failing live-activity
// query surfaces as a bounded 500, not a panic or a silently empty board.
func TestGetOperationsLiveActivityReaderError(t *testing.T) {
	t.Parallel()

	reader := &fakeLiveActivityReader{err: errors.New("connection reset")}
	rec := operationsRequest(t, "/api/v0/status/operations", reader, statuspkg.RawSnapshot{})
	if got, want := rec.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
}

// TestGetOperationsStatusReaderUnavailable verifies a nil StatusReader
// produces a bounded 503, matching every sibling status route.
func TestGetOperationsStatusReaderUnavailable(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{LiveActivity: &fakeLiveActivityReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/operations", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
}

// TestGetOperationsLiveActivityReaderUnavailable verifies a nil LiveActivity
// reader also produces a bounded 503 rather than a nil-pointer panic.
func TestGetOperationsLiveActivityReaderUnavailable(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{StatusReader: fakeStatusReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/operations", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body=%s", got, want, rec.Body.String())
	}
}
