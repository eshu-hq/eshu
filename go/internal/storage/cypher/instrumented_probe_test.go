// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// instrumentedExecutorSatisfiesProbeExecutor is a compile-time assertion that
// InstrumentedExecutor carries the #5998 ProbeExecutor capability. This is the
// review F1 static guard: InstrumentedExecutor sits directly below the
// reducer's backpressure gates in production
// (go/cmd/reducer/observed_service_wiring.go:42-46) and above
// reducerNeo4jExecutor, so a future edit that drops this method would make the
// probe guard permanently report "unsupported" in production while every
// wrapper's own type assertion still appears to succeed (BackpressureExecutor
// always implements ExecuteProbe regardless of what it wraps) -- exactly the
// silent-in-the-middle failure this assertion, plus
// TestInstrumentedExecutorForwardsExecuteProbe below, are meant to catch at
// build/test time instead of in production telemetry.
var _ ProbeExecutor = (*InstrumentedExecutor)(nil)

// TestInstrumentedExecutorForwardsExecuteProbe proves ExecuteProbe delegates
// to Inner.ExecuteProbe (not Execute) and returns its found result unchanged
// when Inner implements ProbeExecutor.
func TestInstrumentedExecutorForwardsExecuteProbe(t *testing.T) {
	t.Parallel()

	inner := &probeCapableExecutor{probeFound: true}
	ie := &InstrumentedExecutor{Inner: inner}

	stmt := Statement{Operation: OperationCanonicalProbe, Cypher: "MATCH (r) RETURN r LIMIT 1"}
	found, err := ie.ExecuteProbe(context.Background(), stmt)
	if err != nil {
		t.Fatalf("ExecuteProbe() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ExecuteProbe() found = false, want true")
	}
	if got := int(inner.probeCalls.Load()); got != 1 {
		t.Errorf("probeCalls = %d, want 1", got)
	}
	if got := int(inner.executeCalls.Load()); got != 0 {
		t.Errorf("executeCalls = %d, want 0 (must not fall back to Execute)", got)
	}
}

// TestInstrumentedExecutorExecuteProbeErrorsWithoutProbeExecutor proves
// ExecuteProbe fails closed with a clear error, never a silent "not found",
// when Inner does not implement ProbeExecutor.
func TestInstrumentedExecutorExecuteProbeErrorsWithoutProbeExecutor(t *testing.T) {
	t.Parallel()

	inner := &recordingExecutor{}
	ie := &InstrumentedExecutor{Inner: inner}

	found, err := ie.ExecuteProbe(context.Background(), Statement{Cypher: "RETURN 1 LIMIT 1"})
	if err == nil {
		t.Fatal("ExecuteProbe() error = nil, want non-nil (Inner does not implement ProbeExecutor)")
	}
	if found {
		t.Fatal("ExecuteProbe() found = true, want false on the unsupported path")
	}
}

// TestInstrumentedExecutorExecuteProbePropagatesError proves a probe error
// from Inner surfaces unchanged, and sets the span status to error.
func TestInstrumentedExecutorExecuteProbePropagatesError(t *testing.T) {
	t.Parallel()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	tracer := tracerProvider.Tracer("test")

	wantErr := errors.New("neo4j read session unavailable")
	inner := &probeCapableExecutor{probeErr: wantErr, failFor: 1}
	ie := &InstrumentedExecutor{Inner: inner, Tracer: tracer}

	_, err := ie.ExecuteProbe(context.Background(), Statement{Cypher: "RETURN 1 LIMIT 1"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("ExecuteProbe() error = %v, want %v", err, wantErr)
	}

	spans := spanRecorder.Ended()
	if got, want := len(spans), 1; got != want {
		t.Fatalf("len(spans) = %d, want %d", got, want)
	}
	if got, want := spans[0].Name(), "neo4j.execute_probe"; got != want {
		t.Fatalf("span.Name() = %q, want %q", got, want)
	}
	if got, want := spans[0].Status().Code, codes.Error; got != want {
		t.Fatalf("span.Status().Code = %v, want %v", got, want)
	}
}

// TestInstrumentedExecutorExecuteProbeRecordsDuration proves ExecuteProbe
// records the same eshu_dp_neo4j_query_duration_seconds histogram Execute and
// ExecuteGroup use, labelled operation=probe so a probe's cost is
// distinguishable from a write's in the existing dashboard.
func TestInstrumentedExecutorExecuteProbeRecordsDuration(t *testing.T) {
	t.Parallel()

	metricReader := metric.NewManualReader()
	meterProvider := metric.NewMeterProvider(metric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	inner := &probeCapableExecutor{probeFound: false}
	ie := &InstrumentedExecutor{Inner: inner, Instruments: instruments}

	ctx := context.Background()
	if _, err := ie.ExecuteProbe(ctx, Statement{Cypher: "RETURN 1 LIMIT 1"}); err != nil {
		t.Fatalf("ExecuteProbe() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	assertFloat64HistogramCount(t, rm, "eshu_dp_neo4j_query_duration_seconds", map[string]string{
		"operation": "probe",
	}, 1)
}
