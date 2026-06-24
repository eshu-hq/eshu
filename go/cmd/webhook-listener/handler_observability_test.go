// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestWebhookHandlerRecordsBoundedTelemetry(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"ref":"refs/heads/main",
		"before":"1111111111111111111111111111111111111111",
		"after":"2222222222222222222222222222222222222222",
		"repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}
	}`)
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("webhook-listener-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	store := &recordingTriggerStore{}
	mux := mustWebhookMuxWithObservability(
		t,
		webhookListenerConfig{
			GitHubSecret:        "secret",
			GitHubPath:          "/webhooks/github",
			MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
		},
		store,
		instruments,
		tracerProvider,
	)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-1")
	req.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	rm := collectMetrics(t, reader)
	assertCounterPoint(t, rm, "eshu_dp_webhook_requests_total", map[string]string{
		telemetry.MetricDimensionProvider: "github",
		telemetry.MetricDimensionOutcome:  "stored",
		telemetry.MetricDimensionReason:   "none",
	})
	assertCounterPoint(t, rm, "eshu_dp_webhook_trigger_decisions_total", map[string]string{
		telemetry.MetricDimensionProvider:  "github",
		telemetry.MetricDimensionEventKind: "push",
		telemetry.MetricDimensionDecision:  "accepted",
		telemetry.MetricDimensionReason:    "none",
		telemetry.MetricDimensionStatus:    "queued",
	})
	assertHistogramPoint(t, rm, "eshu_dp_webhook_request_duration_seconds", map[string]string{
		telemetry.MetricDimensionProvider: "github",
		telemetry.MetricDimensionOutcome:  "stored",
		telemetry.MetricDimensionReason:   "none",
	})
	assertHistogramPoint(t, rm, "eshu_dp_webhook_store_duration_seconds", map[string]string{
		telemetry.MetricDimensionProvider: "github",
		telemetry.MetricDimensionOutcome:  "stored",
		telemetry.MetricDimensionStatus:   "queued",
	})
	if !spanRecorded(spanRecorder, telemetry.SpanWebhookHandle) {
		t.Fatalf("span %q was not recorded", telemetry.SpanWebhookHandle)
	}
	if !spanRecorded(spanRecorder, telemetry.SpanWebhookStore) {
		t.Fatalf("span %q was not recorded", telemetry.SpanWebhookStore)
	}
}

func TestWebhookHandlerLogsBoundedRejectedOutcome(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	payload := []byte(`{
		"repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}
	}`)
	store := &recordingTriggerStore{}
	mux, err := newWebhookMux(webhookHandler{
		Config: webhookListenerConfig{
			GitHubSecret:        "secret",
			GitHubPath:          "/webhooks/github",
			MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
		},
		Store:  store,
		Clock:  func() time.Time { return time.Date(2026, time.May, 12, 16, 0, 0, 0, time.UTC) },
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "delivery-secret-value")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	got := logs.String()
	for _, want := range []string{
		`"msg":"webhook request handled"`,
		`"provider":"github"`,
		`"outcome":"rejected"`,
		`"reason":"auth_failed"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("logs = %q, want %s", got, want)
		}
	}
	for _, sensitive := range []string{"delivery-secret-value", "eshu-hq/eshu"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("logs = %q, must not contain %q", got, sensitive)
		}
	}
}

func TestWebhookHandlerRecordsJiraUnsupportedEventTelemetry(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"webhookEvent": "project_deleted",
		"timestamp": 1780250400000,
		"project": {"id": "10000", "key": "OPS"}
	}`)
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("webhook-listener-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	store := &recordingIncidentFreshnessStore{}
	mux, err := newWebhookMux(webhookHandler{
		Config: webhookListenerConfig{
			JiraSecret:          "secret",
			JiraPath:            "/webhooks/jira",
			JiraScopeID:         "jira:site:example",
			MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
		},
		IncidentFreshnessStore: store,
		Clock:                  func() time.Time { return time.Date(2026, time.May, 31, 18, 0, 0, 0, time.UTC) },
		Instruments:            instruments,
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/jira", bytes.NewReader(payload))
	req.Header.Set("X-Atlassian-Webhook-Identifier", "delivery-jira-unsupported")
	req.Header.Set("X-Hub-Signature", githubSignature("secret", payload))
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored incident triggers = %d, want 0", len(store.triggers))
	}
	rm := collectMetrics(t, reader)
	assertCounterPoint(t, rm, "eshu_dp_webhook_requests_total", map[string]string{
		telemetry.MetricDimensionProvider: "jira_cloud",
		telemetry.MetricDimensionOutcome:  "rejected",
		telemetry.MetricDimensionReason:   "unsupported_event",
	})
}

func mustWebhookMuxWithObservability(
	t *testing.T,
	cfg webhookListenerConfig,
	store triggerStore,
	instruments *telemetry.Instruments,
	tracerProvider *sdktrace.TracerProvider,
) *http.ServeMux {
	t.Helper()
	mux, err := newWebhookMux(webhookHandler{
		Config:      cfg,
		Store:       store,
		Clock:       func() time.Time { return time.Date(2026, time.May, 12, 16, 0, 0, 0, time.UTC) },
		Instruments: instruments,
		Tracer:      tracerProvider.Tracer("webhook-listener-test"),
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v, want nil", err)
	}
	return mux
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	return rm
}

func assertCounterPoint(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s has type %T, want Sum[int64]", name, m.Data)
			}
			for _, point := range sum.DataPoints {
				if attributeSetContains(point.Attributes, attrs) && point.Value > 0 {
					return
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %v was not recorded", name, attrs)
}

func assertHistogramPoint(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs map[string]string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			histogram, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s has type %T, want Histogram[float64]", name, m.Data)
			}
			for _, point := range histogram.DataPoints {
				if attributeSetContains(point.Attributes, attrs) && point.Count > 0 {
					return
				}
			}
		}
	}
	t.Fatalf("histogram %s with attrs %v was not recorded", name, attrs)
}

func attributeSetContains(attrs attribute.Set, want map[string]string) bool {
	for key, wantValue := range want {
		var matched bool
		for _, kv := range attrs.ToSlice() {
			if string(kv.Key) == key && kv.Value.AsString() == wantValue {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func spanRecorded(recorder *tracetest.SpanRecorder, name string) bool {
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			return true
		}
	}
	return false
}
