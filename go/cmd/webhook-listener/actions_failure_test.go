// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type failingActionsStore struct {
	triggers []webhook.ActionsTrigger
	times    []time.Time
	err      error
}

func (s *failingActionsStore) Store(_ context.Context, trigger webhook.ActionsTrigger, at time.Time) (webhook.StoredActionsTrigger, error) {
	s.triggers = append(s.triggers, trigger)
	s.times = append(s.times, at)
	if s.err != nil {
		return webhook.StoredActionsTrigger{}, s.err
	}
	return webhook.StoredActionsTrigger{}, errors.New("inbox unavailable")
}

func TestWebhookHandlerActionsStoreFailures(t *testing.T) {
	t.Parallel()

	clockTime := time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC)
	events := []struct {
		name    string
		payload string
		jobID   string
	}{
		{"workflow_run", `{"action":"completed","repository":{"id":42,"full_name":" Example/Repo "},"workflow_run":{"id":123}}`, ""},
		{"workflow_job", `{"action":"completed","repository":{"id":42,"full_name":" Example/Repo "},"workflow_job":{"id":456,"run_id":123}}`, "456"},
	}
	for _, event := range events {
		for _, failure := range []struct {
			name      string
			status    int
			withStore bool
			storeErr  error
			outcome   string
			reason    string
		}{
			{"missing_store", http.StatusServiceUnavailable, false, nil, "failed", "store_failed"},
			{"store_error", http.StatusInternalServerError, true, nil, "failed", "store_failed"},
			{"identity_conflict", http.StatusConflict, true, fmt.Errorf("wrapped: %w", webhook.ErrActionsDeliveryConflict), "rejected", "delivery_conflict"},
		} {
			t.Run(event.name+"/"+failure.name, func(t *testing.T) {
				t.Parallel()
				reader := sdkmetric.NewManualReader()
				meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
				instruments, err := telemetry.NewInstruments(meterProvider.Meter("actions-failure-test"))
				if err != nil {
					t.Fatal(err)
				}
				spanRecorder := tracetest.NewSpanRecorder()
				tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
				var logs bytes.Buffer
				gitStore := &recordingTriggerStore{}
				failedStore := &failingActionsStore{err: failure.storeErr}
				handler := webhookHandler{
					Config: webhookListenerConfig{GitHubSecret: "secret", GitHubPath: "/webhooks/github", MaxRequestBodyBytes: defaultMaxWebhookBodyBytes},
					Store:  gitStore, Clock: func() time.Time { return clockTime },
					Instruments: instruments, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Tracer: tracerProvider.Tracer("actions-failure-test"),
				}
				if failure.withStore {
					handler.ActionsStore = failedStore
				}
				mux, err := newWebhookMux(handler)
				if err != nil {
					t.Fatal(err)
				}
				payload := []byte(event.payload)
				request := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
				request.Header.Set("X-GitHub-Event", event.name)
				request.Header.Set("X-GitHub-Delivery", " delivery-1 ")
				request.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, request)

				if response.Code != failure.status {
					t.Fatalf("status = %d, want %d; body=%q", response.Code, failure.status, response.Body.String())
				}
				if strings.Contains(response.Body.String(), `"decision":"accepted"`) || strings.Contains(response.Body.String(), `"status":"queued"`) {
					t.Fatalf("failure response claims accepted storage: %q", response.Body.String())
				}
				if len(gitStore.triggers) != 0 {
					t.Fatalf("Git refresh triggers = %d, want 0", len(gitStore.triggers))
				}
				if failure.withStore {
					if len(failedStore.triggers) != 1 || len(failedStore.times) != 1 {
						t.Fatalf("Actions store calls = %d, want 1", len(failedStore.triggers))
					}
					want := webhook.ActionsTrigger{DeliveryID: "delivery-1", EventKind: event.name, Action: "completed", RepositoryID: "42", Repository: "example/repo", RunID: "123", JobID: event.jobID}
					if failedStore.triggers[0] != want || !failedStore.times[0].Equal(clockTime) {
						t.Fatalf("store received trigger=%+v time=%v, want trigger=%+v time=%v", failedStore.triggers[0], failedStore.times[0], want, clockTime)
					}
				} else if len(failedStore.triggers) != 0 {
					t.Fatalf("Actions store calls = %d, want 0", len(failedStore.triggers))
				}
				assertCounterPoint(t, collectMetrics(t, reader), "eshu_dp_webhook_requests_total", map[string]string{
					telemetry.MetricDimensionProvider: "github",
					telemetry.MetricDimensionOutcome:  failure.outcome,
					telemetry.MetricDimensionReason:   failure.reason,
				})
				for _, want := range []string{`"provider":"github"`, `"event_kind":"` + event.name + `"`, `"outcome":"` + failure.outcome + `"`, `"reason":"` + failure.reason + `"`} {
					if !strings.Contains(logs.String(), want) {
						t.Fatalf("logs = %q, want %q", logs.String(), want)
					}
				}
				var matchedSpan bool
				for _, span := range spanRecorder.Ended() {
					if span.Name() == telemetry.SpanWebhookHandle && attributeSetContains(attribute.NewSet(span.Attributes()...), map[string]string{
						telemetry.MetricDimensionOutcome: failure.outcome,
						telemetry.MetricDimensionReason:  failure.reason,
					}) {
						matchedSpan = true
					}
				}
				if !matchedSpan {
					t.Fatalf("webhook.handle span lacks outcome=%q reason=%q", failure.outcome, failure.reason)
				}
				for _, sensitive := range []string{"delivery-1", "example/repo"} {
					if strings.Contains(logs.String(), sensitive) {
						t.Fatalf("logs expose delivery identity %q: %q", sensitive, logs.String())
					}
				}
			})
		}
	}
}
