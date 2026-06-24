// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

func TestWebhookHandlerDoesNotRegisterUnsignedJiraRouteWhenSecretDisabled(t *testing.T) {
	t.Parallel()

	mux := mustWebhookMux(t, webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		JiraPath:            "/webhooks/jira",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, &recordingTriggerStore{})
	req := httptest.NewRequest(http.MethodPost, "/webhooks/jira", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestWebhookHandlerRejectsBadJiraSignature(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"webhookEvent": "jira:issue_updated",
		"timestamp": 1780250400000,
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
	req.Header.Set("X-Atlassian-Webhook-Identifier", "delivery-jira-1")
	req.Header.Set("X-Hub-Signature", "sha256=bad")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored incident triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsUnsupportedJiraEvent(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"webhookEvent": "project_deleted",
		"timestamp": 1780250400000,
		"project": {"id": "10000", "key": "OPS"}
	}`)
	store := &recordingIncidentFreshnessStore{}
	mux := mustIncidentWebhookMux(t, webhookListenerConfig{
		JiraSecret:          "secret",
		JiraPath:            "/webhooks/jira",
		JiraScopeID:         "jira:site:example",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/jira", bytes.NewReader(payload))
	req.Header.Set("X-Atlassian-Webhook-Identifier", "delivery-jira-unsupported")
	req.Header.Set("X-Hub-Signature", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("unsupported")) {
		t.Fatalf("body = %q, want unsupported event diagnostic", rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored incident triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerPreservesJiraDeliveryForStoredKeys(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"webhookEvent": "jira:issue_updated",
		"timestamp": 1780250400000,
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
	stored, err := webhook.NewStoredIncidentFreshnessTrigger(
		store.triggers[0],
		store.triggers[0].ObservedAt,
	)
	if err != nil {
		t.Fatalf("NewStoredIncidentFreshnessTrigger() error = %v, want nil", err)
	}
	if got, want := stored.DeliveryKey, "jira_cloud:delivery-jira-1"; got != want {
		t.Fatalf("DeliveryKey = %q, want %q", got, want)
	}
	if got, want := stored.FreshnessKey, "jira_cloud:jira:site:example"; got != want {
		t.Fatalf("FreshnessKey = %q, want %q", got, want)
	}
}
