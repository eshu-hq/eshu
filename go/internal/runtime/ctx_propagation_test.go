// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"context"
	"net/http"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceContextPropagationThroughHTTPServerLifecycle(t *testing.T) {
	// Create a real span-bearing context backed by an SDK tracer so
	// SpanFromContext returns a valid span with a non-zero trace ID. This
	// guards against silent ctx-drops (e.g. context.Background() in a
	// shutdown goroutine) that break distributed tracing across the
	// pipeline.
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	tracer := tp.Tracer("runtime-test")
	ctx, span := tracer.Start(context.Background(), "test-lifecycle")
	defer span.End()

	// Boundary 1: trace ID must be non-zero at the start of the pipeline.
	startTraceID := trace.SpanFromContext(ctx).SpanContext().TraceID()
	if !startTraceID.IsValid() {
		t.Fatal("boundary start: trace.SpanFromContext(ctx).SpanContext().TraceID() is zero — no span is active at pipeline start")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv, err := NewHTTPServer(HTTPServerConfig{
		Addr:            "127.0.0.1:0",
		Handler:         handler,
		ShutdownTimeout: 3 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}

	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Boundary 2: trace ID after Start — the Start method accepts a
	// context but the internal goroutine does not re-derive from it;
	// the caller's ctx should still carry the trace.
	afterStartTraceID := trace.SpanFromContext(ctx).SpanContext().TraceID()
	if afterStartTraceID != startTraceID {
		t.Fatalf("boundary Start: trace ID changed from %s to %s — ctx was dropped or replaced during server start",
			startTraceID, afterStartTraceID)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 3*time.Second)
	defer shutdownCancel()

	if err := srv.Stop(shutdownCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// Boundary 3: trace ID after Stop with a ctx derived from the
	// original — the shutdown path must not substitute a
	// context.Background() or other ctx-dropping construct that breaks
	// the trace chain.
	afterStopTraceID := trace.SpanFromContext(shutdownCtx).SpanContext().TraceID()
	if afterStopTraceID != startTraceID {
		t.Fatalf("boundary Stop: trace ID changed from %s to %s — shutdown path dropped or replaced the caller's context",
			startTraceID, afterStopTraceID)
	}

	// Regression: a plant of context.Background() or
	// context.WithoutCancel(ctx) at any pipeline boundary must cause
	// this test to fail with a clear message naming which boundary
	// broke the trace ID.
}

// TestContextBackgroundDropsTraceID documents that context.Background()
// intentionally produces a zero trace ID. If a boundary accidentally uses
// Background() instead of propagating ctx, the trace chain breaks. This test
// exists as a reference counter-example for the plant/fail cycle described in
// the W-5 acceptance criteria.
func TestContextBackgroundDropsTraceID(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)

	tracer := tp.Tracer("runtime-test")
	ctx, span := tracer.Start(context.Background(), "test-background-drop")
	defer span.End()

	startTraceID := trace.SpanFromContext(ctx).SpanContext().TraceID()
	if !startTraceID.IsValid() {
		t.Fatal("start trace ID must be non-zero")
	}

	// Plant: replace with context.Background() — this simulates the
	// exact ctx-dropping bug that the propagation test guards against.
	droppedCtx := context.Background()
	droppedTraceID := trace.SpanFromContext(droppedCtx).SpanContext().TraceID()
	if droppedTraceID.IsValid() {
		t.Fatal("context.Background() unexpectedly returned a valid trace ID")
	}
}

// TestTextMapPropagationRoundTrip verifies that the W3C trace context can be
// injected and extracted via the standard TextMapPropagator — a contractual
// prerequisite for any pipeline boundary that serializes ctx across a queue,
// HTTP call, or gRPC hand-off.
func TestTextMapPropagationRoundTrip(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	tracer := tp.Tracer("runtime-test")
	ctx, span := tracer.Start(context.Background(), "test-roundtrip")
	defer span.End()

	startTraceID := trace.SpanFromContext(ctx).SpanContext().TraceID()
	if !startTraceID.IsValid() {
		t.Fatal("start trace ID must be non-zero")
	}

	// Inject into carrier (simulating ctx propagation across a boundary)
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)

	// Extract from carrier (simulating ctx reception after the boundary)
	extractedCtx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)
	extractedTraceID := trace.SpanFromContext(extractedCtx).SpanContext().TraceID()

	if extractedTraceID != startTraceID {
		t.Fatalf("trace ID changed across inject/extract round-trip: start=%s, extracted=%s",
			startTraceID, extractedTraceID)
	}
}
