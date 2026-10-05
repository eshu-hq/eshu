// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package maintenance

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func settleSpans(t *testing.T, maintainErr error, finalizes []ActivationFinalizeResult) []sdktrace.ReadOnlySpan {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	store := &fakeActivationStore{
		queue:     []ActivationObligation{{ScopeID: "git:span", GenerationID: "g1", LeaseToken: 7}},
		finalizes: map[string][]ActivationFinalizeResult{"g1": finalizes},
	}
	runner, _, _ := newActivationRunnerForTest(t, store, &fakeActivationMaintainer{err: maintainErr})
	runner.Tracer = provider.Tracer("test")
	_, err := runner.RunOnce(context.Background())
	require.NoError(t, err)
	return recorder.Ended()
}

func spanAttr(span sdktrace.ReadOnlySpan, key string) string {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.String()
		}
	}
	return ""
}

// D1R-9: one span per settle, scope and generation as span attributes (never
// metric labels); a designed hold is not a trace error, a failed callback is.
func TestActivationRunnerTracesEachSettle(t *testing.T) {
	t.Parallel()
	completed := settleSpans(t, nil, []ActivationFinalizeResult{{Outcome: ActivationOutcomeCompleted, Woken: 1}})
	require.Len(t, completed, 1)
	require.Equal(t, "reducer.activation_obligation_settle", completed[0].Name())
	require.Equal(t, "git:span", spanAttr(completed[0], "scope_id"))
	require.Equal(t, "g1", spanAttr(completed[0], "generation_id"))
	require.Equal(t, "7", spanAttr(completed[0], "claim_token"))
	require.Equal(t, "completed", spanAttr(completed[0], "outcome"))
	require.Equal(t, codes.Unset, completed[0].Status().Code)

	held := settleSpans(t, HoldActivation("catalog_changed", errors.New("refused")),
		[]ActivationFinalizeResult{{Outcome: ActivationOutcomePhaseNotReady}})
	require.Len(t, held, 1)
	require.Equal(t, "catalog_changed", spanAttr(held[0], "hold_reason"))
	require.Equal(t, codes.Unset, held[0].Status().Code, "a designed hold is not a trace error")

	failed := settleSpans(t, fmt.Errorf("graph unavailable"), []ActivationFinalizeResult{{Outcome: ActivationOutcomePhaseNotReady}})
	require.Len(t, failed, 1)
	require.Equal(t, "maintenance", spanAttr(failed[0], "failure_reason"))
	require.Equal(t, codes.Error, failed[0].Status().Code)
}

// With no tracer the runner must not write onto whatever span its context
// already carries (the D3 N2 failure class).
func TestActivationRunnerWithoutATracerLeavesTheCallerSpanAlone(t *testing.T) {
	t.Parallel()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	ctx, parent := provider.Tracer("caller").Start(context.Background(), "caller")
	store := &fakeActivationStore{
		queue:     []ActivationObligation{{ScopeID: "git:span", GenerationID: "g1"}},
		finalizes: map[string][]ActivationFinalizeResult{"g1": {{Outcome: ActivationOutcomePhaseNotReady}}},
	}
	runner, _, _ := newActivationRunnerForTest(t, store, &fakeActivationMaintainer{err: errors.New("graph unavailable")})
	_, err := runner.RunOnce(ctx)
	require.NoError(t, err)
	parent.End()
	ended := recorder.Ended()
	require.Len(t, ended, 1)
	require.Empty(t, ended[0].Attributes())
	require.Equal(t, codes.Unset, ended[0].Status().Code)
}
