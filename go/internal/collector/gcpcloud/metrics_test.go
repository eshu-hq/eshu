// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMetricsRecordBoundedLabels(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("gcpcloud-test")

	metrics, err := NewMetrics(meter)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}

	ctx := context.Background()
	metrics.RecordClaim(ctx, catalog.ClaimStatusSucceeded)
	metrics.RecordAPICall(ctx, "assets.list", catalog.ParentScopeProject, "compute", "resource", "ok")
	metrics.RecordPage(ctx, catalog.ParentScopeProject)
	metrics.RecordPageTokenResume(ctx, catalog.ParentScopeProject)
	metrics.RecordFactsEmitted(ctx, "gcp_cloud_resource", catalog.ParentScopeProject, 3)
	metrics.RecordWarning(ctx, catalog.WarningKindPartialPermission, catalog.OutcomePartial)
	metrics.RecordAttributeExtraction(ctx, "bigquery", catalog.ExtractionOutcomeExtracted)
	metrics.RecordFreshnessLag(ctx, catalog.ParentScopeProject, 42.0)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	// Every recorded attribute value must be a bounded enum: assert no metric
	// attribute carries a raw resource id, project id, full resource name, or URL.
	forbidden := []string{
		"my-project", "123456789", "//compute.googleapis.com", "vm-1",
		"alice@example.com", "https://",
	}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			for _, value := range attributeValues(m) {
				for _, bad := range forbidden {
					if strings.Contains(value, bad) {
						t.Fatalf("metric %q leaked unbounded label value %q", m.Name, value)
					}
				}
			}
		}
	}

	if len(rm.ScopeMetrics) == 0 {
		t.Fatal("expected at least one scope metric recorded")
	}
}

func TestMetricsRecordsAttributeExtractionCounter(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := NewMetrics(provider.Meter("gcpcloud-test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	ctx := context.Background()
	metrics.RecordAttributeExtraction(ctx, "bigquery", catalog.ExtractionOutcomeExtracted)
	metrics.RecordAttributeExtraction(ctx, "bigquery", catalog.ExtractionOutcomeEmpty)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	found := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "eshu_dp_gcp_cloud_attribute_extractions_total" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("eshu_dp_gcp_cloud_attribute_extractions_total counter was not recorded")
	}
}

func attributeValues(m metricdata.Metrics) []string {
	var values []string
	switch data := m.Data.(type) {
	case metricdata.Sum[int64]:
		for _, dp := range data.DataPoints {
			for _, attr := range dp.Attributes.ToSlice() {
				values = append(values, attr.Value.AsString())
			}
		}
	case metricdata.Histogram[float64]:
		for _, dp := range data.DataPoints {
			for _, attr := range dp.Attributes.ToSlice() {
				values = append(values, attr.Value.AsString())
			}
		}
	}
	return values
}
