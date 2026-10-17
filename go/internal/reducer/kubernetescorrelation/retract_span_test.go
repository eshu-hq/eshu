// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetescorrelation

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestKubernetesCorrelationMaterializationSpanCarriesRetractDecision proves
// the RUNS_IMAGE handler goes through the shared prior-generation predicate
// (#8167) and records the retract decision and its closed reason on its span.
func TestKubernetesCorrelationMaterializationSpanCarriesRetractDecision(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		prior       bool
		attempt     int
		wantSkip    bool
		wantReason  string
		wantRetract int
	}{
		{
			name: "first generation", prior: false, attempt: 1, wantSkip: true,
			wantReason: reducercontract.RetractReasonFirstGeneration, wantRetract: 0,
		},
		{
			name: "prior generation", prior: true, attempt: 1,
			wantReason: reducercontract.RetractReasonPriorGeneration, wantRetract: 1,
		},
		{
			name: "retry attempt", prior: false, attempt: 2,
			wantReason: reducercontract.RetractReasonRetryAttempt, wantRetract: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			writer := &recordingKubernetesCorrelationEdgeWriter{}
			prior := tt.prior
			handler := KubernetesCorrelationMaterializationHandler{
				FactLoader:           &stubKubernetesCorrelationFactLoader{scopeFacts: exactDigestEdgeFixture()},
				EdgeWriter:           writer,
				ReadinessLookup:      readyLookup(true, true),
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return prior, nil },
				Tracer:               provider.Tracer("test"),
			}
			intent := kubernetesCorrelationMaterializationIntent()
			intent.AttemptCount = tt.attempt
			if _, err := handler.Handle(context.Background(), intent); err != nil {
				t.Fatalf("Handle returned error: %v", err)
			}
			if writer.retractCalls != tt.wantRetract {
				t.Fatalf("retractCalls = %d, want %d", writer.retractCalls, tt.wantRetract)
			}
			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("ended spans = %d, want 1", len(spans))
			}
			assertRetractSpanAttrs(t, spans[0], tt.wantSkip, tt.wantReason)
		})
	}
}

// assertRetractSpanAttrs checks the skip_retract and skip_retract_reason span
// attributes.
func assertRetractSpanAttrs(t *testing.T, span sdktrace.ReadOnlySpan, wantSkip bool, wantReason string) {
	t.Helper()
	var gotSkip, sawSkip bool
	var gotReason string
	for _, attr := range span.Attributes() {
		switch string(attr.Key) {
		case telemetry.LogKeySkipRetract:
			gotSkip, sawSkip = attr.Value.AsBool(), true
		case telemetry.LogKeySkipRetractReason:
			gotReason = attr.Value.AsString()
		}
	}
	if !sawSkip || gotSkip != wantSkip {
		t.Fatalf("span %s = %v (present %v), want %v", telemetry.LogKeySkipRetract, gotSkip, sawSkip, wantSkip)
	}
	if gotReason != wantReason {
		t.Fatalf("span %s = %q, want %q", telemetry.LogKeySkipRetractReason, gotReason, wantReason)
	}
}
