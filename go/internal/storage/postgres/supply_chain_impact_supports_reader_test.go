// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func supportsReadyFake(rows [][]any) *fakeExecQueryer {
	return &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: rows}}}
}

func supportsReaderMetrics(t *testing.T) (*telemetry.Instruments, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	return inst, reader
}

// TestRequireSupplyChainImpactSupportsReadyUsesThePinnedPredicate pins that the
// helper runs the pinned readiness statement and nothing else.
func TestRequireSupplyChainImpactSupportsReadyUsesThePinnedPredicate(t *testing.T) {
	t.Parallel()

	database := supportsReadyFake([][]any{{true}})
	if err := requireSupplyChainImpactSupportsReady(context.Background(), database, nil, "x"); err != nil {
		t.Fatalf("requireSupplyChainImpactSupportsReady() error = %v, want nil", err)
	}
	if len(database.queries) != 1 || !strings.Contains(database.queries[0].query, supplyChainImpactSupportsReadySQL) {
		t.Fatalf("queries = %+v, want exactly the pinned readiness statement", database.queries)
	}
}

// TestRequireSupplyChainImpactSupportsReadyRefusesNonCounting pins the refusal:
// a not-ready table returns the typed error that is retryable, an expected wait
// and carries the non-counting class, and counts not_ready for its reader.
func TestRequireSupplyChainImpactSupportsReadyRefusesNonCounting(t *testing.T) {
	t.Parallel()

	inst, reader := supportsReaderMetrics(t)
	err := requireSupplyChainImpactSupportsReady(context.Background(), supportsReadyFake([][]any{{false}}), inst,
		telemetry.SupplyChainImpactSupportsReaderSecurityAlertReconciliation)
	var notReady *supplychaincore.SupplyChainImpactSupportsNotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("error = %v, want *SupplyChainImpactSupportsNotReadyError", err)
	}
	if !reducercontract.IsRetryable(err) || !reducercontract.IsExpectedWait(err) ||
		notReady.FailureClass() != supplychaincore.SupplyChainImpactSupportsNotReadyFailureClass {
		t.Fatalf("refusal retryable=%v expectedWait=%v class=%q, want retryable expected wait of the not-ready class",
			reducercontract.IsRetryable(err), reducercontract.IsExpectedWait(err), notReady.FailureClass())
	}
	if got := supportsReadSeries(t, reader, telemetry.SupplyChainImpactSupportsReaderSecurityAlertReconciliation, telemetry.SupplyChainImpactSupportsReadNotReady); got != 1 {
		t.Fatalf("not_ready = %d, want 1", got)
	}
}

// TestRequireSupplyChainImpactSupportsReadyProbeFailureKeepsItsOwnClass pins
// that a failing probe is not the not-ready refusal and counts failed.
func TestRequireSupplyChainImpactSupportsReadyProbeFailureKeepsItsOwnClass(t *testing.T) {
	t.Parallel()

	inst, reader := supportsReaderMetrics(t)
	probeErr := errors.New("probe boom")
	err := requireSupplyChainImpactSupportsReady(context.Background(), errReadinessQueryer{err: probeErr}, inst,
		telemetry.SupplyChainImpactSupportsReaderServiceVulnerabilityAdvisory)
	if err == nil || errors.As(err, new(*supplychaincore.SupplyChainImpactSupportsNotReadyError)) {
		t.Fatalf("error = %v, want the probe failure, never the not-ready refusal", err)
	}
	if reducercontract.IsRetryable(err) {
		t.Fatalf("probe failure is retryable; it must keep its own classification")
	}
	if got := supportsReadSeries(t, reader, telemetry.SupplyChainImpactSupportsReaderServiceVulnerabilityAdvisory, telemetry.SupplyChainImpactSupportsReadFailed); got != 1 {
		t.Fatalf("failed = %d, want 1", got)
	}
}

// supportsReadSeries returns the eshu_dp_supply_chain_impact_supports_reads_total
// value for one reader and outcome, 0 when the series was never written.
func supportsReadSeries(t *testing.T, reader *sdkmetric.ManualReader, source, outcome string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_supports_reads_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("reads_total data = %T, want Sum[int64]", m.Data)
			}
			for _, p := range sum.DataPoints {
				gotSource, _ := p.Attributes.Value(attribute.Key("source"))
				gotOutcome, _ := p.Attributes.Value(attribute.Key("outcome"))
				if gotSource.AsString() == source && gotOutcome.AsString() == outcome {
					total += p.Value
				}
			}
		}
	}
	return total
}

