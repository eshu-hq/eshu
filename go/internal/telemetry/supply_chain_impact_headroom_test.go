// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSupplyChainImpactHeadroomTrackerCountsPartialScopesByCause(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", []string{SupplyChainImpactPartialCauseBudget}, 120_000, 100_000, now)
	tracker.RecordPass("scope-b", []string{SupplyChainImpactPartialCauseBudget, SupplyChainImpactPartialCauseRounds}, 120_000, 100_000, now)
	tracker.RecordPass("scope-c", nil, 10, 100_000, now)

	got := tracker.PartialByCause()
	if got[SupplyChainImpactPartialCauseBudget] != 2 || got[SupplyChainImpactPartialCauseRounds] != 1 {
		t.Fatalf("PartialByCause() = %v, want budget=2 rounds=1 (scope-c was complete)", got)
	}
	if got[SupplyChainImpactPartialCauseOther] != 0 {
		t.Fatalf("PartialByCause() = %v, want other=0", got)
	}
}

func TestSupplyChainImpactHeadroomTrackerCompletePassClearsTheScope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", []string{SupplyChainImpactPartialCauseBudget}, 120_000, 100_000, now)
	tracker.RecordPass("scope-a", nil, 5_000, 100_000, now.Add(time.Minute))

	if got := tracker.PartialByCause()[SupplyChainImpactPartialCauseBudget]; got != 0 {
		t.Fatalf("budget-partial scopes = %d after a complete pass, want 0 (latest pass wins)", got)
	}
}

func TestSupplyChainImpactHeadroomTrackerLatestPassReplacesTheCauseSet(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", []string{SupplyChainImpactPartialCauseRounds}, 10, 100_000, now)
	tracker.RecordPass("scope-a", []string{SupplyChainImpactPartialCauseBudget}, 120_000, 100_000, now.Add(time.Minute))

	got := tracker.PartialByCause()
	if got[SupplyChainImpactPartialCauseRounds] != 0 || got[SupplyChainImpactPartialCauseBudget] != 1 {
		t.Fatalf("PartialByCause() = %v, want only the latest pass's cause", got)
	}
}

func TestSupplyChainImpactHeadroomTrackerFoldsUnknownCausesIntoOther(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", []string{"made_up_cause"}, 1, 100_000, now)

	got := tracker.PartialByCause()
	if got[SupplyChainImpactPartialCauseOther] != 1 || len(got) != 3 {
		t.Fatalf("PartialByCause() = %v, want the unknown cause folded into %q", got, SupplyChainImpactPartialCauseOther)
	}
}

func TestSupplyChainImpactHeadroomTrackerWidestRatioIsWindowed(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	if _, ok := tracker.WidestBudgetRatio(start); ok {
		t.Fatal("WidestBudgetRatio() reported a value before any pass")
	}
	tracker.RecordPass("scope-a", nil, 32_984, 100_000, start)
	tracker.RecordPass("scope-b", nil, 892, 100_000, start.Add(10*time.Minute))

	ratio, ok := tracker.WidestBudgetRatio(start.Add(20 * time.Minute))
	if !ok || ratio < 0.3298 || ratio > 0.3299 {
		t.Fatalf("WidestBudgetRatio() = %v, %v; want 0.32984 (the widest pass in the window)", ratio, ok)
	}

	ratio, ok = tracker.WidestBudgetRatio(start.Add(SupplyChainImpactHeadroomWindow + 5*time.Minute))
	if !ok || ratio > 0.01 {
		t.Fatalf("WidestBudgetRatio() = %v, %v; want the 0.00892 pass once the widest aged out", ratio, ok)
	}

	if _, ok = tracker.WidestBudgetRatio(start.Add(2*SupplyChainImpactHeadroomWindow + time.Minute)); ok {
		t.Fatal("WidestBudgetRatio() still reported a value after every pass aged out")
	}
}

func TestSupplyChainImpactHeadroomTrackerRatioPassesOneWhenBudgetIsExceeded(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", []string{SupplyChainImpactPartialCauseBudget}, 150_000, 100_000, now)
	ratio, ok := tracker.WidestBudgetRatio(now)
	if !ok || ratio != 1.5 {
		t.Fatalf("WidestBudgetRatio() = %v, %v; want 1.5, because the load that crosses the limit is kept", ratio, ok)
	}
}

func TestSupplyChainImpactHeadroomTrackerIgnoresAnUnknownBudget(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("scope-a", nil, 500, 0, now)
	if _, ok := tracker.WidestBudgetRatio(now); ok {
		t.Fatal("a pass with no positive budget produced a ratio")
	}
}

