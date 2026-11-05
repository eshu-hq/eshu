// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
)

// Step-6 signal names, mirrored from owned_target_replan_metrics.go: the
// coverage verifier only recognizes literal constructor names, so the test
// pins its own literals beside the production ones.
const (
	replanRecordsMetric      = "eshu_dp_osv_delta_records_total"
	replanWatermarkAgeMetric = "eshu_dp_osv_delta_watermark_age_seconds"
	replanPollAgeMetric      = "eshu_dp_osv_delta_poll_age_seconds"
	replanDepthMetric        = "eshu_dp_owned_target_replan_depth"
	replanOldestMetric       = "eshu_dp_owned_target_replan_oldest_age_seconds"
)

// TestNewMetricsPublishesOSVDeltaReplanSignals pins the five #9777 step-2
// operator signals: the records counter with the closed outcome set, the
// watermark and poll age gauges (absent until the first commit and the
// first successful pass), and the replan depth and oldest-age gauges. Fetch
// bytes emit from the satellite poller at enablement, never in-tree.
func TestNewMetricsPublishesOSVDeltaReplanSignals(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}
	ctx := context.Background()
	recorder.RecordOSVDeltaReplan(ctx, OSVDeltaReplanMetricsObservation{
		CollectorInstanceID: "osv-qa",
		Admitted:            7,
		Duplicates:          3,
		ParseFailed:         1,
		Drained:             5,
		Overflow:            2,
		PendingDepth:        2,
		OldestPendingAge:    30 * time.Minute,
		OldestPendingKnown:  true,
		WatermarkAge:        10 * time.Minute,
		WatermarkKnown:      true,
		PollAge:             5 * time.Minute,
		PollKnown:           true,
	})
	recorder.RecordOSVDeltaReplan(ctx, OSVDeltaReplanMetricsObservation{
		CollectorInstanceID: "osv-warming",
		Admitted:            1,
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for outcome, want := range map[string]int64{
		vulnerability.OSVDeltaOutcomeAdmitted:         7,
		vulnerability.OSVDeltaOutcomeDuplicate:        3,
		vulnerability.OSVDeltaOutcomeParseFailed:      1,
		vulnerability.OSVDeltaOutcomeDrained:          5,
		vulnerability.OSVDeltaOutcomeOverflowRetained: 2,
	} {
		if got := replanRecordCount(rm, "osv-qa", outcome); got != want {
			t.Errorf("osv-qa outcome %s count = %d, want %d", outcome, got, want)
		}
	}
	if got := replanRecordCount(rm, "osv-qa", "invented"); got != 0 {
		t.Errorf("invented outcome count = %d, want 0 (closed outcome set)", got)
	}
	if got := rotationGaugeValue(t, rm, replanDepthMetric, "osv-qa"); got != 2 {
		t.Errorf("osv-qa replan depth = %v, want 2", got)
	}
	if got := rotationGaugeValue(t, rm, replanWatermarkAgeMetric, "osv-qa"); got != (10 * time.Minute).Seconds() {
		t.Errorf("osv-qa watermark age = %v, want %v", got, (10 * time.Minute).Seconds())
	}
	if got := rotationGaugeValue(t, rm, replanPollAgeMetric, "osv-qa"); got != (5 * time.Minute).Seconds() {
		t.Errorf("osv-qa poll age = %v, want %v", got, (5 * time.Minute).Seconds())
	}
	if got := rotationGaugeValue(t, rm, replanOldestMetric, "osv-qa"); got != (30 * time.Minute).Seconds() {
		t.Errorf("osv-qa oldest age = %v, want %v", got, (30 * time.Minute).Seconds())
	}
	// An instance with no commit and no successful pass exports no age
	// series and a zero depth, never a stale zero age.
	if _, found := lookupRotationGauge(rm, replanWatermarkAgeMetric, "osv-warming"); found {
		t.Error("an instance with no committed watermark must export no watermark age series")
	}
	if _, found := lookupRotationGauge(rm, replanPollAgeMetric, "osv-warming"); found {
		t.Error("an instance with no successful poll must export no poll age series")
	}
	if got := rotationGaugeValue(t, rm, replanDepthMetric, "osv-warming"); got != 0 {
		t.Errorf("osv-warming replan depth = %v, want 0", got)
	}

	// A nil recorder is a no-op: the flag-off path never calls it, and a
	// nil Metrics must not panic either.
	var nilRecorder *otelMetrics
	nilRecorder.RecordOSVDeltaReplan(ctx, OSVDeltaReplanMetricsObservation{})
}

func replanRecordCount(rm metricdata.ResourceMetrics, instance, outcome string) int64 {
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != replanRecordsMetric {
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
