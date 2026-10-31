// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	awsfreshness "github.com/eshu-hq/eshu/go/internal/coordinator/aws/freshness"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// TestIntakeRequiresBearerAuth is the #9377 RED test: every intake route
// must reject requests without a valid bearer token, and must never accept
// an empty server token (fail-closed configuration).
func TestIntakeRequiresBearerAuth(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Token: "core-intake-token"}, testStores().stores())
	mux := server.Mux()
	paths := []string{
		"/api/v1/webhook/intake/repository",
		"/api/v1/webhook/intake/incident",
		"/api/v1/webhook/intake/aws",
		"/api/v1/webhook/intake/gcp",
		"/api/v1/webhook/intake/actions",
	}
	body := []byte(`{"api_version":"v1","provider":"github"}`)
	for _, path := range paths {
		for _, name := range []string{"missing", "wrong"} {
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
			if name == "wrong" {
				req.Header.Set("Authorization", "Bearer wrong-token")
			}
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("POST %s auth=%s = %d, want 401", path, name, rec.Code)
			}
		}
	}
}

// TestIntakeRefusesEmptyToken is the #9377 RED test: a server configured
// without a token must fail closed at construction.
func TestIntakeRefusesEmptyToken(t *testing.T) {
	t.Parallel()

	if _, err := NewServerChecked(ServerConfig{}, testStores().stores()); err == nil {
		t.Fatal("NewServerChecked() with empty token = nil, want fail-closed error")
	}
}

