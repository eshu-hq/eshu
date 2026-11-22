// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type sweepSnapshotSource struct {
	snap SupplyChainImpactWinnersStagedAuditSnapshot
}

func (s sweepSnapshotSource) StagedAuditSnapshot() SupplyChainImpactWinnersStagedAuditSnapshot {
	return s.snap
}

// collectSweepPoints returns metric name -> one observed value (int or float).
func collectSweepPoints(t *testing.T, src SupplyChainImpactWinnersStagedAuditSource) map[string]float64 {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	if err := RegisterSupplyChainImpactWinnersStagedAuditInstruments(provider.Meter("test"), src); err != nil {
		t.Fatalf("register: %v", err)
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	out := map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				for _, dp := range d.DataPoints {
					out[m.Name] += float64(dp.Value)
				}
			case metricdata.Gauge[int64]:
				for _, dp := range d.DataPoints {
					out[m.Name] = float64(dp.Value)
				}
			case metricdata.Gauge[float64]:
				for _, dp := range d.DataPoints {
					out[m.Name] = dp.Value
				}
			}
		}
	}
	return out
}

// TestSupplyChainImpactWinnersAuditSweepInstruments pins the #11131 sweep
// signals: counters always report, the position and last-completion gauges
// appear once the persisted row was seen, and the completion gauges stay
// absent until a sweep has completed.
func TestSupplyChainImpactWinnersAuditSweepInstruments(t *testing.T) {
	t.Parallel()
	got := collectSweepPoints(t, sweepSnapshotSource{SupplyChainImpactWinnersStagedAuditSnapshot{Deferrals: map[string]int64{"lock_busy": 2}}})
	if got["eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_total"] != 0 || got["eshu_dp_supply_chain_impact_winners_staged_audit_deferrals_total"] != 2 {
		t.Fatalf("empty snapshot points = %v", got)
	}
	if _, ok := got["eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done"]; ok {
		t.Fatal("ranges_done reported before the persisted row was seen")
	}
	snap := SupplyChainImpactWinnersStagedAuditSnapshot{
		Deferrals: map[string]int64{}, Ranges: 7, RangeRepairs: 3, LastRange: 2 * time.Second, LastRangeBudget: 5000,
		SweepSeen: true, SweepRangesDone: 4,
	}
	got = collectSweepPoints(t, sweepSnapshotSource{snap})
	if got["eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_total"] != 7 ||
		got["eshu_dp_supply_chain_impact_winners_audit_sweep_repairs_total"] != 3 ||
		got["eshu_dp_supply_chain_impact_winners_audit_sweep_ranges_done"] != 4 ||
		got["eshu_dp_supply_chain_impact_winners_audit_sweep_last_range_budget"] != 5000 {
		t.Fatalf("mid-sweep points = %v", got)
	}
	if _, ok := got["eshu_dp_supply_chain_impact_winners_audit_sweep_last_completed_age_seconds"]; ok {
		t.Fatal("completion age reported before any sweep completed")
	}
	snap.LastSweepCompletedAt, snap.LastSweepRanges = time.Now().Add(-time.Minute), 120
	got = collectSweepPoints(t, sweepSnapshotSource{snap})
	if age := got["eshu_dp_supply_chain_impact_winners_audit_sweep_last_completed_age_seconds"]; age < 59 || age > 120 {
		t.Fatalf("completion age = %v, want about 60 s", age)
	}
	if got["eshu_dp_supply_chain_impact_winners_audit_sweep_last_ranges"] != 120 {
		t.Fatalf("last ranges = %v", got["eshu_dp_supply_chain_impact_winners_audit_sweep_last_ranges"])
	}
}
