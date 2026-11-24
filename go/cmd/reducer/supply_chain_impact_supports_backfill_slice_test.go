// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// verifySliceSamples collects the verification slice counter and histogram
// samples by outcome.
func verifySliceSamples(t *testing.T, reader *sdkmetric.ManualReader) (counts map[string]int64, durations map[string]float64) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	counts, durations = map[string]int64{}, map[string]float64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_supply_chain_impact_supports_backfill_verify_slices_total":
				for _, p := range m.Data.(metricdata.Sum[int64]).DataPoints {
					v, _ := p.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
					counts[v.AsString()] += p.Value
				}
			case "eshu_dp_supply_chain_impact_supports_backfill_verify_slice_duration_seconds":
				for _, p := range m.Data.(metricdata.Histogram[float64]).DataPoints {
					v, _ := p.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
					durations[v.AsString()] += p.Sum
				}
			}
		}
	}
	return counts, durations
}

// TestRunSupportsBackfillPassRecordsAFailedSlice proves a step that fails inside
// a verification slice (a statement timeout) still lands on the slice counter and
// histogram as outcome failed with its duration before the error returns, and
// that a slice cut short by shutdown (the pass context cancelled) records nothing.
func TestRunSupportsBackfillPassRecordsAFailedSlice(t *testing.T) {
	t.Parallel()
	timeout := errors.New("canceling statement due to statement timeout")
	failed := func(context.Context) (postgres.SupplyChainImpactSupportsBackfillReport, error) {
		return postgres.SupplyChainImpactSupportsBackfillReport{
			Verified:            true,
			VerifySlice:         telemetry.SupplyChainImpactSupportsVerifySliceFailed,
			VerifySliceDuration: 30 * time.Second,
		}, timeout
	}

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("instruments: %v", err)
	}
	if _, err := runSupportsBackfillPass(context.Background(), failed, instruments, nil); !errors.Is(err, timeout) {
		t.Fatalf("pass error = %v, want the step's error", err)
	}
	counts, durations := verifySliceSamples(t, reader)
	if counts[telemetry.SupplyChainImpactSupportsVerifySliceFailed] != 1 || durations[telemetry.SupplyChainImpactSupportsVerifySliceFailed] != 30 {
		t.Fatalf("failed slice samples = counts %v durations %v, want one failed slice of 30 s", counts, durations)
	}

	shutdownReader := sdkmetric.NewManualReader()
	shutdownInstruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(shutdownReader)).Meter("test"))
	if err != nil {
		t.Fatalf("instruments: %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runSupportsBackfillPass(cancelled, failed, shutdownInstruments, nil); err == nil {
		t.Fatal("a cancelled pass with a failing step returned no error")
	}
	if counts, _ := verifySliceSamples(t, shutdownReader); len(counts) != 0 {
		t.Fatalf("slice samples after shutdown = %v, want none: a cancelled slice is not a failed one", counts)
	}
}
