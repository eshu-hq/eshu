package collector

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func collectorCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()

	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metricRecord := range scopeMetrics.Metrics {
			if metricRecord.Name != metricName {
				continue
			}

			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf(
					"metric %s data = %T, want metricdata.Sum[int64]",
					metricName,
					metricRecord.Data,
				)
			}

			for _, dp := range sum.DataPoints {
				if collectorHasAttrs(dp.Attributes.ToSlice(), wantAttrs) {
					return dp.Value
				}
			}
		}
	}

	t.Fatalf("metric %s with attrs %v not found", metricName, wantAttrs)
	return 0
}

func collectorHasAttrs(actual []attribute.KeyValue, want map[string]string) bool {
	matched := 0
	for _, attr := range actual {
		wantValue, ok := want[string(attr.Key)]
		if !ok {
			continue
		}
		if wantValue != attr.Value.AsString() {
			return false
		}
		matched++
	}
	return matched == len(want)
}
