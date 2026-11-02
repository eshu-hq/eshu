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

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestServedIntakeEmitsFreshnessAlertSeries is the #9822 red/green pair for
// the listener's served mount, mirroring TestIntakeHostEmitsFreshnessAlertSeries
// in go/cmd/webhook-intake. It builds the served intake server through the
// production constructor over the production telemetry providers and scrapes
// the Prometheus handler. The EshuTriggerHandoffFailed alert selects
// eshu_dp_webhook_trigger_decisions_total{status="failed"}: a forced store
// failure on the served mount must expose that series, and a healthy store
// must not.
func TestServedIntakeEmitsFreshnessAlertSeries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failErr    error
		wantStatus int
		wantFailed bool
	}{
		{"store_failure", errors.New("postgres unavailable"), http.StatusInternalServerError, true},
		{"store_healthy", nil, http.StatusAccepted, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if previous, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); ok {
				t.Cleanup(func() { _ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", previous) })
			}
			_ = os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
			bootstrap, err := telemetry.NewBootstrap("webhook-listener")
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
			stores, repo, _, _, _, _ := serveTestStores()
			repo.failErr = tc.failErr
			server, err := buildServedIntake(serveTestConfig(), stores, nil, instruments)
			if err != nil {
				t.Fatalf("buildServedIntake() error = %v, want nil", err)
			}
			mux := http.NewServeMux()
			mountServedIntake(mux, server)

			rec := postServedIntake(mux, "/api/v1/webhook/intake/repository", validRepositoryIntakeBody(), "serve-token")
			if rec.Code != tc.wantStatus {
				t.Fatalf("served intake status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
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
			if strings.Contains(body, "eshu-hq/eshu") || strings.Contains(body, "serve-delivery-1") {
				t.Fatal("scrape leaked a repository name or delivery id into a label")
			}
		})
	}
}