// TestSupportsReadersCountServedOnce pins that each reducer loader counts one
// served read per load, under its own reader label.
func TestSupportsReadersCountServedOnce(t *testing.T) {
	t.Parallel()

	t.Run("service vulnerability advisory loader", func(t *testing.T) {
		inst, reader := supportsReaderMetrics(t)
		store := NewFactStore(&fakeExecQueryer{queryResponses: []queueFakeRows{supportsReadyResponse(), {}}})
		store.instruments = inst
		if _, err := store.GetSupplyChainAdvisoriesForRepos(context.Background(), []string{"repo-a"}); err != nil {
			t.Fatalf("GetSupplyChainAdvisoriesForRepos() error = %v, want nil", err)
		}
		if got := supportsReadSeries(t, reader, telemetry.SupplyChainImpactSupportsReaderServiceVulnerabilityAdvisory, telemetry.SupplyChainImpactSupportsReadServed); got != 1 {
			t.Fatalf("served = %d, want 1", got)
		}
	})
	t.Run("security alert reconciliation loader", func(t *testing.T) {
		inst, reader := supportsReaderMetrics(t)
		store := NewFactStore(&fakeExecQueryer{queryResponses: []queueFakeRows{supportsReadyResponse(), {rows: [][]any{{false}}}, {}, {}}})
		store.instruments = inst
		if _, err := store.ListActiveSecurityAlertReconciliationFacts(context.Background(),
			securityalert.SecurityAlertReconciliationFactFilter{RepositoryIDs: []string{"acme/app"}}); err != nil {
			t.Fatalf("ListActiveSecurityAlertReconciliationFacts() error = %v, want nil", err)
		}
		if got := supportsReadSeries(t, reader, telemetry.SupplyChainImpactSupportsReaderSecurityAlertReconciliation, telemetry.SupplyChainImpactSupportsReadServed); got != 1 {
			t.Fatalf("served = %d, want 1", got)
		}
	})
}

// supportsDrainRawRowPoints returns the data points of the drain raw-rows
// histogram for one reader.
func supportsDrainRawRowPoints(t *testing.T, reader *sdkmetric.ManualReader, source string) []metricdata.HistogramDataPoint[int64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var points []metricdata.HistogramDataPoint[int64]
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_supports_drain_raw_rows" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[int64])
			if !ok {
				t.Fatalf("drain_raw_rows data = %T, want Histogram[int64]", m.Data)
			}
			for _, p := range hist.DataPoints {
				if got, _ := p.Attributes.Value(attribute.Key("source")); got.AsString() == source {
					points = append(points, p)
				}
			}
		}
	}
	return points
}

// TestSupportsDrainRecordsRawRowsPerReader pins the hot-key telemetry (ruling
// arbiter-hotkey-page.md decision 9): one drain records one histogram point under
// its reader, carrying the RAW rows it read (here 2 raw rows, 1 active).
func TestSupportsDrainRecordsRawRowsPerReader(t *testing.T) {
	t.Parallel()

	inst, reader := supportsReaderMetrics(t)
	store := NewFactStore(&fakeExecQueryer{queryResponses: []queueFakeRows{
		supportsReadyResponse(),
		{rows: [][]any{
			supportsPageRowForTest("fact-1", false, ""),
			supportsPageRowForTest("fact-2", true, `{"repository_id":"repo-a"}`),
		}},
	}})
	store.instruments = inst
	if _, err := store.GetSupplyChainAdvisoriesForRepos(context.Background(), []string{"repo-a"}); err != nil {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() error = %v, want nil", err)
	}
	points := supportsDrainRawRowPoints(t, reader, telemetry.SupplyChainImpactSupportsReaderServiceVulnerabilityAdvisory)
	if len(points) != 1 || points[0].Count != 1 || points[0].Sum != 2 {
		t.Fatalf("drain raw rows points = %+v, want one point of one drain summing 2 raw rows", points)
	}
}

// TestSupportsDrainRecordsDurationPerReader pins the drain duration histogram
// (ruling arbiter-b2c-wal-repo-arm.md decision 8): one point per drain under its
// reader.
func TestSupportsDrainRecordsDurationPerReader(t *testing.T) {
	t.Parallel()

	inst, reader := supportsReaderMetrics(t)
	store := NewFactStore(&fakeExecQueryer{queryResponses: []queueFakeRows{
		supportsReadyResponse(),
		{rows: [][]any{supportsPageRowForTest("fact-2", true, `{"repository_id":"repo-a"}`)}},
	}})
	store.instruments = inst
	if _, err := store.GetSupplyChainAdvisoriesForRepos(context.Background(), []string{"repo-a"}); err != nil {
		t.Fatalf("GetSupplyChainAdvisoriesForRepos() error = %v, want nil", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	var points int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_supports_drain_duration_seconds" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("drain_duration data = %T, want Histogram[float64]", m.Data)
			}
			for _, p := range hist.DataPoints {
				if got, _ := p.Attributes.Value(attribute.Key("source")); got.AsString() == telemetry.SupplyChainImpactSupportsReaderServiceVulnerabilityAdvisory {
					points += int64(p.Count)
				}
			}
		}
	}
	if points != 1 {
		t.Fatalf("drain duration points = %d, want 1 (one drain)", points)
	}
}
