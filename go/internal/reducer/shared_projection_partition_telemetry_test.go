// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestRecordSharedProjectionPartitionMetrics_HistogramAndCounter proves that
// recordSharedProjectionPartitionMetrics emits:
//  1. eshu_dp_shared_projection_partition_processing_seconds with
//     projection_domain and partition_id labels.
//  2. eshu_dp_shared_projection_intents_completed_total with projection_domain
//     label and correct count.
//
// This is the primary drain-path telemetry for #3624 Phase 1.
func TestRecordSharedProjectionPartitionMetrics_HistogramAndCounter(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	runner := SharedProjectionRunner{Instruments: inst}

	const domain = DomainInheritanceEdges
	const partitionID = 3
	const durationSeconds = 0.42

	runner.recordSharedProjectionPartitionMetrics(
		context.Background(),
		domain,
		partitionID,
		durationSeconds,
		PartitionProcessResult{ProcessedIntents: 7},
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Assert histogram recorded with correct labels.
	histName := "eshu_dp_shared_projection_partition_processing_seconds"
	if !histogramHasAttrsAndValue(rm, histName, map[string]any{
		telemetry.MetricDimensionDomain:      domain,
		telemetry.MetricDimensionPartitionID: int64(partitionID),
	}, durationSeconds) {
		t.Errorf("%s: no data point with domain=%q partition_id=%d duration~=%.3f", histName, domain, partitionID, durationSeconds)
	}

	// Assert counter recorded with correct domain and count.
	counterName := "eshu_dp_shared_projection_intents_completed_total"
	if !counterHasValue(rm, counterName, domain, 7) {
		t.Errorf("%s: expected count=7 for domain=%q", counterName, domain)
	}
}

// TestRecordSharedProjectionPartitionMetrics_SkipsZeroDuration proves that a
// zero total-duration cycle (e.g. lease not acquired, no timing recorded) does
// not emit a spurious zero histogram bucket.
func TestRecordSharedProjectionPartitionMetrics_SkipsZeroDuration(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	runner := SharedProjectionRunner{Instruments: inst}

	runner.recordSharedProjectionPartitionMetrics(
		context.Background(),
		DomainSQLRelationships,
		0,
		0.0, // zero duration — must be skipped
		PartitionProcessResult{ProcessedIntents: 0},
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	// Neither instrument should have any data points.
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_shared_projection_partition_processing_seconds",
				"eshu_dp_shared_projection_intents_completed_total":
				t.Errorf("unexpected metric %q emitted for zero-duration zero-processed cycle", m.Name)
			}
		}
	}
}

// TestRecordSharedProjectionPartitionMetrics_CardinalityBounded proves that
// the instruments only carry bounded dimension keys (domain + partition_id).
// Raw intent IDs, scope IDs, and generation IDs must never appear.
func TestRecordSharedProjectionPartitionMetrics_CardinalityBounded(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	runner := SharedProjectionRunner{Instruments: inst}
	runner.recordSharedProjectionPartitionMetrics(
		context.Background(),
		DomainHandlesRoute,
		5,
		0.10,
		PartitionProcessResult{ProcessedIntents: 3},
	)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	forbidden := []string{"intent_id", "scope_id", "generation_id", "acceptance_unit_id", "repository_id"}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_shared_projection_partition_processing_seconds":
				hist, ok := m.Data.(metricdata.Histogram[float64])
				if !ok {
					continue
				}
				for _, dp := range hist.DataPoints {
					for _, key := range forbidden {
						if _, ok := dp.Attributes.Value(attribute.Key(key)); ok {
							t.Errorf("%s data point carries forbidden label %q", m.Name, key)
						}
					}
				}
			case "eshu_dp_shared_projection_intents_completed_total":
				sum, ok := m.Data.(metricdata.Sum[int64])
				if !ok {
					continue
				}
				for _, dp := range sum.DataPoints {
					for _, key := range forbidden {
						if _, ok := dp.Attributes.Value(attribute.Key(key)); ok {
							t.Errorf("%s data point carries forbidden label %q", m.Name, key)
						}
					}
				}
			}
		}
	}
}

// histogramHasAttrsAndValue checks that at least one histogram data point
// matches the given attribute map and has a sum within 1% of wantSum.
// Attribute values may be string or int64.
func histogramHasAttrsAndValue(rm metricdata.ResourceMetrics, name string, attrs map[string]any, wantSum float64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			for _, dp := range hist.DataPoints {
				match := true
				for key, want := range attrs {
					got, ok := dp.Attributes.Value(attribute.Key(key))
					if !ok {
						match = false
						break
					}
					var gotVal any
					switch got.Type() {
					case attribute.STRING:
						gotVal = got.AsString()
					case attribute.INT64:
						gotVal = got.AsInt64()
					default:
						match = false
					}
					if gotVal != want {
						match = false
						break
					}
				}
				if match && dp.Count > 0 {
					// wantSum should appear in the histogram sum (within float tolerance).
					diff := dp.Sum - wantSum
					if diff < 0 {
						diff = -diff
					}
					if diff <= wantSum*0.01+0.0001 {
						return true
					}
				}
			}
		}
	}
	return false
}

// counterHasValue checks that a Sum[int64] metric carries at least one data
// point with the given domain label and value.
func counterHasValue(rm metricdata.ResourceMetrics, name, domain string, wantValue int64) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				got, ok := dp.Attributes.Value(attribute.Key(telemetry.MetricDimensionDomain))
				if !ok {
					continue
				}
				if got.AsString() == domain && dp.Value == wantValue {
					return true
				}
			}
		}
	}
	return false
}
