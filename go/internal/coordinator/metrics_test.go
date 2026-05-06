package coordinator

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewMetricsPublishesReconcileMetricsAndGauges(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := NewMetrics(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewMetrics() error = %v, want nil", err)
	}

	recorder.RecordReconcile(context.Background(), ReconcileObservation{
		Outcome:      reconcileOutcomeSuccess,
		Duration:     250 * time.Millisecond,
		DesiredCount: 3,
		DurableCount: 1,
	})
	recorder.RecordReap(context.Background(), ReapObservation{
		Outcome:    reaperOutcomeSuccess,
		Duration:   125 * time.Millisecond,
		ReapedRows: 2,
	})
	recorder.RecordRunReconciliation(context.Background(), RunReconciliationObservation{
		Outcome:        runReconcileOutcomeSuccess,
		Duration:       300 * time.Millisecond,
		ReconciledRuns: 4,
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got := coordinatorCounterValue(t, rm, coordinatorMetricPrefix+"reconcile_total", map[string]string{
		"outcome": reconcileOutcomeSuccess,
	}); got != 1 {
		t.Fatalf("reconcile total = %d, want 1", got)
	}
	if got := coordinatorHistogramCount(t, rm, coordinatorMetricPrefix+"reconcile_duration_seconds", map[string]string{
		"outcome": reconcileOutcomeSuccess,
	}); got != 1 {
		t.Fatalf("reconcile duration histogram count = %d, want 1", got)
	}
	if got := coordinatorGaugeValue(t, rm, coordinatorMetricPrefix+"desired_collector_instances"); got != 3 {
		t.Fatalf("desired collector instances = %d, want 3", got)
	}
	if got := coordinatorGaugeValue(t, rm, coordinatorMetricPrefix+"durable_collector_instances"); got != 1 {
		t.Fatalf("durable collector instances = %d, want 1", got)
	}
	if got := coordinatorGaugeValue(t, rm, coordinatorMetricPrefix+"collector_instance_drift"); got != 2 {
		t.Fatalf("collector instance drift = %d, want 2", got)
	}
	if got := coordinatorCounterValue(t, rm, coordinatorMetricPrefix+"reap_total", map[string]string{
		"outcome": reaperOutcomeSuccess,
	}); got != 1 {
		t.Fatalf("reap total = %d, want 1", got)
	}
	if got := coordinatorCounterValue(t, rm, coordinatorMetricPrefix+"run_reconcile_total", map[string]string{
		"outcome": runReconcileOutcomeSuccess,
	}); got != 1 {
		t.Fatalf("run reconcile total = %d, want 1", got)
	}
	if got := coordinatorGaugeValue(t, rm, coordinatorMetricPrefix+"last_reaped_claims"); got != 2 {
		t.Fatalf("last reaped claims = %d, want 2", got)
	}
	if got := coordinatorGaugeValue(t, rm, coordinatorMetricPrefix+"last_reconciled_runs"); got != 4 {
		t.Fatalf("last reconciled runs = %d, want 4", got)
	}
}

func TestNewMetricsRequiresMeter(t *testing.T) {
	t.Parallel()

	var meter metric.Meter
	metrics, err := NewMetrics(meter)
	if err == nil {
		t.Fatal("NewMetrics() error = nil, want non-nil")
	}
	if metrics != nil {
		t.Fatalf("NewMetrics() metrics = %#v, want nil", metrics)
	}
}

func coordinatorCounterValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Sum[int64]", metricName, metricRecord.Data)
			}
			for _, dp := range sum.DataPoints {
				if coordinatorAttrsMatch(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func coordinatorHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, metricName string, wantAttrs map[string]string) uint64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Histogram[float64]", metricName, metricRecord.Data)
			}
			for _, dp := range histogram.DataPoints {
				if coordinatorAttrsMatch(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Count
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func coordinatorGaugeValue(t *testing.T, rm metricdata.ResourceMetrics, metricName string) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}
			gauge, ok := metricRecord.Data.(metricdata.Gauge[int64])
			if !ok {
				t.Fatalf("metric %s data = %T, want metricdata.Gauge[int64]", metricName, metricRecord.Data)
			}
			if len(gauge.DataPoints) != 1 {
				t.Fatalf("metric %s datapoints = %d, want 1", metricName, len(gauge.DataPoints))
			}
			return gauge.DataPoints[0].Value
		}
	}

	t.Fatalf("metric %s not found", metricName)
	return 0
}

func coordinatorAttrsMatch(actual []attribute.KeyValue, want map[string]string) bool {
	if len(actual) != len(want) {
		return false
	}
	for _, attr := range actual {
		if want[string(attr.Key)] != attr.Value.AsString() {
			return false
		}
	}
	return true
}
