// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// discardExporter ends spans without retaining them, so a long benchmark does
// not grow memory the way a span recorder would.
type discardExporter struct{}

func (discardExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (discardExporter) Shutdown(context.Context) error                             { return nil }

// noopObserver is a legacy Observer with no context method and no work.
type noopObserver struct{}

func (noopObserver) Observe(string, Stage, Outcome, time.Duration) {}

// newBenchTracer returns a tracer whose spans are recorded, ended through a
// synchronous exporter, and discarded.
func newBenchTracer(b *testing.B) trace.Tracer {
	b.Helper()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(discardExporter{})))
	b.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return provider.Tracer("benchmark")
}

func newBenchObserver(b *testing.B, tracer trace.Tracer) Observer {
	b.Helper()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(sdkmetric.NewManualReader())).Meter("benchmark")
	observer, err := NewObserver(meter, tracer)
	if err != nil {
		b.Fatal(err)
	}
	return observer
}

// BenchmarkReaderQueryObserve measures the callback cost of one fenced
// guarded-reader query (four stage observations: borrow, identity, replay,
// business query) against the in-process fake driver. It isolates what the
// observer and request context add; it does not measure a network, a real
// PostgreSQL, a deployed endpoint, or exporter I/O. The arms differ only in
// the observer and whether a recording request span exists:
//
//	nil_observer          no observer, no request span (the floor)
//	legacy_observer       an Observe-only observer, no request span
//	otel_no_request_span  the OpenTelemetry observer, no request span
//	otel_request_span     the OpenTelemetry observer under a recording request span
func BenchmarkReaderQueryObserve(b *testing.B) {
	arms := []struct {
		name        string
		observer    func(*testing.B, trace.Tracer) Observer
		requestSpan bool
	}{
		{"nil_observer", func(*testing.B, trace.Tracer) Observer { return nil }, false},
		{"legacy_observer", func(*testing.B, trace.Tracer) Observer { return noopObserver{} }, false},
		{"otel_no_request_span", newBenchObserver, false},
		{"otel_request_span", newBenchObserver, true},
	}
	for _, arm := range arms {
		b.Run(arm.name, func(b *testing.B) {
			tracer := newBenchTracer(b)
			access, base := newStageFixtureAccess(b, arm.observer(b, tracer), 0)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ctx := base
				var request trace.Span
				if arm.requestSpan {
					ctx, request = tracer.Start(base, "request")
				}
				runStageFixtureQuery(b, access, ctx)
				if request != nil {
					request.End()
				}
			}
		})
	}
}
