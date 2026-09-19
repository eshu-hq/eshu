// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitrepo

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/parser/fingerprint"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRecordFingerprintStatsEmitsCounterAndHistogram proves the snapshot
// parse emits eshu_dp_code_fingerprint_entities_total by outcome/reason and
// the per-file eshu_dp_code_fingerprint_duration_seconds histogram (#6835).
// Before this telemetry an operator could not tell whether a missing
// fingerprint meant "below floor", "error graph", or "no body".
func TestRecordFingerprintStatsEmitsCounterAndHistogram(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("fingerprint-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Instruments: instruments}

	parsed := map[string]any{
		fingerprint.StatsKey: map[string]any{
			"fingerprinted": 2,
			"below_floor":   3,
			"has_error":     1,
			"no_body":       4,
			"micros_total":  1500,
		},
	}
	snapshotter.recordFingerprintStats(context.Background(), parsed, "go")

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	sum := fingerprintCounterSum(t, rm, map[string]string{
		"language": "go", "outcome": "fingerprinted",
	})
	if sum != 2 {
		t.Fatalf("fingerprinted sum = %d, want 2", sum)
	}
	sum = fingerprintCounterSum(t, rm, map[string]string{
		"language": "go", "outcome": "skipped", "reason": fingerprint.ReasonBelowFloor,
	})
	if sum != 3 {
		t.Fatalf("below_floor sum = %d, want 3", sum)
	}
	sum = fingerprintCounterSum(t, rm, map[string]string{
		"language": "go", "outcome": "skipped", "reason": fingerprint.ReasonHasError,
	})
	if sum != 1 {
		t.Fatalf("has_error sum = %d, want 1", sum)
	}
	sum = fingerprintCounterSum(t, rm, map[string]string{
		"language": "go", "outcome": "skipped", "reason": fingerprint.ReasonNoBody,
	})
	if sum != 4 {
		t.Fatalf("no_body sum = %d, want 4", sum)
	}
	count := scipHistogramCount(t, rm, "eshu_dp_code_fingerprint_duration_seconds", map[string]string{
		"language": "go",
	})
	if count != 1 {
		t.Fatalf("fingerprint duration histogram count = %d, want 1", count)
	}
}

// TestRecordFingerprintStatsSilentWithoutStats proves pre-fingerprint
// payloads (no StatsKey) and a nil instrument set emit nothing.
func TestRecordFingerprintStatsSilentWithoutStats(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("fingerprint-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Instruments: instruments}
	snapshotter.recordFingerprintStats(context.Background(), map[string]any{}, "go")

	bare := NativeRepositorySnapshotter{}
	bare.recordFingerprintStats(context.Background(), map[string]any{
		fingerprint.StatsKey: map[string]any{"fingerprinted": 1},
	}, "go")

	var rm metricdata.ResourceMetrics
	if err := metricReader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name == "eshu_dp_code_fingerprint_entities_total" ||
				metricRecord.Name == "eshu_dp_code_fingerprint_duration_seconds" {
				t.Fatalf("metric %s emitted, want silent", metricRecord.Name)
			}
		}
	}
}

func fingerprintCounterSum(t *testing.T, rm metricdata.ResourceMetrics, wantAttrs map[string]string) int64 {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != "eshu_dp_code_fingerprint_entities_total" {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric data = %T, want metricdata.Sum[int64]", metricRecord.Data)
			}
			for _, dp := range sum.DataPoints {
				if collectorHasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}
	t.Fatalf("counter with attrs %v not found", wantAttrs)
	return 0
}
