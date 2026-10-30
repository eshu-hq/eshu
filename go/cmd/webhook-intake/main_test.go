// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	awsfreshness "github.com/eshu-hq/eshu/go/internal/coordinator/aws/freshness"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
)

// fakeIntakeRepositoryStore is a zero-SQL stand-in for the Postgres
// repository trigger store behind the served intake API.
type fakeIntakeRepositoryStore struct{}

func (fakeIntakeRepositoryStore) StoreTrigger(_ context.Context, trigger webhook.Trigger, _ time.Time) (webhook.StoredTrigger, error) {
	return webhook.StoredTrigger{
		Trigger:   trigger,
		TriggerID: "trig-1",
		Status:    webhook.TriggerStatusQueued,
	}, nil
}

// fakeIntakeIncidentStore is a zero-SQL stand-in for the Postgres incident
// freshness store behind the served intake API.
type fakeIntakeIncidentStore struct{}

func (fakeIntakeIncidentStore) StoreIncidentFreshnessTrigger(_ context.Context, trigger webhook.IncidentFreshnessTrigger, _ time.Time) (webhook.StoredIncidentFreshnessTrigger, error) {
	return webhook.StoredIncidentFreshnessTrigger{
		IncidentFreshnessTrigger: trigger,
		TriggerID:                "incident-1",
		Status:                   webhook.TriggerStatusQueued,
	}, nil
}

// fakeIntakeAWSStore is a zero-SQL stand-in for the Postgres AWS freshness
// store behind the served intake API.
type fakeIntakeAWSStore struct{}

func (fakeIntakeAWSStore) StoreTrigger(_ context.Context, trigger awsfreshness.Trigger, _ time.Time) (awsfreshness.StoredTrigger, error) {
	return awsfreshness.StoredTrigger{
		Trigger:   trigger,
		TriggerID: "aws-1",
		Status:    awsfreshness.TriggerStatusQueued,
	}, nil
}

// fakeIntakeGCPStore is a zero-SQL stand-in for the Postgres GCP freshness
// store behind the served intake API.
type fakeIntakeGCPStore struct{}

func (fakeIntakeGCPStore) StoreTrigger(_ context.Context, trigger gcpfreshness.Trigger, _ time.Time) (gcpfreshness.StoredTrigger, error) {
	return gcpfreshness.StoredTrigger{
		Trigger:   trigger,
		TriggerID: "gcp-1",
		Status:    catalog.TriggerStatusQueued,
	}, nil
}

// testIntakeStores bundles the zero-SQL fakes behind every intake family so
// the host tests exercise the production server constructor end to end.
func testIntakeStores() intake.Stores {
	return intake.Stores{
		Repository: fakeIntakeRepositoryStore{},
		Incident:   fakeIntakeIncidentStore{},
		AWS:        fakeIntakeAWSStore{},
		GCP:        fakeIntakeGCPStore{},
		Actions:    fakeIntakeActionsStore{},
	}
}

// fakeIntakeActionsStore is a zero-SQL stand-in for the Postgres Actions
// delivery store behind the served intake API.
type fakeIntakeActionsStore struct{}

func (fakeIntakeActionsStore) Store(_ context.Context, trigger webhook.ActionsTrigger, _ time.Time) (webhook.StoredActionsTrigger, error) {
	return webhook.StoredActionsTrigger{
		ActionsTrigger: trigger,
		Status:         webhook.TriggerStatusQueued,
	}, nil
}

func testIntakeServer(t *testing.T, cfg webhookIntakeConfig) http.Handler {
	t.Helper()

	server, err := buildIntakeServer(cfg, testIntakeStores(), nil)
	if err != nil {
		t.Fatalf("buildIntakeServer() error = %v, want nil", err)
	}
	if server == nil {
		t.Fatal("buildIntakeServer() = nil, want serving server")
	}
	return server.Mux()
}

func postIntake(t *testing.T, handler http.Handler, route string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, route, bytes.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// The dedicated host serves core's canonical admission through the
// production server constructor: an authenticated satellite submission is
// accepted with 202.
func TestIntakeHostServesRepositoryIntake(t *testing.T) {
	t.Parallel()

	handler := testIntakeServer(t, webhookIntakeConfig{
		IntakeToken:         "serve-token",
		MaxRequestBodyBytes: defaultMaxWebhookIntakeBodyBytes,
	})
	body, err := json.Marshal(map[string]any{
		"api_version":            "v1",
		"provider":               "github",
		"delivery_id":            "delivery-1",
		"event_kind":             "push",
		"decision":               "accepted",
		"repository_external_id": "42",
		"repository_full_name":   "eshu-hq/eshu",
		"default_branch":         "main",
		"target_sha":             "2222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("marshal intake body: %v", err)
	}
	rec := postIntake(t, handler, "/api/v1/webhook/intake/repository", body, "serve-token")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
}

// No Bearer means no admission: the host answers 401 before storage.
func TestIntakeHostRejectsMissingBearer(t *testing.T) {
	t.Parallel()

	handler := testIntakeServer(t, webhookIntakeConfig{
		IntakeToken:         "serve-token",
		MaxRequestBodyBytes: defaultMaxWebhookIntakeBodyBytes,
	})
	rec := postIntake(t, handler, "/api/v1/webhook/intake/repository", []byte(`{}`), "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

// A wrong wire version fails closed with 400 before storage.
func TestIntakeHostRejectsUnsupportedAPIVersion(t *testing.T) {
	t.Parallel()

	handler := testIntakeServer(t, webhookIntakeConfig{
		IntakeToken:         "serve-token",
		MaxRequestBodyBytes: defaultMaxWebhookIntakeBodyBytes,
	})
	body, err := json.Marshal(map[string]any{
		"api_version":            "v9",
		"provider":               "github",
		"delivery_id":            "delivery-1",
		"event_kind":             "push",
		"decision":               "accepted",
		"repository_external_id": "42",
		"repository_full_name":   "eshu-hq/eshu",
		"default_branch":         "main",
		"target_sha":             "2222222222222222222222222222222222222222",
	})
	if err != nil {
		t.Fatalf("marshal intake body: %v", err)
	}
	rec := postIntake(t, handler, "/api/v1/webhook/intake/repository", body, "serve-token")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// The host constructor fails closed on an empty Bearer: an intake API
// without authentication must never serve.
func TestBuildIntakeServerRequiresToken(t *testing.T) {
	t.Parallel()

	if _, err := buildIntakeServer(webhookIntakeConfig{MaxRequestBodyBytes: defaultMaxWebhookIntakeBodyBytes}, testIntakeStores(), nil); err == nil {
		t.Fatal("buildIntakeServer() error = nil, want fail-closed token error")
	}
}
