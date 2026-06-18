package azurecloud

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestNewMetricsRegistersInstruments(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("azurecloud-test")

	m, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics error: %v", err)
	}

	ctx := context.Background()
	boundary := testBoundary()
	m.RecordAPICall(ctx, boundary, "resources_list", StatusClassSuccess)
	m.RecordSkipTokenResume(ctx, boundary)
	m.RecordPartialScope(ctx, boundary, WarningPartialScope)
	m.RecordFactsEmitted(ctx, boundary, "azure_cloud_resource", 3)
	m.RecordFreshnessLag(ctx, boundary, 42.0)
	m.RecordClaim(ctx, ClaimStatusSucceeded)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	labels := collectLabelKeys(t, rm)
	// Bounded-label guard: telemetry must never carry raw identifiers.
	forbidden := []string{
		"arm_resource_id", "subscription_id", "subscription", "tenant_id",
		"resource_group", "resource_name", "resource_id", "location",
		"tags", "kql", "query", "url", "credential",
	}
	for _, key := range forbidden {
		if _, present := labels[key]; present {
			t.Fatalf("telemetry exposed forbidden label %q; labels=%v", key, keysOf(labels))
		}
	}
	// Bounded labels we do allow.
	for _, key := range []string{"collector_kind", "scope_kind", "source_lane"} {
		if _, present := labels[key]; !present {
			t.Fatalf("expected bounded label %q to be present; labels=%v", key, keysOf(labels))
		}
	}
}

func TestNewMetricsRejectsNilMeter(t *testing.T) {
	if _, err := NewMetrics(nil); err == nil {
		t.Fatal("expected error for nil meter")
	}
}

func TestNopMetricsSafe(t *testing.T) {
	var m Metrics = NopMetrics{}
	ctx := context.Background()
	boundary := testBoundary()
	// Must not panic.
	m.RecordAPICall(ctx, boundary, "resources_list", StatusClassSuccess)
	m.RecordSkipTokenResume(ctx, boundary)
	m.RecordPartialScope(ctx, boundary, WarningPartialScope)
	m.RecordFactsEmitted(ctx, boundary, "azure_cloud_resource", 1)
	m.RecordFreshnessLag(ctx, boundary, 1.0)
	m.RecordClaim(ctx, ClaimStatusFailed)
}

func TestRecordClaimUsesBoundedLabels(t *testing.T) {
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	meter := provider.Meter("azurecloud-claim-test")

	m, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics error: %v", err)
	}

	ctx := context.Background()
	m.RecordClaim(ctx, ClaimStatusSucceeded)
	m.RecordClaim(ctx, ClaimStatusPartial)
	m.RecordClaim(ctx, ClaimStatusFailed)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	labels := collectLabelKeys(t, rm)
	for _, forbidden := range []string{"subscription_id", "tenant_id", "credential", "scope_id"} {
		if _, present := labels[forbidden]; present {
			t.Fatalf("claim telemetry exposed forbidden label %q; labels=%v", forbidden, keysOf(labels))
		}
	}
	for _, want := range []string{"collector_kind", "status"} {
		if _, present := labels[want]; !present {
			t.Fatalf("claim telemetry missing bounded label %q; labels=%v", want, keysOf(labels))
		}
	}
}

func collectLabelKeys(t *testing.T, rm metricdata.ResourceMetrics) map[string]struct{} {
	t.Helper()
	labels := map[string]struct{}{}
	for _, scope := range rm.ScopeMetrics {
		for _, metricItem := range scope.Metrics {
			switch data := metricItem.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range data.DataPoints {
					addAttrKeys(labels, dp.Attributes.ToSlice())
				}
			case metricdata.Histogram[float64]:
				for _, dp := range data.DataPoints {
					addAttrKeys(labels, dp.Attributes.ToSlice())
				}
			}
		}
	}
	return labels
}

func addAttrKeys(into map[string]struct{}, attrs []attribute.KeyValue) {
	for _, attr := range attrs {
		into[string(attr.Key)] = struct{}{}
	}
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}
