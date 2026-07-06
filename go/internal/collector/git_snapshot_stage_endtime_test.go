// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRecordSnapshotStageAtExcludesPostEndTimeWork proves recordSnapshotStageAt
// records only the startedAt..endedAt window and ignores any work the caller
// performs after capturing endedAt but before making the call. This is the
// mechanism the pre_scan call site now relies on: it must capture endedAt
// before building the per-language telemetry summary, or that summary-build
// cost inflates eshu_dp_collector_snapshot_stage_duration_seconds (#4767).
func TestRecordSnapshotStageAtExcludesPostEndTimeWork(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("stage-endtime-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Instruments: instruments}

	startedAt := time.Now()
	endedAt := startedAt.Add(5 * time.Millisecond)

	snapshotter.recordSnapshotStageAt(
		context.Background(), "/repo", telemetry.SnapshotStagePreScan, startedAt, endedAt,
	)

	// Simulate expensive post-endedAt work (the per-file summary-build loop
	// preScanLanguageSummary performs). This must NOT be reflected in the
	// recorded stage duration because endedAt was already captured above.
	time.Sleep(50 * time.Millisecond)

	gotSeconds := stageHistogramSum(t, metricReader, "eshu_dp_collector_snapshot_stage_duration_seconds", map[string]string{
		"collector_kind": "git",
		"stage":          telemetry.SnapshotStagePreScan,
	})

	// Allow generous scheduling slack, but the recorded duration must stay far
	// below the 50ms of post-endedAt work — proving that work was excluded.
	if gotSeconds >= 0.03 {
		t.Fatalf(
			"recordSnapshotStageAt duration = %.4fs, want < 0.03s (must exclude post-endedAt work)",
			gotSeconds,
		)
	}
}

// TestRecordSnapshotStageSelfCapturesEndTimeAtCallTime proves the
// self-capturing recordSnapshotStage variant DOES fold in any work the caller
// does while constructing its variadic attrs, since Go evaluates all call
// arguments before the function executes. This documents why a call site
// whose attrs construction is expensive (a per-file telemetry summary loop)
// must switch to recordSnapshotStageAt instead of recordSnapshotStage (#4767).
func TestRecordSnapshotStageSelfCapturesEndTimeAtCallTime(t *testing.T) {
	t.Parallel()

	metricReader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))
	instruments, err := telemetry.NewInstruments(meterProvider.Meter("stage-endtime-selfcapture-test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}

	snapshotter := NativeRepositorySnapshotter{Instruments: instruments}

	startedAt := time.Now()
	// expensiveAttr simulates a per-file summary-build loop evaluated as a
	// call argument: Go evaluates it before recordSnapshotStage runs, so its
	// cost lands inside the measured window.
	expensiveAttr := func() string {
		time.Sleep(50 * time.Millisecond)
		return "summary"
	}

	snapshotter.recordSnapshotStage(
		context.Background(), "/repo", telemetry.SnapshotStagePreScan, startedAt,
		expensiveAttr(),
	)

	gotSeconds := stageHistogramSum(t, metricReader, "eshu_dp_collector_snapshot_stage_duration_seconds", map[string]string{
		"collector_kind": "git",
		"stage":          telemetry.SnapshotStagePreScan,
	})

	if gotSeconds < 0.05 {
		t.Fatalf(
			"recordSnapshotStage duration = %.4fs, want >= 0.05s (self-captured endedAt must include pre-call attrs work)",
			gotSeconds,
		)
	}
}

// stageHistogramSum reads the summed value of the named float64 histogram
// matching wantAttrs from a manual reader collection.
func stageHistogramSum(
	t *testing.T,
	reader *sdkmetric.ManualReader,
	metricName string,
	wantAttrs map[string]string,
) float64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf(
					"metric %s data = %T, want metricdata.Histogram[float64]",
					metricName,
					metricRecord.Data,
				)
			}
			for _, dp := range histogram.DataPoints {
				if collectorHasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Sum
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}
