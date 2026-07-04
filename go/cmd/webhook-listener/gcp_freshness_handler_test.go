// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
)

func gcpPushEnvelope(t *testing.T, temporalAssetJSON string) []byte {
	t.Helper()
	envelope := struct {
		Message struct {
			Data      string `json:"data"`
			MessageID string `json:"messageId"`
		} `json:"message"`
		Subscription string `json:"subscription"`
	}{}
	envelope.Message.Data = base64.StdEncoding.EncodeToString([]byte(temporalAssetJSON))
	envelope.Message.MessageID = "123456789012345"
	envelope.Subscription = "projects/demo/subscriptions/gcp-freshness"
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal push envelope: %v", err)
	}
	return encoded
}

const gcpFreshnessValidTemporalAsset = `{
	"asset": {
		"name": "//compute.googleapis.com/projects/123456789012/zones/us-central1-a/instances/vm-1",
		"assetType": "compute.googleapis.com/Instance",
		"ancestors": ["projects/123456789012"],
		"updateTime": "2026-05-15T10:10:59Z",
		"resource": {"location": "us-central1-a"}
	},
	"deleted": false
}`

func TestWebhookHandlerAcceptsGCPFreshnessPushPayload(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := len(store.triggers), 1; got != want {
		t.Fatalf("stored triggers = %d, want %d", got, want)
	}
	trigger := store.triggers[0]
	if trigger.Kind != freshness.EventKindAssetChange {
		t.Fatalf("Kind = %q, want %q", trigger.Kind, freshness.EventKindAssetChange)
	}
	if trigger.ParentScopeKind != gcpcloud.ParentScopeProject {
		t.Fatalf("ParentScopeKind = %q, want %q", trigger.ParentScopeKind, gcpcloud.ParentScopeProject)
	}
}

func TestWebhookHandlerRejectsGCPFreshnessMissingToken(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsGCPFreshnessBadToken(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "wrong")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsGCPFreshnessBadTokenBeforeBodyRead(t *testing.T) {
	t.Parallel()

	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	body := &readCountingCloser{}
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", body)
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "wrong")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if body.reads != 0 {
		t.Fatalf("body reads = %d, want 0", body.reads)
	}
}

func TestWebhookHandlerRejectsGCPFreshnessWhenTokenNotConfigured(t *testing.T) {
	t.Parallel()

	// Fail-closed: an empty configured token must never validate any
	// presented token, matching the AWS freshness path's zero-value guard.
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "")
	if validGCPFreshnessToken(req, "") {
		t.Fatal("validGCPFreshnessToken() = true with empty configured token, want false (fail-closed)")
	}
}

func TestWebhookHandlerRejectsGCPFreshnessMalformedEnvelope(t *testing.T) {
	t.Parallel()

	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader([]byte(`not json`)))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerAcceptsGCPFreshnessWelcomeMessageAsIgnored(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, `"You have successfully subscribed."`)
	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	// A welcome message is a benign no-op: acknowledged (2xx) so Pub/Sub does
	// not retry it, but never stored as a trigger.
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerGCPFreshnessRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodGet, "/webhook/gcp-freshness", nil)
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhookHandlerGCPFreshnessCoalescesRedelivery(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
		req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("delivery %d: status = %d, want %d", i, rec.Code, http.StatusAccepted)
		}
	}
	// The fake store records every StoreTrigger call; the redelivery-coalescing
	// contract itself (ON CONFLICT freshness_key) is proven in the Postgres
	// store tests. Here we only prove the handler calls StoreTrigger for every
	// accepted delivery, including a redelivery of the same trigger.
	if got, want := len(store.triggers), 2; got != want {
		t.Fatalf("stored triggers = %d, want %d", got, want)
	}
}

func TestWebhookHandlerAcceptsGCPFreshnessBearerToken(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if len(store.triggers) != 1 {
		t.Fatalf("stored triggers = %d, want 1", len(store.triggers))
	}
}

func TestWebhookHandlerGCPFreshnessCoalescedRedeliveryReturnsCoalescedAction(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{duplicateCount: 1}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body["status"] != string(freshness.TriggerStatusQueued) {
		t.Fatalf("status = %v, want %q", body["status"], freshness.TriggerStatusQueued)
	}
}

func TestWebhookHandlerGCPFreshnessStoreFailureReturns500(t *testing.T) {
	t.Parallel()

	payload := gcpPushEnvelope(t, gcpFreshnessValidTemporalAsset)
	store := &recordingGCPFreshnessStore{failWith: errStoreBoom}
	mux := mustWebhookMuxWithGCPFreshness(t, webhookListenerConfig{
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhook/gcp-freshness", bytes.NewReader(payload))
	req.Header.Set("X-Eshu-GCP-Freshness-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestLoadWebhookListenerConfigAllowsGCPFreshnessOnly(t *testing.T) {
	t.Parallel()

	cfg, err := loadWebhookListenerConfig(func(key string) string {
		values := map[string]string{
			"ESHU_GCP_FRESHNESS_TOKEN": "secret",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("loadWebhookListenerConfig() error = %v, want nil", err)
	}
	if cfg.GCPFreshnessPath != "/webhook/gcp-freshness" {
		t.Fatalf("GCPFreshnessPath = %q, want /webhook/gcp-freshness", cfg.GCPFreshnessPath)
	}
}

func mustWebhookMuxWithGCPFreshness(
	t *testing.T,
	cfg webhookListenerConfig,
	store *recordingGCPFreshnessStore,
) *http.ServeMux {
	t.Helper()
	mux, err := newWebhookMux(webhookHandler{
		Config:            cfg,
		GCPFreshnessStore: store,
		Clock: func() time.Time {
			return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v", err)
	}
	return mux
}

var errStoreBoom = errors.New("boom")

type recordingGCPFreshnessStore struct {
	triggers       []freshness.Trigger
	duplicateCount int
	failWith       error
}

func (s *recordingGCPFreshnessStore) StoreTrigger(
	_ context.Context,
	trigger freshness.Trigger,
	receivedAt time.Time,
) (freshness.StoredTrigger, error) {
	if s.failWith != nil {
		return freshness.StoredTrigger{}, s.failWith
	}
	s.triggers = append(s.triggers, trigger)
	stored, err := freshness.NewStoredTrigger(trigger, receivedAt)
	if err != nil {
		return freshness.StoredTrigger{}, err
	}
	stored.DuplicateCount = s.duplicateCount
	return stored, nil
}

var _ io.ReadCloser = &readCountingCloser{}
