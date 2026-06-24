// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

func TestWebhookHandlerAcceptsSignedGitHubPush(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ref":"refs/heads/main",
		"before":"1111111111111111111111111111111111111111",
		"after":"2222222222222222222222222222222222222222",
		"repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}
	}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := len(store.triggers), 1; got != want {
		t.Fatalf("stored triggers = %d, want %d", got, want)
	}
	if store.triggers[0].Decision != webhook.DecisionAccepted {
		t.Fatalf("Decision = %q, want accepted", store.triggers[0].Decision)
	}
}

func TestWebhookHandlerAcceptsSignedGitHubPingWithoutStoringTrigger(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"zen":"Keep it logically awesome.",
		"hook_id":123,
		"repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}
	}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "ping")
	req.Header.Set("X-GitHub-Delivery", "delivery-ping")
	req.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := len(store.triggers), 0; got != want {
		t.Fatalf("stored triggers = %d, want %d", got, want)
	}
	if got := rec.Body.String(); !bytes.Contains([]byte(got), []byte(`"reason":"ping"`)) {
		t.Fatalf("body = %q, want ping reason", got)
	}
}

func TestWebhookHandlerAcceptsSignedPagerDutyFreshnessTrigger(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"event": {
			"id": "evt-123",
			"event_type": "incident.triggered",
			"occurred_at": "2026-05-31T18:00:00Z",
			"data": {"id": "PINC123", "type": "incident"}
		}
	}`)
	store := &recordingIncidentFreshnessStore{}
	mux := mustIncidentWebhookMux(t, webhookListenerConfig{
		PagerDutySecret:     "secret",
		PagerDutyPath:       "/webhooks/pagerduty",
		PagerDutyScopeID:    "pagerduty:account:example",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/pagerduty", bytes.NewReader(payload))
	req.Header.Set("X-PagerDuty-Signature", pagerDutyWebhookSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := len(store.triggers), 1; got != want {
		t.Fatalf("stored incident triggers = %d, want %d", got, want)
	}
	if got, want := store.triggers[0].ScopeID, "pagerduty:account:example"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
}

func TestWebhookHandlerAcceptsSignedJiraFreshnessTrigger(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"webhookEvent": "jira:issue_updated",
		"timestamp": 1780250400000,
		"issue": {
			"id": "10001",
			"key": "OPS-123",
			"self": "https://example.atlassian.net/rest/api/3/issue/10001"
		}
	}`)
	store := &recordingIncidentFreshnessStore{}
	mux := mustIncidentWebhookMux(t, webhookListenerConfig{
		JiraSecret:          "secret",
		JiraPath:            "/webhooks/jira",
		JiraScopeID:         "jira:site:example",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/jira", bytes.NewReader(payload))
	req.Header.Set("X-Atlassian-Webhook-Identifier", "delivery-jira-1")
	req.Header.Set("X-Hub-Signature", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := len(store.triggers), 1; got != want {
		t.Fatalf("stored incident triggers = %d, want %d", got, want)
	}
	if got, want := store.triggers[0].ResourceID, "OPS-123"; got != want {
		t.Fatalf("ResourceID = %q, want %q", got, want)
	}
}

func TestWebhookHandlerRejectsMissingPagerDutySignature(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"event":{"id":"evt-123","event_type":"incident.triggered"}}`)
	store := &recordingIncidentFreshnessStore{}
	mux := mustIncidentWebhookMux(t, webhookListenerConfig{
		PagerDutySecret:     "secret",
		PagerDutyPath:       "/webhooks/pagerduty",
		PagerDutyScopeID:    "pagerduty:account:example",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/pagerduty", bytes.NewReader(payload))
	req.Header.Set("X-Webhook-Id", "delivery-pd-1")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored incident triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsMissingPagerDutyDeliveryIdentity(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"event":{"event_type":"incident.triggered"}}`)
	store := &recordingIncidentFreshnessStore{}
	mux := mustIncidentWebhookMux(t, webhookListenerConfig{
		PagerDutySecret:     "secret",
		PagerDutyPath:       "/webhooks/pagerduty",
		PagerDutyScopeID:    "pagerduty:account:example",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/pagerduty", bytes.NewReader(payload))
	req.Header.Set("X-PagerDuty-Signature", pagerDutyWebhookSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored incident triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsMissingJiraDeliveryID(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"webhookEvent": "jira:issue_updated",
		"issue": {"id": "10001", "key": "OPS-123"}
	}`)
	store := &recordingIncidentFreshnessStore{}
	mux := mustIncidentWebhookMux(t, webhookListenerConfig{
		JiraSecret:          "secret",
		JiraPath:            "/webhooks/jira",
		JiraScopeID:         "jira:site:example",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/jira", bytes.NewReader(payload))
	req.Header.Set("X-Hub-Signature", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored incident triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsBadGitHubSignature(t *testing.T) {
	t.Parallel()

	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsMissingGitHubDeliveryID(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ref":"refs/heads/main",
		"after":"2222222222222222222222222222222222222222",
		"repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}
	}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerAcceptsGitLabToken(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"object_kind":"push",
		"ref":"refs/heads/main",
		"after":"2222222222222222222222222222222222222222",
		"project":{"id":77,"path_with_namespace":"eshu-hq/eshu","default_branch":"main"}
	}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitLabToken:         "secret",
		GitLabPath:          "/webhooks/gitlab",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", bytes.NewReader(payload))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "secret")
	req.Header.Set("Idempotency-Key", "retry-stable-delivery")
	req.Header.Set("X-Gitlab-Event-UUID", "delivery-2")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if store.triggers[0].Provider != webhook.ProviderGitLab {
		t.Fatalf("Provider = %q, want gitlab", store.triggers[0].Provider)
	}
	if store.triggers[0].DeliveryID != "retry-stable-delivery" {
		t.Fatalf("DeliveryID = %q, want Idempotency-Key value", store.triggers[0].DeliveryID)
	}
}

func TestWebhookHandlerAcceptsSignedBitbucketPush(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"repository":{"uuid":"{repo-uuid}","full_name":"eshu-hq/eshu","mainbranch":{"name":"main"}},
		"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"2222222222222222222222222222222222222222"}}}]}
	}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		BitbucketSecret:     "secret",
		BitbucketPath:       "/webhooks/bitbucket",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bitbucket", bytes.NewReader(payload))
	req.Header.Set("X-Event-Key", "repo:push")
	req.Header.Set("X-Request-UUID", "request-1")
	req.Header.Set("X-Hub-Signature", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := len(store.triggers), 1; got != want {
		t.Fatalf("stored triggers = %d, want %d", got, want)
	}
	if store.triggers[0].Provider != webhook.ProviderBitbucket {
		t.Fatalf("Provider = %q, want bitbucket", store.triggers[0].Provider)
	}
}

func TestWebhookHandlerRejectsMissingBitbucketDeliveryID(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"repository":{"uuid":"{repo-uuid}","full_name":"eshu-hq/eshu","mainbranch":{"name":"main"}},
		"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"2222222222222222222222222222222222222222"}}}]}
	}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		BitbucketSecret:     "secret",
		BitbucketPath:       "/webhooks/bitbucket",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bitbucket", bytes.NewReader(payload))
	req.Header.Set("X-Event-Key", "repo:push")
	req.Header.Set("X-Hub-Signature", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsMissingGitLabDeliveryID(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"object_kind":"push",
		"ref":"refs/heads/main",
		"after":"2222222222222222222222222222222222222222",
		"project":{"id":77,"path_with_namespace":"eshu-hq/eshu","default_branch":"main"}
	}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitLabToken:         "secret",
		GitLabPath:          "/webhooks/gitlab",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", bytes.NewReader(payload))
	req.Header.Set("X-Gitlab-Event", "Push Hook")
	req.Header.Set("X-Gitlab-Token", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerDistinguishesBodyReadErrors(t *testing.T) {
	t.Parallel()

	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", errReader{})
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", "sha256=unused")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerReportsOversizedBody(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"too":"large"}`)
	store := &recordingTriggerStore{}
	mux := mustWebhookMux(t, webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		MaxRequestBodyBytes: 4,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestLoadWebhookListenerConfigRequiresProviderSecret(t *testing.T) {
	t.Parallel()

	_, err := loadWebhookListenerConfig(func(string) string { return "" })
	if err == nil {
		t.Fatal("loadWebhookListenerConfig() error = nil, want provider secret error")
	}
}

func mustWebhookMux(t *testing.T, cfg webhookListenerConfig, store triggerStore) *http.ServeMux {
	t.Helper()
	mux, err := newWebhookMux(webhookHandler{
		Config: cfg,
		Store:  store,
		Clock:  func() time.Time { return time.Date(2026, time.May, 12, 16, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v, want nil", err)
	}
	return mux
}

func mustIncidentWebhookMux(t *testing.T, cfg webhookListenerConfig, store incidentFreshnessStore) *http.ServeMux {
	t.Helper()
	mux, err := newWebhookMux(webhookHandler{
		Config:                 cfg,
		IncidentFreshnessStore: store,
		Clock:                  func() time.Time { return time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v, want nil", err)
	}
	return mux
}

func githubSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func pagerDutyWebhookSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

type recordingTriggerStore struct {
	triggers []webhook.Trigger
}

type recordingIncidentFreshnessStore struct {
	triggers []webhook.IncidentFreshnessTrigger
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("socket closed")
}

func (errReader) Close() error {
	return nil
}

var _ io.ReadCloser = errReader{}

func (s *recordingTriggerStore) StoreTrigger(
	_ context.Context,
	trigger webhook.Trigger,
	receivedAt time.Time,
) (webhook.StoredTrigger, error) {
	s.triggers = append(s.triggers, trigger)
	return webhook.StoredTrigger{
		Trigger:    trigger,
		TriggerID:  "trigger-1",
		Status:     webhook.TriggerStatusQueued,
		ReceivedAt: receivedAt,
		UpdatedAt:  receivedAt,
	}, nil
}

func (s *recordingIncidentFreshnessStore) StoreIncidentFreshnessTrigger(
	_ context.Context,
	trigger webhook.IncidentFreshnessTrigger,
	receivedAt time.Time,
) (webhook.StoredIncidentFreshnessTrigger, error) {
	s.triggers = append(s.triggers, trigger)
	return webhook.StoredIncidentFreshnessTrigger{
		IncidentFreshnessTrigger: trigger,
		TriggerID:                "incident-trigger-1",
		Status:                   webhook.TriggerStatusQueued,
		ReceivedAt:               receivedAt,
		UpdatedAt:                receivedAt,
	}, nil
}
