// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"context"
	"testing"

	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestListEmitsRuntimeDeclarationsSpan pins the span name and the route and
// capability attributes operators build triage queries on. Not parallel: it
// swaps the package tracer.
func TestListEmitsRuntimeDeclarationsSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := tracesdk.NewTracerProvider(tracesdk.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	previous := declarationHandlerTracer
	declarationHandlerTracer = provider.Tracer("runtime-declaration-span-test")
	t.Cleanup(func() { declarationHandlerTracer = previous })

	post(newMux(&fakeGraph{}), `{}`)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want exactly 1", len(spans))
	}
	if got := spans[0].Name(); got != telemetry.SpanQueryRuntimeDeclarations {
		t.Fatalf("span name = %q, want %q", got, telemetry.SpanQueryRuntimeDeclarations)
	}
	attributes := map[string]any{}
	for _, item := range spans[0].Attributes() {
		attributes[string(item.Key)] = item.Value.AsInterface()
	}
	if attributes["http.route"] != listRoute || attributes["eshu.capability"] != Capability {
		t.Fatalf("span attributes = %v", attributes)
	}
}
