// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
)

// failingIntakeRepositoryStore forces the durable write to fail.
type failingIntakeRepositoryStore struct{}

func (failingIntakeRepositoryStore) StoreTrigger(context.Context, webhook.Trigger, time.Time) (webhook.StoredTrigger, error) {
	return webhook.StoredTrigger{}, errors.New("postgres unavailable")
}

const repositoryIntakeBody = `{
	"api_version": "v1", "provider": "github", "delivery_id": "delivery-1",
	"event_kind": "push", "decision": "accepted", "repository_external_id": "123",
	"repository_full_name": "octo/repo", "default_branch": "main", "target_sha": "abc123"
}`

// TestIntakeHostEmitsFreshnessAlertSeries is the #9811 host-level red/green
// pair. It builds the server through the production constructor, over the
// production telemetry providers, and scrapes the Prometheus handler the host
// mounts at /metrics. The EshuTriggerHandoffFailed alert selects
// eshu_dp_webhook_trigger_decisions_total{status="failed"}: a forced store
// failure must expose that series, and a healthy store must not.
func TestIntakeHostEmitsFreshnessAlertSeries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		store      func() intake.RepositoryStore
		wantStatus int
		wantFailed bool
	}{
		{"store_failure", func() intake.RepositoryStore { return failingIntakeRepositoryStore{} }, http.StatusInternalServerError, true},
		{"store_healthy", func() intake.RepositoryStore { return fakeIntakeRepositoryStore{} }, http.StatusAccepted, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
			bootstrap, err := telemetry.NewBootstrap("webhook-intake")
			if err != nil {
				t.Fatalf("NewBootstrap() error = %v", err)
			}
			providers, err := telemetry.NewProviders(context.Background(), bootstrap)
			if err != nil {
				t.Fatalf("NewProviders() error = %v", err)
			}
			t.Cleanup(func() { _ = providers.Shutdown(context.Background()) })
			instruments, err := telemetry.NewInstruments(providers.MeterProvider.Meter(telemetry.DefaultSignalName))
			if err != nil {
				t.Fatalf("NewInstruments() error = %v", err)
			}
			stores := testIntakeStores()
			stores.Repository = tc.store()
			server, err := buildIntakeServer(webhookIntakeConfig{
				IntakeToken:         "serve-token",
				MaxRequestBodyBytes: defaultMaxWebhookIntakeBodyBytes,
			}, stores, nil, instruments)
			if err != nil {
				t.Fatalf("buildIntakeServer() error = %v", err)
			}

			rec := postIntake(t, server.Mux(), "/api/v1/webhook/intake/repository", []byte(repositoryIntakeBody), "serve-token")
			if rec.Code != tc.wantStatus {
				t.Fatalf("intake status = %d, want %d", rec.Code, tc.wantStatus)
			}

			scrape := httptest.NewRecorder()
			providers.PrometheusHandler.ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			body := scrape.Body.String()
			failed := regexp.MustCompile(`(?m)^eshu_dp_webhook_trigger_decisions_total\{[^}]*status="failed"[^}]*\} [1-9]`)
			if got := failed.MatchString(body); got != tc.wantFailed {
				t.Fatalf("alert series status=\"failed\" exposed = %v, want %v\n%s", got, tc.wantFailed, body)
			}
			for _, name := range []string{
				"eshu_dp_webhook_requests_total{",
				"eshu_dp_webhook_request_duration_seconds_count{",
				"eshu_dp_webhook_store_duration_seconds_count{",
			} {
				if !strings.Contains(body, name) {
					t.Fatalf("scrape is missing %s", name)
				}
			}
			if strings.Contains(body, "octo/repo") || strings.Contains(body, "delivery-1") {
				t.Fatal("scrape leaked a repository name or delivery id into a label")
			}
		})
	}
}
