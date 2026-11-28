// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// instrumentsFailingReadStore is a db.ReadStore whose every read fails. A
// refused or failed read still records its route counter (countRoute runs
// before the readiness and watermark probes) and, on the legacy route with no
// probes, its duration sample, so it proves the wiring carried Instruments to
// the store without needing a live database.
type instrumentsFailingReadStore struct{ err error }

type instrumentsFailingRow struct{ err error }

func (r instrumentsFailingRow) Scan(...any) error { return r.err }

func (s instrumentsFailingReadStore) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, s.err
}

func (s instrumentsFailingReadStore) QueryRowContext(context.Context, string, ...any) db.Row {
	return instrumentsFailingRow(s)
}

func (s instrumentsFailingReadStore) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	return nil, s.err
}

// newSatelliteInstrumentsTestStack builds Instruments over a ManualReader so
// the test can assert actual metric emission.
func newSatelliteInstrumentsTestStack(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	instruments, err := telemetry.NewInstruments(provider.Meter("satellite-instruments-test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments, reader
}

// satelliteRouteTotals reads eshu_dp_supply_chain_impact_read_route_total by route.
func satelliteRouteTotals(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_supply_chain_impact_read_route_total" {
				continue
			}
			sum, ok := record.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("counter data = %T, want Sum[int64]", record.Data)
			}
			for _, point := range sum.DataPoints {
				route, _ := point.Attributes.Value("route")
				out[route.AsString()] += point.Value
			}
		}
	}
	return out
}

// satelliteDurationSamples reads the sample count of
// eshu_dp_supply_chain_impact_read_duration_seconds by route.
func satelliteDurationSamples(t *testing.T, reader *sdkmetric.ManualReader) map[string]uint64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	out := map[string]uint64{}
	for _, scope := range rm.ScopeMetrics {
		for _, record := range scope.Metrics {
			if record.Name != "eshu_dp_supply_chain_impact_read_duration_seconds" {
				continue
			}
			histogram, ok := record.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("histogram data = %T, want Histogram[float64]", record.Data)
			}
			for _, point := range histogram.DataPoints {
				route, _ := point.Attributes.Value("route")
				out[route.AsString()] += point.Count
			}
		}
	}
	return out
}

// TestSatelliteSupplyChainHandlerCarriesInstruments proves the satellite
// defect: newSupplyChainHandler must carry the same Instruments instance onto
// the Findings, Aggregates, GroupedInventory, and Explanations impact stores,
// exactly as newCoreSupplyChainHandler does. Identity, not merely non-nil, so
// the satellite emits on the shared meter.
func TestSatelliteSupplyChainHandlerCarriesInstruments(t *testing.T) {
	t.Parallel()

	instruments, _ := newSatelliteInstrumentsTestStack(t)
	handler := newSupplyChainHandler(
		&fakeSuppressionAdmission{}, instrumentsFailingReadStore{err: errors.New("boom")},
		nil, nil, query.QueryProfile(""), false, nil, instruments,
	)
	if handler == nil {
		t.Fatal("newSupplyChainHandler() = nil")
	}
	findings, ok := handler.ImpactFindings.(impact.PostgresFindingStore)
	if !ok {
		t.Fatalf("handler.ImpactFindings = %T, want impact.PostgresFindingStore", handler.ImpactFindings)
	}
	if findings.Instruments != instruments {
		t.Fatalf("ImpactFindings.Instruments = %p, want the shared instance %p", findings.Instruments, instruments)
	}
	aggregates, ok := handler.ImpactAggregates.(impact.PostgresAggregateStore)
	if !ok {
		t.Fatalf("handler.ImpactAggregates = %T, want impact.PostgresAggregateStore", handler.ImpactAggregates)
	}
	if aggregates.Instruments != instruments {
		t.Fatalf("ImpactAggregates.Instruments = %p, want the shared instance %p", aggregates.Instruments, instruments)
	}
	grouped, ok := handler.ImpactGroupedInventory.(impact.PostgresGroupedInventoryStore)
	if !ok {
		t.Fatalf("handler.ImpactGroupedInventory = %T, want impact.PostgresGroupedInventoryStore", handler.ImpactGroupedInventory)
	}
	if grouped.Instruments != instruments {
		t.Fatalf("ImpactGroupedInventory.Instruments = %p, want the shared instance %p", grouped.Instruments, instruments)
	}
	explanations, ok := handler.ImpactExplanations.(impact.PostgresFindingStore)
	if !ok {
		t.Fatalf("handler.ImpactExplanations = %T, want impact.PostgresFindingStore", handler.ImpactExplanations)
	}
	if explanations.Instruments != instruments {
		t.Fatalf("ImpactExplanations.Instruments = %p, want the shared instance %p", explanations.Instruments, instruments)
	}
}

// TestSatelliteSupplyChainHandlerEmitsReadRouteTelemetry proves the wired
// instruments actually emit: a legacy-route aggregate count through the
// handler's ImpactAggregates store increments read_route_total and records a
// read_duration_seconds sample. The stub fails the statement, but the route
// counter and the probe-free legacy duration are recorded before the failure
// surfaces.
func TestSatelliteSupplyChainHandlerEmitsReadRouteTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newSatelliteInstrumentsTestStack(t)
	handler := newSupplyChainHandler(
		&fakeSuppressionAdmission{}, instrumentsFailingReadStore{err: errors.New("boom")},
		nil, nil, query.QueryProfile(""), false, nil, instruments,
	)
	if handler == nil {
		t.Fatal("newSupplyChainHandler() = nil")
	}
	aggregates, ok := handler.ImpactAggregates.(impact.PostgresAggregateStore)
	if !ok {
		t.Fatalf("handler.ImpactAggregates = %T, want impact.PostgresAggregateStore", handler.ImpactAggregates)
	}
	if _, err := aggregates.CountSupplyChainImpactFindings(context.Background(), impact.AggregateFilter{}); err == nil {
		t.Fatal("CountSupplyChainImpactFindings() = nil, want the stub read failure")
	}
	if got := satelliteRouteTotals(t, reader)["legacy"]; got != 1 {
		t.Fatalf("read_route_total{route=legacy} = %d, want 1", got)
	}
	if got := satelliteDurationSamples(t, reader)["legacy"]; got != 1 {
		t.Fatalf("read_duration_seconds{route=legacy} count = %d, want 1", got)
	}
}

// TestSatelliteSupplyChainEvidenceSourceEmitsReadRouteTelemetry proves the SI
// report path carries instruments too: the aggregate store behind
// newSupplyChainEvidenceSourceWithReadStore must emit its route counter and
// duration sample when the report section reads. The stub fails the read, but
// the legacy route records both before the failure surfaces.
func TestSatelliteSupplyChainEvidenceSourceEmitsReadRouteTelemetry(t *testing.T) {
	t.Parallel()

	instruments, reader := newSatelliteInstrumentsTestStack(t)
	source := newSupplyChainEvidenceSourceWithReadStore(
		instrumentsFailingReadStore{err: errors.New("boom")}, nil, false, instruments,
	)
	if _, err := source.SupplyChainInventoryForWorkload(context.Background(), "workload:satellite-instruments"); err == nil {
		t.Fatal("SupplyChainInventoryForWorkload() = nil, want the stub read failure")
	}
	if got := satelliteRouteTotals(t, reader)["legacy"]; got != 1 {
		t.Fatalf("SI read_route_total{route=legacy} = %d, want 1", got)
	}
	if got := satelliteDurationSamples(t, reader)["legacy"]; got != 1 {
		t.Fatalf("SI read_duration_seconds{route=legacy} count = %d, want 1", got)
	}
}
