// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	epssDerivedAdmittedMetric = "eshu_dp_workflow_coordinator_epss_derived_admitted_total"
	epssDerivedSkippedMetric  = "eshu_dp_workflow_coordinator_epss_derived_skipped_total"
)

// TestNewMetricsPublishesEPSSDerivedSignals pins the two #9876 operator
// signals: planned findings-derived EPSS targets, and skipped CVE ids
// labelled by the closed EPSS skip-reason set. Counts only: CVE ids never
// reach a label, and non-EPSS skip evidence never reaches these counters.
func TestNewMetricsPublishesEPSSDerivedSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	ctx := context.Background()
	recorder.RecordEPSSDerivedPage(ctx, EPSSDerivedObservation{
		PlannedTargets: 3,
		Skipped: []schedule.DerivedTargetSkipEvidence{
			{
				TargetClass:  schedule.TargetClassFindingCVE,
				Reason:       schedule.DerivedTargetSkipReasonNonCVEIdentifier,
				SkippedCount: 2,
			},
			{
				TargetClass:  schedule.TargetClassFindingCVE,
				Reason:       schedule.DerivedTargetSkipReasonBudgetExhausted,
				SkippedCount: 100,
			},
			{
				// Owned-package evidence from the same plan must not
				// leak into the EPSS counters.
				TargetClass:  schedule.TargetClassOwnedPackage,
				Reason:       schedule.DerivedTargetSkipReasonBudgetExhausted,
				SkippedCount: 7,
			},
			{
				// Blank reasons and non-positive counts never reach
				// the counter.
				TargetClass:  schedule.TargetClassFindingCVE,
				SkippedCount: 0,
			},
		},
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := epssCounterValue(rm, epssDerivedAdmittedMetric, ""); got != 3 {
		t.Errorf("admitted EPSS targets = %d, want 3", got)
	}
	for reason, want := range map[string]int64{
		schedule.DerivedTargetSkipReasonNonCVEIdentifier: 2,
		schedule.DerivedTargetSkipReasonBudgetExhausted:  100,
	} {
		if got := epssCounterValue(rm, epssDerivedSkippedMetric, reason); got != want {
			t.Errorf("skipped reason %s = %d, want %d", reason, got, want)
		}
	}
}

func epssCounterValue(rm metricdata.ResourceMetrics, name, reason string) int64 {
	var total int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				if reason == "" {
					total += dp.Value
					continue
				}
				got, _ := dp.Attributes.Value(telemetry.MetricDimensionReason)
				if got.AsString() == reason {
					total += dp.Value
				}
			}
		}
	}
	return total
}
