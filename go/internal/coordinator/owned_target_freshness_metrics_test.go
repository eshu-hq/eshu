// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
)

const (
	rotationPeriodMetric   = "eshu_dp_owned_target_rotation_period_seconds"
	rotationBoundMetric    = "eshu_dp_owned_target_max_staleness_seconds"
	rotationExceedsMetric  = "eshu_dp_owned_target_rotation_exceeds_max_staleness"
	instanceDimensionLabel = "collector_instance_id"
)

// TestNewMetricsPublishesOwnedTargetRotationGauges pins the three gauges an
// operator reads for #9775, one series per collector instance, and that a
// later unknown observation (derivation disabled or single_pass) removes the
// instance's series instead of leaving a stale breach on the dashboard.
func TestNewMetricsPublishesOwnedTargetRotationGauges(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	ctx := context.Background()
	recorder.RecordOwnedTargetRotation(ctx, OwnedTargetRotationObservation{
		CollectorInstanceID: "osv-qa",
		Freshness:           schedule.OwnedTargetRotationFreshness(1024, 30*time.Minute, 0),
	})
	recorder.RecordOwnedTargetRotation(ctx, OwnedTargetRotationObservation{
		CollectorInstanceID: "osv-ok",
		Freshness:           schedule.OwnedTargetRotationFreshness(32, 30*time.Minute, 24*time.Hour),
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	wantSeconds := map[string]struct{ period, bound, exceeds float64 }{
		"osv-qa": {period: (512 * time.Hour).Seconds(), bound: (24 * time.Hour).Seconds(), exceeds: 1},
		"osv-ok": {period: (16 * time.Hour).Seconds(), bound: (24 * time.Hour).Seconds(), exceeds: 0},
	}
	for instance, want := range wantSeconds {
		if got := rotationGaugeValue(t, rm, rotationPeriodMetric, instance); got != want.period {
			t.Errorf("%s period gauge = %v, want %v", instance, got, want.period)
		}
		if got := rotationGaugeValue(t, rm, rotationBoundMetric, instance); got != want.bound {
			t.Errorf("%s bound gauge = %v, want %v", instance, got, want.bound)
		}
		if got := rotationBreachValue(t, rm, instance); got != want.exceeds {
			t.Errorf("%s exceeds gauge = %v, want %v", instance, got, want.exceeds)
		}
	}

	recorder.RecordOwnedTargetRotation(ctx, OwnedTargetRotationObservation{CollectorInstanceID: "osv-qa"})
	var after metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &after); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := rotationSeriesCount(after, rotationPeriodMetric); got != 1 {
		t.Errorf("series after clearing osv-qa = %d, want 1 (osv-ok only)", got)
	}
	if _, found := lookupRotationGauge(after, rotationPeriodMetric, "osv-qa"); found {
		t.Error("osv-qa period series still exported after an unknown observation")
	}
}

func lookupRotationGauge(rm metricdata.ResourceMetrics, name, instance string) (float64, bool) {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != name {
				continue
			}
			gauge, ok := metricRecord.Data.(metricdata.Gauge[float64])
			if !ok {
				return 0, false
			}
			for _, dp := range gauge.DataPoints {
				if value, ok := dp.Attributes.Value(instanceDimensionLabel); ok && value.AsString() == instance {
					return dp.Value, true
				}
			}
		}
	}
	return 0, false
}

func rotationGaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name, instance string) float64 {
	t.Helper()
	value, found := lookupRotationGauge(rm, name, instance)
	if !found {
		t.Fatalf("metric %s for instance %q not found as a float gauge", name, instance)
	}
	return value
}

// rotationBreachValue reads the int64 breach gauge for one instance as 0 or 1.
func rotationBreachValue(t *testing.T, rm metricdata.ResourceMetrics, instance string) float64 {
	t.Helper()
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != rotationExceedsMetric {
				continue
			}
			gauge, ok := metricRecord.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Gauge[int64]", rotationExceedsMetric, metricRecord.Data)
			}
			for _, dp := range gauge.DataPoints {
				if value, ok := dp.Attributes.Value(instanceDimensionLabel); ok && value.AsString() == instance {
					return float64(dp.Value)
				}
			}
		}
	}
	t.Fatalf("metric %s for instance %q not found", rotationExceedsMetric, instance)
	return 0
}

func rotationSeriesCount(rm metricdata.ResourceMetrics, name string) int {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != name {
				continue
			}
			if gauge, ok := metricRecord.Data.(metricdata.Gauge[float64]); ok {
				return len(gauge.DataPoints)
			}
		}
	}
	return 0
}

// TestOwnedTargetRotationGaugesSurviveConcurrentRecordAndCollect pins the
// locking: the reconcile loop writes observations while the metrics reader's
// callback collects them on another goroutine. Run it with -race.
func TestOwnedTargetRotationGaugesSurviveConcurrentRecordAndCollect(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	ctx := context.Background()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			slots := int64(32)
			if i%3 == 0 {
				slots = 0 // an unknown observation deletes the series
			}
			recorder.RecordOwnedTargetRotation(ctx, OwnedTargetRotationObservation{
				CollectorInstanceID: "osv-race",
				Freshness:           schedule.OwnedTargetRotationFreshness(slots, 30*time.Minute, 0),
			})
		}
	}()
	for i := 0; i < 50; i++ {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(ctx, &rm); err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
	}
	<-done
}
