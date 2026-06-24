// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSearchIndexInstrumentsRecordBoundedLabels(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	if inst.SearchIndexMutations == nil {
		t.Fatal("SearchIndexMutations counter was not registered")
	}
	if inst.SearchIndexErrors == nil {
		t.Fatal("SearchIndexErrors counter was not registered")
	}
	if inst.SearchIndexWriteDuration == nil {
		t.Fatal("SearchIndexWriteDuration histogram was not registered")
	}

	inst.SearchIndexMutations.Add(context.Background(), 5, metric.WithAttributes(
		AttrDomain("eshu_search_document"),
		AttrKind("term"),
		AttrOperation("upsert"),
		AttrResult("success"),
	))
	inst.SearchIndexErrors.Add(context.Background(), 1, metric.WithAttributes(
		AttrDomain("eshu_search_document"),
		AttrOperation("term_upsert"),
	))
	inst.SearchIndexWriteDuration.Record(context.Background(), 0.25, metric.WithAttributes(
		AttrDomain("eshu_search_document"),
		AttrResult("success"),
	))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := searchIndexCounterValue(t, rm, "eshu_dp_search_index_mutations_total", map[string]string{
		MetricDimensionDomain:    "eshu_search_document",
		MetricDimensionKind:      "term",
		MetricDimensionOperation: "upsert",
		MetricDimensionResult:    "success",
	}); got != 5 {
		t.Fatalf("search-index mutations = %d, want 5", got)
	}
	if got := searchIndexCounterValue(t, rm, "eshu_dp_search_index_errors_total", map[string]string{
		MetricDimensionDomain:    "eshu_search_document",
		MetricDimensionOperation: "term_upsert",
	}); got != 1 {
		t.Fatalf("search-index errors = %d, want 1", got)
	}
	searchIndexHistogramPoint(t, rm, "eshu_dp_search_index_write_duration_seconds", map[string]string{
		MetricDimensionDomain: "eshu_search_document",
		MetricDimensionResult: "success",
	})
}

func searchIndexCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if searchIndexMetricPointHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %#v not found", name, wantAttrs)
	return 0
}

func searchIndexHistogramPoint(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Float64 histogram", name, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if searchIndexMetricPointHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					return
				}
			}
		}
	}
	t.Fatalf("histogram %s with attrs %#v not found", name, wantAttrs)
}

func searchIndexMetricPointHasAttrs(attrs []attribute.KeyValue, want map[string]string) bool {
	for wantKey, wantValue := range want {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == wantKey && attr.Value.AsString() == wantValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