func TestSupplyChainImpactHeadroomTrackerNilAndBlankAreSafe(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var nilTracker *SupplyChainImpactHeadroomTracker
	nilTracker.RecordPass("scope-a", []string{SupplyChainImpactPartialCauseBudget}, 1, 1, now)
	if got := nilTracker.PartialByCause(); len(got) != 0 {
		t.Fatalf("nil PartialByCause() = %v, want empty", got)
	}
	if _, ok := nilTracker.WidestBudgetRatio(now); ok {
		t.Fatal("nil WidestBudgetRatio() reported a value")
	}

	tracker := NewSupplyChainImpactHeadroomTracker()
	tracker.RecordPass("  ", []string{SupplyChainImpactPartialCauseBudget}, 1, 10, now)
	if got := tracker.PartialByCause()[SupplyChainImpactPartialCauseBudget]; got != 0 {
		t.Fatalf("a blank scope was counted as partial: %d", got)
	}
	if ratio, ok := tracker.WidestBudgetRatio(now); !ok || ratio != 0.1 {
		t.Fatalf("a blank scope's pass must still feed the ratio: %v, %v", ratio, ok)
	}
}

func TestSupplyChainImpactHeadroomTrackerIsRaceFree(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	tracker := NewSupplyChainImpactHeadroomTracker()
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scopeID := fmt.Sprintf("scope-%d", worker%4)
			for i := 0; i < 200; i++ {
				var causes []string
				if i%2 == 0 {
					causes = []string{SupplyChainImpactPartialCauseBudget}
				}
				tracker.RecordPass(scopeID, causes, i*100, 100_000, now.Add(time.Duration(i)*time.Second))
				_ = tracker.PartialByCause()
				_, _ = tracker.WidestBudgetRatio(now)
			}
		}()
	}
	wg.Wait()
}

func TestInstrumentsReportSupplyChainImpactHeadroomGauges(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	inst, err := NewInstruments(meter)
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	now := time.Now()
	inst.SupplyChainImpactHeadroom.RecordPass("scope-a", []string{SupplyChainImpactPartialCauseBudget}, 120_000, 100_000, now)
	inst.SupplyChainImpactHeadroom.RecordPass("scope-b", nil, 32_984, 100_000, now)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	partial := map[string]int64{}
	var ratio float64
	var sawPartial, sawRatio bool
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			switch m.Name {
			case "eshu_dp_supply_chain_impact_partial_scopes":
				sawPartial = true
				gauge, ok := m.Data.(metricdata.Gauge[int64])
				if !ok {
					t.Fatalf("%s data = %T, want Gauge[int64]", m.Name, m.Data)
				}
				for _, point := range gauge.DataPoints {
					if point.Attributes.Len() != 1 {
						t.Fatalf("partial_scopes labels = %v, want only cause", point.Attributes.ToSlice())
					}
					cause, _ := point.Attributes.Value("cause")
					partial[cause.AsString()] = point.Value
				}
			case "eshu_dp_supply_chain_impact_evidence_budget_ratio":
				sawRatio = true
				gauge, ok := m.Data.(metricdata.Gauge[float64])
				if !ok {
					t.Fatalf("%s data = %T, want Gauge[float64]", m.Name, m.Data)
				}
				if len(gauge.DataPoints) != 1 || gauge.DataPoints[0].Attributes.Len() != 0 {
					t.Fatalf("budget_ratio data points = %v, want one unlabeled point", gauge.DataPoints)
				}
				ratio = gauge.DataPoints[0].Value
			}
		}
	}
	if !sawPartial || !sawRatio {
		t.Fatalf("partial_scopes collected=%v budget_ratio collected=%v; want both", sawPartial, sawRatio)
	}
	if partial[SupplyChainImpactPartialCauseBudget] != 1 || partial[SupplyChainImpactPartialCauseRounds] != 0 {
		t.Fatalf("partial_scopes = %v, want budget=1 and an explicit rounds=0 series", partial)
	}
	if ratio != 1.2 {
		t.Fatalf("budget_ratio = %v, want 1.2 (the widest pass, 120000 over 100000)", ratio)
	}
}

func TestInstrumentsOmitBudgetRatioBeforeAnyPass(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test")
	if _, err := NewInstruments(meter); err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scopeMetrics := range collected.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			if m.Name == "eshu_dp_supply_chain_impact_evidence_budget_ratio" {
				if gauge, ok := m.Data.(metricdata.Gauge[float64]); ok && len(gauge.DataPoints) != 0 {
					t.Fatalf("budget_ratio reported %v before any pass; a fake 0 would read as headroom", gauge.DataPoints)
				}
			}
		}
	}
}
