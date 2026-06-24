// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"slices"
	"testing"

	"go.opentelemetry.io/otel/metric/noop"
)

func TestScannerWorkerTelemetryContractNames(t *testing.T) {
	t.Parallel()

	dimensions := MetricDimensionKeys()
	for _, want := range []string{
		MetricDimensionAnalyzer,
		MetricDimensionTargetKind,
		MetricDimensionLimitKind,
	} {
		if !slices.Contains(dimensions, want) {
			t.Fatalf("MetricDimensionKeys() missing scanner dimension %q in %v", want, dimensions)
		}
	}

	spans := SpanNames()
	for _, want := range []string{
		SpanScannerWorkerClaimProcess,
		SpanScannerWorkerAnalyze,
		SpanScannerWorkerFactEmitBatch,
	} {
		if !slices.Contains(spans, want) {
			t.Fatalf("SpanNames() missing scanner span %q in %v", want, spans)
		}
	}

	inst, err := NewInstruments(noop.NewMeterProvider().Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	if inst.ScannerWorkerClaims == nil {
		t.Fatal("ScannerWorkerClaims counter is nil")
	}
	if inst.ScannerWorkerRetries == nil {
		t.Fatal("ScannerWorkerRetries counter is nil")
	}
	if inst.ScannerWorkerDeadLetters == nil {
		t.Fatal("ScannerWorkerDeadLetters counter is nil")
	}
	if inst.ScannerWorkerFactsEmitted == nil {
		t.Fatal("ScannerWorkerFactsEmitted counter is nil")
	}
	if inst.ScannerWorkerQueueWaitDuration == nil {
		t.Fatal("ScannerWorkerQueueWaitDuration histogram is nil")
	}
	if inst.ScannerWorkerScanDuration == nil {
		t.Fatal("ScannerWorkerScanDuration histogram is nil")
	}
	if inst.ScannerWorkerTargetCount == nil {
		t.Fatal("ScannerWorkerTargetCount histogram is nil")
	}
	if inst.ScannerWorkerResultCount == nil {
		t.Fatal("ScannerWorkerResultCount histogram is nil")
	}
	if inst.ScannerWorkerCPUSeconds == nil {
		t.Fatal("ScannerWorkerCPUSeconds histogram is nil")
	}
	if inst.ScannerWorkerMemoryBytes == nil {
		t.Fatal("ScannerWorkerMemoryBytes histogram is nil")
	}
}
