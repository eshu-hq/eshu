// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// TestReducerAdmissionDepthContract pins the closed signal and outcome value
// sets of eshu_dp_reducer_admission_depth_reads_total (#11233) and that
// NewInstruments registers all three shared-depth instruments.
func TestReducerAdmissionDepthContract(t *testing.T) {
	t.Parallel()

	if got, want := []string{ReducerAdmissionDepthSignalQueue, ReducerAdmissionDepthSignalGraphWriteTimeout},
		[]string{"queue_depth", "graph_write_timeout_depth"}; !slices.Equal(got, want) {
		t.Fatalf("depth signals = %v, want the closed set %v", got, want)
	}
	if got, want := []string{
		ReducerAdmissionDepthReadRead, ReducerAdmissionDepthReadShared, ReducerAdmissionDepthReadHeld,
		ReducerAdmissionDepthReadError, ReducerAdmissionDepthReadCancelled,
	}, []string{"read", "shared", "held", "error", "cancelled"}; !slices.Equal(got, want) {
		t.Fatalf("depth read outcomes = %v, want the closed set %v", got, want)
	}

	instruments, err := NewInstruments(sdkmetric.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if instruments.ReducerAdmissionDepthReads == nil {
		t.Fatal("ReducerAdmissionDepthReads counter not registered")
	}
	if instruments.ReducerAdmissionDepthAge == nil {
		t.Fatal("ReducerAdmissionDepthAge histogram not registered")
	}
	if instruments.ReducerAdmissionReadBackoff == nil {
		t.Fatal("ReducerAdmissionReadBackoff gauge not registered")
	}
}
