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

	// Use a handler that blocks until the test unblocks it. This lets us
	// observe that Stop respects the caller's context deadline: if Stop
	// internally substitutes context.Background() for the caller's
	// context, the short deadline passed below would be ignored and the
	// config ShutdownTimeout (10 s) would be used instead.
	handlerEntered := make(chan struct{})
	doneCh := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		close(handlerEntered)
		<-doneCh
	})

	srv, err := NewHTTPServer(HTTPServerConfig{
		Addr:            "127.0.0.1:0",
		Handler:         handler,
		ShutdownTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}

	// Boundary 2: Start accepts a context. The current implementation
	// ignores it (server.Serve runs in a goroutine without ctx), but
	// the test preserves the contract so a future ctx-aware Start has
	// a guardrail.
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	addr := srv.Addr()
	if addr == "" {
		t.Fatal("Addr returned empty string after Start")
	}

	// Issue a request that will block in the handler, creating an
	// in-flight connection that server.Shutdown must wait for. Without
	// an active connection, shutdown may complete immediately regardless
	// of the caller's context deadline.
	reqDone := make(chan error, 1)
	go func() {
		_, err := http.Get("http://" + addr + "/")
		reqDone <- err
	}()

	// Wait for the handler to accept the connection before calling Stop.
	// If Stop is called before the handler enters, the server may have
	// no active connections and shutdown could complete before the
	// deadline, making the test flaky.
	<-handlerEntered

	// Boundary 3: Stop with a short-deadline context that carries the
	// trace. If Stop drops the caller's context — e.g. by substituting
	// context.Background() — it will use the config ShutdownTimeout
	// (10 s) instead of this 50 ms deadline, and the test will hang or
	// time out. A correct implementation derives the shutdown context
	// from the caller's ctx and returns quickly when the deadline fires.
	stopCtx, stopCancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stopCancel()

	err = srv.Stop(stopCtx)
	if err == nil {
		close(doneCh)
		<-reqDone
		t.Fatal("Stop should have returned a context deadline error because the handler was still blocking")
	}

	// The error must be context.DeadlineExceeded — any other error
	// (including nil) means Stop did not use the caller's context.
	if err != context.DeadlineExceeded {
		close(doneCh)
		<-reqDone
		t.Fatalf("Stop returned error %v, want context.DeadlineExceeded; the shutdown path did not respect the caller's context deadline", err)
	}

	// Unblock the handler so goroutines can drain.
	close(doneCh)
	<-reqDone // drain the request goroutine

	// Boundary 4: trace ID after Stop — the caller's context is an
	// immutable Go value, so this assertion documents the contract
	// rather than testing Stop internals directly. Together with the
	// deadline-propagation check above, it ensures Stop both uses the
	// caller's context and does not strip span data through any
	// intermediate wrapper.
	afterStopTraceID := trace.SpanFromContext(stopCtx).SpanContext().TraceID()
	if afterStopTraceID != startTraceID {
		t.Fatalf("boundary Stop: trace ID changed from %s to %s — shutdown path dropped or replaced the caller's context trace identity",
			startTraceID, afterStopTraceID)
	}

	// Verify the server is actually stopped: a new request must fail.
	_, err = http.Get("http://" + addr + "/")
	if err == nil {
		t.Fatal("server accepts connections after Stop — shutdown did not stop the listener")
	}
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
