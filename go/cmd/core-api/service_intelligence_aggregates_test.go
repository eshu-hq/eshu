// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// zeroRow answers every aggregate scan with zero counts and empty bucket maps.
type zeroRow struct{}

func (zeroRow) Scan(dest ...any) error {
	for i := range dest {
		switch target := dest[i].(type) {
		case *sql.NullInt64:
			*target = sql.NullInt64{Valid: true}
		case *[]byte:
			*target = []byte(`[]`)
		}
	}
	return nil
}

// countingReader is a read store whose single-row reads answer zero counts.
type countingReader struct{}

func (countingReader) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, sql.ErrConnDone
}
func (countingReader) QueryRowContext(context.Context, string, ...any) db.Row { return zeroRow{} }
func (countingReader) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, sql.ErrConnDone
}

// TestServiceIntelligenceAggregatesRecordTheirReads pins that the service
// intelligence report's aggregate store carries the instruments: a count through it
// increments eshu_dp_supply_chain_impact_read_route_total and records a duration
// sample for its route. Without them the report's route V reads (precise profile
// plus a workload, gate on) would be neither counted nor timed.
func TestServiceIntelligenceAggregatesRecordTheirReads(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("service-intelligence-aggregates-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	// Gate off: the legacy route needs no readiness probe, so the count is one
	// statement the zero-row reader answers.
	store := serviceIntelligenceAggregates(countingReader{}, false, nil, instruments)
	if _, err := store.CountSupplyChainImpactFindings(t.Context(), impact.AggregateFilter{DetectionProfile: impact.ProfilePrecise, WorkloadID: "workload:w"}); err != nil {
		t.Fatalf("count = %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	seen := map[string]bool{}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name == "eshu_dp_supply_chain_impact_read_route_total" || record.Name == "eshu_dp_supply_chain_impact_read_duration_seconds" {
				seen[record.Name] = true
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("recorded instruments %v, want both the route counter and the duration histogram", seen)
	}
}
