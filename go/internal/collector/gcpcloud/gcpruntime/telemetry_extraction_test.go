// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

// TestRecordAttributeExtractionsOutcomes proves the source records one extraction
// outcome per resource whose asset type has a registered extractor (extracted vs
// empty) and skips asset types with no extractor entirely.
func TestRecordAttributeExtractionsOutcomes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := gcpcloud.NewMetrics(provider.Meter("gcpruntime-test"))
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	s := &Source{Metrics: metrics}

	s.recordAttributeExtractions(context.Background(), []gcpcloud.ResourceObservation{
		{AssetType: "bigquery.googleapis.com/Table", Attributes: map[string]any{"table_type": "TABLE"}},
		{AssetType: "bigquery.googleapis.com/Table"}, // extractor ran, no attributes
		{AssetType: "eshu.test/UntypedAsset"},        // no extractor -> skipped
	})

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}

	byOutcome := map[string]int64{}
	total := int64(0)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_gcp_cloud_attribute_extractions_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("unexpected metric data type %T", m.Data)
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
				for _, attr := range dp.Attributes.ToSlice() {
					switch attr.Value.AsString() {
					case gcpcloud.ExtractionOutcomeExtracted, gcpcloud.ExtractionOutcomeEmpty:
						byOutcome[attr.Value.AsString()] += dp.Value
					}
				}
			}
		}
	}

	if got := byOutcome[gcpcloud.ExtractionOutcomeExtracted]; got != 1 {
		t.Errorf("extracted count = %d, want 1", got)
	}
	if got := byOutcome[gcpcloud.ExtractionOutcomeEmpty]; got != 1 {
		t.Errorf("empty count = %d, want 1", got)
	}
	if total != 2 {
		t.Errorf("total extraction datapoints = %d, want 2 (untyped sentinel must be skipped)", total)
	}
}

func TestRecordAttributeExtractionsNilMetricsIsSafe(t *testing.T) {
	s := &Source{}
	// Must not panic when telemetry is disabled.
	s.recordAttributeExtractions(context.Background(), []gcpcloud.ResourceObservation{
		{AssetType: "bigquery.googleapis.com/Table", Attributes: map[string]any{"x": 1}},
	})
}
