// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestIntakeHostEmitsRequestAndStoreSpans is the #9815 host-level red/green
// pair. It builds the server through the production constructor with a
// recording tracer, as main wires it, and checks that a stored repository
// delivery produces webhook.handle with a webhook.store child. A constructor
// that drops the tracer produces no spans and fails here.
func TestIntakeHostEmitsRequestAndStoreSpans(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	server, err := buildIntakeServer(webhookIntakeConfig{
		IntakeToken:         "serve-token",
		MaxRequestBodyBytes: defaultMaxWebhookIntakeBodyBytes,
	}, testIntakeStores(), nil, nil, provider.Tracer("intake-host-test"))
	if err != nil {
		t.Fatalf("buildIntakeServer() error = %v", err)
	}

	rec := postIntake(t, server.Mux(), "/api/v1/webhook/intake/repository", []byte(repositoryIntakeBody), "serve-token")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("intake status = %d, want 202", rec.Code)
	}

	var handle, store sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		switch span.Name() {
		case "webhook.handle":
			handle = span
		case "webhook.store":
			store = span
		}
	}
	if handle == nil || store == nil {
		t.Fatalf("spans: handle=%v store=%v, want both", handle != nil, store != nil)
	}
	if store.Parent().SpanID() != handle.SpanContext().SpanID() {
		t.Fatalf("store span parent = %s, want the handle span %s", store.Parent().SpanID(), handle.SpanContext().SpanID())
	}
}
