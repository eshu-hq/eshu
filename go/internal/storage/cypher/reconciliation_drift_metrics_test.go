// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRecordReconciliationDriftRetractionsRequiresStatementMarker(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	RecordReconciliationDriftRetractions(context.Background(), instruments, Statement{
		Operation: OperationCanonicalRetract,
		Parameters: map[string]any{
			StatementMetadataPhaseKey: "retract",
		},
	}, 7, 11)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if metricPresent(rm, "eshu_dp_reconciliation_drift_retractions_total") {
		t.Fatal("unmarked statement recorded reconciliation drift metric")
	}
}

func TestRecordReconciliationDriftRetractionsRecordsNodeAndEdgeDeletes(t *testing.T) {
	t.Parallel()

	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}

	RecordReconciliationDriftRetractions(context.Background(), instruments, Statement{
		Operation: OperationCanonicalRetract,
		Parameters: map[string]any{
			StatementMetadataPhaseKey:               "entity_retract",
			StatementMetadataReconciliationDriftKey: true,
		},
	}, 7, 11)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := metricCounterValue(t, rm, "eshu_dp_reconciliation_drift_retractions_total", "kind", "node"); got != 7 {
		t.Fatalf("node drift retractions = %d, want 7", got)
	}
	if got := metricCounterValue(t, rm, "eshu_dp_reconciliation_drift_retractions_total", "kind", "edge"); got != 11 {
		t.Fatalf("edge drift retractions = %d, want 11", got)
	}
}

func metricPresent(rm metricdata.ResourceMetrics, name string) bool {
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name == name {
				return true
			}
		}
	}
	return false
}

func metricCounterValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	attrKey string,
	attrValue string,
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
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == attrKey && attr.Value.AsString() == attrValue {
						return point.Value
					}
				}
			}
		}
	}
	t.Fatalf("metric %s with %s=%q not found", name, attrKey, attrValue)
	return 0
}
