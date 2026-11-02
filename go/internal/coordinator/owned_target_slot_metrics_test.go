// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	slotAgeMetric       = "eshu_dp_owned_target_oldest_slot_age_seconds"
	slotSelectionMetric = "eshu_dp_owned_target_slot_selection_total"
)

// TestNewMetricsPublishesOwnedTargetSlotSignals pins the two #9776 operator
// signals: the oldest planned slot age gauge, one series per collector
// instance and absent when the age is unknown, and the selection counter
// labelled by the closed outcome set.
func TestNewMetricsPublishesOwnedTargetSlotSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	ctx := context.Background()
	recorder.RecordOwnedTargetSlotSelection(ctx, OwnedTargetSlotObservation{
		CollectorInstanceID: "osv-qa",
		Outcomes:            []string{workflow.SlotSelectionOldest, workflow.SlotSelectionAdvanced},
		OldestSlotAge:       90 * time.Minute,
		AgeKnown:            true,
	})
	recorder.RecordOwnedTargetSlotSelection(ctx, OwnedTargetSlotObservation{
		CollectorInstanceID: "osv-qa",
		Outcomes:            []string{workflow.SlotSelectionSameBucketReuse, workflow.SlotSelectionLostRace},
		OldestSlotAge:       90 * time.Minute,
		AgeKnown:            true,
	})
	recorder.RecordOwnedTargetSlotSelection(ctx, OwnedTargetSlotObservation{
		CollectorInstanceID: "osv-warming",
		Outcomes:            []string{workflow.SlotSelectionOldest},
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := rotationGaugeValue(t, rm, slotAgeMetric, "osv-qa"); got != (90 * time.Minute).Seconds() {
		t.Errorf("osv-qa slot age gauge = %v, want %v", got, (90 * time.Minute).Seconds())
	}
	if _, found := lookupRotationGauge(rm, slotAgeMetric, "osv-warming"); found {
		t.Error("an instance whose age is unknown (first rotation still warming) must export no age series")
	}
	for outcome, want := range map[string]int64{
		workflow.SlotSelectionOldest:          2,
		workflow.SlotSelectionAdvanced:        1,
		workflow.SlotSelectionSameBucketReuse: 1,
		workflow.SlotSelectionLostRace:        1,
	} {
		if got := slotSelectionCount(rm, "osv-qa", outcome) + slotSelectionCount(rm, "osv-warming", outcome); got != want {
			t.Errorf("outcome %s count = %d, want %d", outcome, got, want)
		}
	}

	// A later unknown age removes the series so a stale breach never lingers.
	recorder.RecordOwnedTargetSlotSelection(ctx, OwnedTargetSlotObservation{CollectorInstanceID: "osv-qa"})
	var after metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &after); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if _, found := lookupRotationGauge(after, slotAgeMetric, "osv-qa"); found {
		t.Error("osv-qa age series still exported after an unknown observation")
	}
}

func slotSelectionCount(rm metricdata.ResourceMetrics, instance, outcome string) int64 {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != slotSelectionMetric {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				return 0
			}
			for _, dp := range sum.DataPoints {
				id, _ := dp.Attributes.Value(instanceDimensionLabel)
				out, _ := dp.Attributes.Value("outcome")
				if id.AsString() == instance && out.AsString() == outcome {
					return dp.Value
				}
			}
		}
	}
	return 0
}
