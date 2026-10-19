// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestCompletionWakeContract pins the closed outcome value set of
// eshu_dp_reducer_completion_wake_total (#8294) and that NewInstruments
// registers the counter.
func TestCompletionWakeContract(t *testing.T) {
	t.Parallel()

	if got, want := []string{CompletionWakeOutcomeReopened, CompletionWakeOutcomeNoop, CompletionWakeOutcomeError},
		[]string{"reopened", "noop", "error"}; !slices.Equal(got, want) {
		t.Fatalf("completion wake outcomes = %v, want the closed set %v", got, want)
	}

	instruments, err := NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.ReducerCompletionWake == nil {
		t.Fatal("ReducerCompletionWake counter not registered")
	}
}
