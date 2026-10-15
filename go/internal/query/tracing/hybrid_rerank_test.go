// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestStartHybridRerankSpanEmitsTheStageContract pins the re-rank stage span an
// operator reads at 3 AM: its name, its parent, and its three low-cardinality
// attributes, for both an applied and a lexical-fallback pass (#7746).
func TestStartHybridRerankSpanEmitsTheStageContract(t *testing.T) {
	for _, applied := range []bool{true, false} {
		recorder := tracetest.NewSpanRecorder()
		provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
		t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
		tracer := provider.Tracer(tracerName)

		parentCtx, parent := tracer.Start(context.Background(), "request")
		ctx, finish := StartHybridRerankSpan(parentCtx, tracer, HybridRerankToolFindCode, 150)
		if ctx == parentCtx {
			t.Fatal("StartHybridRerankSpan returned the parent context, want the stage span context")
		}
		finish(applied)
		parent.End()

		ended := recorder.Ended()
		if len(ended) != 2 {
			t.Fatalf("applied=%v: ended spans = %d, want stage and request", applied, len(ended))
		}
		stage := ended[0]
		if stage.Name() != telemetry.SpanQueryHybridRerank {
			t.Fatalf("stage span = %q, want %q", stage.Name(), telemetry.SpanQueryHybridRerank)
		}
		if stage.Parent().SpanID() != parent.SpanContext().SpanID() {
			t.Fatal("stage span is not a child of the request span")
		}
		assertHybridRerankAttributes(t, stage.Attributes(), HybridRerankToolFindCode, 150, applied)
	}
}

// assertHybridRerankAttributes checks the tool, row count, and applied flag
// recorded on one hybrid re-rank span.
func assertHybridRerankAttributes(t *testing.T, attrs []attribute.KeyValue, tool string, rows int64, applied bool) {
	t.Helper()
	seen := map[attribute.Key]attribute.Value{}
	for _, attr := range attrs {
		seen[attr.Key] = attr.Value
	}
	if got := seen[HybridRerankAttrTool].AsString(); got != tool {
		t.Errorf("%s = %q, want %q", HybridRerankAttrTool, got, tool)
	}
	if got := seen[HybridRerankAttrRows].AsInt64(); got != rows {
		t.Errorf("%s = %d, want %d", HybridRerankAttrRows, got, rows)
	}
	value, ok := seen[HybridRerankAttrApplied]
	if !ok {
		t.Fatalf("%s missing", HybridRerankAttrApplied)
	}
	if value.AsBool() != applied {
		t.Errorf("%s = %v, want %v", HybridRerankAttrApplied, value.AsBool(), applied)
	}
}
