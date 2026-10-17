// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestIAMCanPerformSpanCarriesRetractDecision proves the CAN_PERFORM handler
// goes through the shared prior-generation predicate (#8167) and records the
// retract decision and its closed reason on its span.
func TestIAMCanPerformSpanCarriesRetractDecision(t *testing.T) {
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
			writer := &recordingIAMCanPerformWriter{}
			prior := tt.prior
			handler := IAMCanPerformMaterializationHandler{
				FactLoader:           &stubFactLoader{envelopes: iamCanPerformFacts()},
				Writer:               writer,
				ReadinessLookup:      allKeyspacesReady(),
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return prior, nil },
				Tracer:               provider.Tracer("test"),
			}
			intent := iamCanPerformIntent()
			intent.AttemptCount = tt.attempt
			if _, err := handler.Handle(context.Background(), intent); err != nil {
				t.Fatalf("Handle returned error: %v", err)
			}
			if writer.retractCalls != tt.wantRetract {
				t.Fatalf("retractCalls = %d, want %d", writer.retractCalls, tt.wantRetract)
			}
			var handlerSpan sdktrace.ReadOnlySpan
			for _, span := range recorder.Ended() {
				if span.Name() == telemetry.SpanReducerIAMCanPerformMaterialization {
					handlerSpan = span
				}
			}
			if handlerSpan == nil {
				t.Fatalf("no %s span ended", telemetry.SpanReducerIAMCanPerformMaterialization)
			}
			var gotSkip, sawSkip bool
			var gotReason string
			for _, attr := range handlerSpan.Attributes() {
				switch string(attr.Key) {
				case telemetry.LogKeySkipRetract:
					gotSkip, sawSkip = attr.Value.AsBool(), true
				case telemetry.LogKeySkipRetractReason:
					gotReason = attr.Value.AsString()
				}
			}
			if !sawSkip || gotSkip != tt.wantSkip {
				t.Fatalf("span skip_retract = %v (present %v), want %v", gotSkip, sawSkip, tt.wantSkip)
			}
			if gotReason != tt.wantReason {
				t.Fatalf("span skip_retract_reason = %q, want %q", gotReason, tt.wantReason)
			}
		})
	}
}
