// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	awsfreshness "github.com/eshu-hq/eshu/go/internal/coordinator/aws/freshness"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// coalescingAWSStore reports every stored delivery as joined to an existing row.
type coalescingAWSStore struct{}

func (coalescingAWSStore) StoreTrigger(_ context.Context, trigger awsfreshness.Trigger, _ time.Time) (awsfreshness.StoredTrigger, error) {
	return awsfreshness.StoredTrigger{
		Trigger: trigger, TriggerID: "aws-1", Status: awsfreshness.TriggerStatusQueued, DuplicateCount: 3,
	}, nil
}

// coalescingGCPStore reports every stored delivery as joined to an existing row.
type coalescingGCPStore struct{}

func (coalescingGCPStore) StoreTrigger(_ context.Context, trigger gcpfreshness.Trigger, _ time.Time) (gcpfreshness.StoredTrigger, error) {
	return gcpfreshness.StoredTrigger{
		Trigger: trigger, TriggerID: "gcp-1", Status: catalog.TriggerStatusQueued, DuplicateCount: 1,
	}, nil
}

func mustJSON(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

// TestIntakeHostEmitsFreshnessActionSeries is the #9814 host-level red/green
// pair. It builds the server through the production constructor over the
// production telemetry providers and scrapes the Prometheus handler the host
// mounts at /metrics. A coalesced AWS and GCP delivery must expose the
// freshness action series the runbook queries select, and the GCP series must
// carry auth_path="n/a" so it shares one label set with the coordinator.
func TestIntakeHostEmitsFreshnessActionSeries(t *testing.T) {
	if previous, ok := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT"); ok {
		t.Cleanup(func() { _ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", previous) })
	}
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
	stores.AWS = coalescingAWSStore{}
	stores.GCP = coalescingGCPStore{}
	server, err := buildIntakeServer(webhookIntakeConfig{
		IntakeToken:         "serve-token",
		MaxRequestBodyBytes: defaultMaxWebhookIntakeBodyBytes,
	}, stores, nil, instruments)
	if err != nil {
		t.Fatalf("buildIntakeServer() error = %v", err)
	}

	for route, payload := range map[string]map[string]any{
		"/api/v1/webhook/intake/aws": {
			"api_version": "v1", "event_id": "evt-1", "kind": "config_change",
			"account_id": "123456789012", "region": "us-east-1", "service_kind": "lambda",
			"resource_type": "aws_lambda_function", "resource_id": "fn-1",
			"observed_at": "2026-05-15T10:00:00Z",
		},
		"/api/v1/webhook/intake/gcp": {
			"api_version": "v1", "event_id": "evt-1", "kind": string(catalog.EventKindAssetChange),
			"parent_scope_kind": string(catalog.ParentScopeProject), "parent_scope_id": "demo-project",
			"asset_type": "compute.googleapis.com/Instance", "location": "us-central1-a",
			"observed_at": "2026-05-15T10:00:00Z",
		},
	} {
		if rec := postIntake(t, server.Mux(), route, mustJSON(t, payload), "serve-token"); rec.Code != http.StatusAccepted {
			t.Fatalf("%s = %d (%s), want 202", route, rec.Code, rec.Body.String())
		}
	}

	scrape := httptest.NewRecorder()
	providers.PrometheusHandler.ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()
	for name, pattern := range map[string]string{
		"aws coalesced": `(?m)^eshu_dp_aws_freshness_events_total\{[^}]*action="intake_coalesced"[^}]*kind="config_change"[^}]*\} 1`,
		"gcp coalesced": `(?m)^eshu_dp_gcp_freshness_events_total\{[^}]*action="intake_coalesced"[^}]*auth_path="n/a"[^}]*kind="asset_change"[^}]*\} 1`,
	} {
		if !regexp.MustCompile(pattern).MatchString(body) {
			t.Fatalf("scrape is missing the %s series\n%s", name, body)
		}
	}
	if regexp.MustCompile(`(?m)^eshu_dp_(aws|gcp)_freshness_events_total\{[^}]*action="intake_(rejected|ignored)"`).MatchString(body) {
		t.Fatal("scrape exposes intake_rejected or intake_ignored; the host must not emit them")
	}
}
