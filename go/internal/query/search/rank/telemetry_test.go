// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestRecordOutcomeCountsAndTagsTheRequestSpan is not parallel: it installs a
// process-global meter provider (see testutil.WithPackageMetricReader).
func TestRecordOutcomeCountsAndTagsTheRequestSpan(t *testing.T) {
	reader := testutil.WithPackageMetricReader(t, resetInstrumentsForTest)

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, span := provider.Tracer("test").Start(context.Background(), "request")
	RecordOutcome(ctx, ToolFindCode, OutcomeServed, 3)
	RecordOutcome(ctx, ToolFindCode, OutcomeBasisMismatch, 0)
	RecordOutcome(ctx, ToolSearchEntityContent, OutcomeServed, 1)
	span.End()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	counts := map[[2]string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != CursorOutcomeMetric {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data = %T, want Sum[int64]", m.Name, m.Data)
			}
			for _, point := range sum.DataPoints {
				tool, _ := point.Attributes.Value(attribute.Key("tool"))
				outcome, _ := point.Attributes.Value(attribute.Key("outcome"))
				counts[[2]string{tool.AsString(), outcome.AsString()}] += point.Value
			}
		}
	}
	want := map[[2]string]int64{
		{ToolFindCode, OutcomeServed}:            1,
		{ToolFindCode, OutcomeBasisMismatch}:     1,
		{ToolSearchEntityContent, OutcomeServed}: 1,
	}
	for key, value := range want {
		if counts[key] != value {
			t.Fatalf("count%v = %d, want %d (all: %v)", key, counts[key], value, counts)
		}
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	attrs := map[attribute.Key]attribute.Value{}
	for _, kv := range spans[0].Attributes() {
		attrs[kv.Key] = kv.Value
	}
	if got := attrs[AttrOutcome].AsString(); got != OutcomeServed {
		t.Fatalf("span %s = %q, want the last outcome %q", AttrOutcome, got, OutcomeServed)
	}
	if got := attrs[AttrServed].AsInt64(); got != 1 {
		t.Fatalf("span %s = %d, want 1", AttrServed, got)
	}
}

// TestOutcomeLabelsAreClosed pins the bounded outcome label set.
func TestOutcomeLabelsAreClosed(t *testing.T) {
	t.Parallel()
	want := []string{OutcomeServed, OutcomeWindowEnd, OutcomeBasisMismatch, OutcomeInvalid, OutcomeUnsupported}
	if len(outcomes) != len(want) {
		t.Fatalf("outcomes = %v, want %v", outcomes, want)
	}
	for _, outcome := range want {
		if _, ok := outcomes[outcome]; !ok {
			t.Fatalf("outcome %q missing from the closed set", outcome)
		}
	}
	if got := boundedOutcome("free text"); got != OutcomeInvalid {
		t.Fatalf("boundedOutcome(free text) = %q, want %q", got, OutcomeInvalid)
	}
}

// TestRecordErrorMapsEveryCodeToItsOutcome is not parallel: it installs a
// process-global meter provider.
func TestRecordErrorMapsEveryCodeToItsOutcome(t *testing.T) {
	reader := testutil.WithPackageMetricReader(t, resetInstrumentsForTest)
	ctx := context.Background()
	RecordError(ctx, ToolFindCode, mismatch(ReasonRankOrderChanged, "m"))
	RecordError(ctx, ToolFindCode, invalid(ReasonMalformed, "m"))
	RecordError(ctx, ToolFindCode, Unsupported(ReasonRankerDisabled))
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == CursorOutcomeMetric {
				for _, point := range sum.DataPoints {
					outcome, _ := point.Attributes.Value(attribute.Key("outcome"))
					got[outcome.AsString()] += point.Value
				}
			}
		}
	}
	for _, outcome := range []string{OutcomeBasisMismatch, OutcomeInvalid, OutcomeUnsupported} {
		if got[outcome] != 1 {
			t.Fatalf("outcome %q count = %d, want 1 (all %v)", outcome, got[outcome], got)
		}
	}
}
