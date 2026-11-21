// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// winnersGaugeValues returns every data point of the named float64 gauge.
func winnersGaugeValues(t *testing.T, collect func(*metricdata.ResourceMetrics) error, name string) []float64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := collect(&rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	var out []float64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			if g, ok := m.Data.(metricdata.Gauge[float64]); ok {
				for _, dp := range g.DataPoints {
					out = append(out, dp.Value)
				}
			}
		}
	}
	return out
}

// TestWinnersAuditReportsEveryCompletedPass pins the #11117 audit signals
// (arbiter addendum item 5c): a completed pass, including one that found
// nothing, logs one line and starts the audit last-success age gauge, so an
// operator can tell an audit that runs clean from one that never runs.
func TestWinnersAuditReportsEveryCompletedPass(t *testing.T) {
	t.Parallel()

	inst, reader := winnersSignalsHarness(t)
	collect := func(rm *metricdata.ResourceMetrics) error { return reader.Collect(context.Background(), rm) }
	const ageGauge = "eshu_dp_supply_chain_impact_winners_audit_last_success_age_seconds"
	if got := winnersGaugeValues(t, collect, ageGauge); len(got) != 0 {
		t.Fatalf("audit age before any pass = %v, want no value", got)
	}
	var logs bytes.Buffer
	m := SupplyChainImpactWinnersMaintainer{
		Rebuilder: &fakeWinnersRebuilder{}, LeaseManager: &fakeWinnersLeaseManager{claim: true},
		Auditor: &fakeWinnersAuditor{}, Instruments: inst,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	}
	if err := m.runAuditOnce(context.Background()); err != nil {
		t.Fatalf("runAuditOnce() error = %v", err)
	}
	if n := strings.Count(logs.String(), "supply chain impact winners audit completed"); n != 1 {
		t.Fatalf("completion log lines = %d for a zero-repair pass, want 1:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "repairs=0") {
		t.Fatalf("completion log %q lacks repairs=0", logs.String())
	}
	got := winnersGaugeValues(t, collect, ageGauge)
	if len(got) != 1 || got[0] < 0 || got[0] > 60 {
		t.Fatalf("audit age after a pass = %v, want one sample of a few seconds", got)
	}
}