// TestIntakeRejectsUnsupportedAPIVersion is the #9377 RED test: the
// versioned contract accepts absent (oldest client) or "v1" api_version and
// rejects anything else without touching durable storage.
func TestIntakeRejectsUnsupportedAPIVersion(t *testing.T) {
	t.Parallel()

	stores := testStores()
	server := NewServer(ServerConfig{Token: "core-intake-token"}, stores.stores())
	mux := server.Mux()

	body := []byte(`{"api_version":"v2","provider":"github","delivery_id":"d1","repository_external_id":"123","repository_full_name":"o/r","default_branch":"main","target_sha":"abc"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/intake/repository", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer core-intake-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("api_version v2 = %d, want 400", rec.Code)
	}
	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if decoded["code"] != "unsupported_api_version" {
		t.Fatalf("error code = %#v, want unsupported_api_version", decoded["code"])
	}
	if stores.repository.calls != 0 {
		t.Fatalf("repository store calls = %d, want 0 (rejected before storage)", stores.repository.calls)
	}
}

// TestIntakeRejectsOversizedBody is the #9377 RED test: bodies past the
// intake cap fail with 413, other unreadable bodies with 400.
func TestIntakeRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Token: "core-intake-token", MaxBodyBytes: 64}, testStores().stores())
	mux := server.Mux()

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/webhook/intake/repository",
		bytes.NewReader(bytes.Repeat([]byte("x"), 65)),
	)
	req.Header.Set("Authorization", "Bearer core-intake-token")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d, want 413", rec.Code)
	}
}

// TestIntakeRepositoryAcceptsGitHubTrigger is the #9377 RED test: a valid
// normalized repository trigger through the API persists via the core store
// and returns a durable receipt.
func TestIntakeRepositoryAcceptsGitHubTrigger(t *testing.T) {
	t.Parallel()

	stores := testStores()
	server := NewServer(ServerConfig{Token: "core-intake-token"}, stores.stores())
	mux := server.Mux()

	rec := postIntake(t, mux, "/api/v1/webhook/intake/repository", map[string]any{
		"api_version":            "v1",
		"provider":               "github",
		"delivery_id":            "delivery-1",
		"event_kind":             "push",
		"decision":               "accepted",
		"repository_external_id": "123",
		"repository_full_name":   "octo/repo",
		"default_branch":         "main",
		"target_sha":             "abc123",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid repository intake = %d (%s), want 202", rec.Code, rec.Body.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
		t.Fatalf("decode receipt: %v", err)
	}
	if receipt["api_version"] != APIVersionV1 {
		t.Fatalf("receipt api_version = %#v, want v1", receipt["api_version"])
	}
	if receipt["trigger_id"] == "" || receipt["trigger_id"] == nil {
		t.Fatalf("receipt missing trigger_id: %s", rec.Body.String())
	}
	if got := stores.repository.calls; got != 1 {
		t.Fatalf("repository store calls = %d, want 1", got)
	}
}

// TestIntakeRejectsRouteFamilyMismatch is the #9377 RED test: a provider
// outside the route's family fails closed before storage.
func TestIntakeRejectsRouteFamilyMismatch(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Token: "core-intake-token"}, testStores().stores())
	mux := server.Mux()

	rec := postIntake(t, mux, "/api/v1/webhook/intake/repository", map[string]any{
		"api_version":            "v1",
		"provider":               "pagerduty",
		"delivery_id":            "delivery-1",
		"repository_external_id": "123",
		"repository_full_name":   "octo/repo",
		"default_branch":         "main",
		"target_sha":             "abc123",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("family mismatch = %d, want 400", rec.Code)
	}
}

// TestIntakeActionsConflictReturns409 is the #9377 RED test: a conflicting
// actions delivery identity through the API surfaces 409 so the listener
// can preserve its exact-duplicate-202/conflict-409 contract.
func TestIntakeActionsConflictReturns409(t *testing.T) {
	t.Parallel()

	stores := testStores()
	stores.actions.err = webhook.ErrActionsDeliveryConflict
	server := NewServer(ServerConfig{Token: "core-intake-token"}, stores.stores())
	mux := server.Mux()

	rec := postIntake(t, mux, "/api/v1/webhook/intake/actions", map[string]any{
		"api_version":   "v1",
		"provider":      "github",
		"delivery_id":   "delivery-1",
		"event_kind":    "workflow_run",
		"action":        "completed",
		"repository_id": "123",
		"repository":    "octo/repo",
		"run_id":        "42",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting actions delivery = %d (%s), want 409", rec.Code, rec.Body.String())
	}
}

// TestIntakeStoreFailureReturns500 is the #9377 RED test: a durable-store
// failure through the API surfaces 500 so the provider redelivers (durable
// retry without direct core-table mutation on the extracted path).
func TestIntakeStoreFailureReturns500(t *testing.T) {
	t.Parallel()

	stores := testStores()
	stores.repository.err = errors.New("postgres unavailable")
	server := NewServer(ServerConfig{Token: "core-intake-token"}, stores.stores())
	mux := server.Mux()

	rec := postIntake(t, mux, "/api/v1/webhook/intake/repository", map[string]any{
		"api_version":            "v1",
		"provider":               "github",
		"delivery_id":            "delivery-1",
		"event_kind":             "push",
		"decision":               "accepted",
		"repository_external_id": "123",
		"repository_full_name":   "octo/repo",
		"default_branch":         "main",
		"target_sha":             "abc123",
	})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store failure = %d, want 500", rec.Code)
	}
}

// TestIntakeIgnoresUnknownFields is the #9377 RED test for oldest/newest
// compatibility: a newer client sending fields this server does not know
// must still be accepted.
func TestIntakeIgnoresUnknownFields(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{Token: "core-intake-token"}, testStores().stores())
	mux := server.Mux()

	rec := postIntake(t, mux, "/api/v1/webhook/intake/repository", map[string]any{
		"api_version":            "v1",
		"provider":               "github",
		"delivery_id":            "delivery-1",
		"event_kind":             "push",
		"decision":               "accepted",
		"repository_external_id": "123",
		"repository_full_name":   "octo/repo",
		"default_branch":         "main",
		"target_sha":             "abc123",
		"future_field":           "from-a-newer-client",
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("unknown field = %d (%s), want 202", rec.Code, rec.Body.String())
	}
}

func postIntake(t *testing.T, mux http.Handler, path string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal intake payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer core-intake-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

type testStoreSet struct {
	repository *fakeRepositoryStore
	incident   *fakeIncidentStore
	aws        *fakeAWSStore
	gcp        *fakeGCPStore
	actions    *fakeActionsStore
}

func testStores() *testStoreSet {
	return &testStoreSet{
		repository: &fakeRepositoryStore{},
		incident:   &fakeIncidentStore{},
		aws:        &fakeAWSStore{},
		gcp:        &fakeGCPStore{},
		actions:    &fakeActionsStore{},
	}
}

func (s *testStoreSet) stores() Stores {
	return Stores{
		Repository: s.repository,
		Incident:   s.incident,
		AWS:        s.aws,
		GCP:        s.gcp,
		Actions:    s.actions,
	}
}

type fakeRepositoryStore struct {
	calls    int
	err      error
	received []webhook.Trigger
}

func (f *fakeRepositoryStore) StoreTrigger(_ context.Context, trigger webhook.Trigger, _ time.Time) (webhook.StoredTrigger, error) {
	f.calls++
	f.received = append(f.received, trigger)
	if f.err != nil {
		return webhook.StoredTrigger{}, f.err
	}
	return webhook.StoredTrigger{
		Trigger:        trigger,
		TriggerID:      fmt.Sprintf("trigger-%d", f.calls),
		DeliveryKey:    string(trigger.Provider) + ":" + trigger.DeliveryID,
		RefreshKey:     "refresh-key",
		Status:         statusForDecision(trigger.Decision),
		DuplicateCount: 0,
	}, nil
}

func statusForDecision(decision webhook.Decision) webhook.TriggerStatus {
	if decision == webhook.DecisionIgnored {
		return webhook.TriggerStatusIgnored
	}
	return webhook.TriggerStatusQueued
}

type fakeIncidentStore struct {
	calls int
	err   error
}

func (f *fakeIncidentStore) StoreIncidentFreshnessTrigger(_ context.Context, trigger webhook.IncidentFreshnessTrigger, receivedAt time.Time) (webhook.StoredIncidentFreshnessTrigger, error) {
	f.calls++
	if f.err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, f.err
	}
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(trigger, receivedAt)
	if err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, err
	}
	return stored, nil
}

type fakeAWSStore struct {
	calls int
	err   error
}

func (f *fakeAWSStore) StoreTrigger(_ context.Context, trigger awsfreshness.Trigger, receivedAt time.Time) (awsfreshness.StoredTrigger, error) {
	f.calls++
	if f.err != nil {
		return awsfreshness.StoredTrigger{}, f.err
	}
	return awsfreshness.StoredTrigger{
		Trigger:    trigger,
		TriggerID:  fmt.Sprintf("aws-trigger-%d", f.calls),
		Status:     awsfreshness.TriggerStatusQueued,
		ReceivedAt: receivedAt,
		UpdatedAt:  receivedAt,
	}, nil
}

type fakeGCPStore struct {
	calls int
	err   error
}

func (f *fakeGCPStore) StoreTrigger(_ context.Context, trigger gcpfreshness.Trigger, receivedAt time.Time) (gcpfreshness.StoredTrigger, error) {
	f.calls++
	if f.err != nil {
		return gcpfreshness.StoredTrigger{}, f.err
	}
	return gcpfreshness.StoredTrigger{
		Trigger:    trigger,
		TriggerID:  fmt.Sprintf("gcp-trigger-%d", f.calls),
		Status:     catalog.TriggerStatusQueued,
		ReceivedAt: receivedAt,
		UpdatedAt:  receivedAt,
	}, nil
}

type fakeActionsStore struct {
	calls int
	err   error
}

func (f *fakeActionsStore) Store(_ context.Context, trigger webhook.ActionsTrigger, _ time.Time) (webhook.StoredActionsTrigger, error) {
	f.calls++
	if f.err != nil {
		return webhook.StoredActionsTrigger{}, f.err
	}
	return webhook.StoredActionsTrigger{
		ActionsTrigger: trigger,
		Status:         webhook.TriggerStatusQueued,
	}, nil
}
