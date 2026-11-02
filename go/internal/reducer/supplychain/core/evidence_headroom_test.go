// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"math"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type headroomHarness struct {
	inst   *telemetry.Instruments
	reader *sdkmetric.ManualReader
}

func newHeadroomHarness(t *testing.T) headroomHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return headroomHarness{inst: inst, reader: reader}
}

// pass runs one supply_chain_impact pass for scopeID against the shared
// instruments, as one reducer process would.
func (h headroomHarness) pass(t *testing.T, scopeID string, loader *stubSupplyChainImpactFactLoader, budget int) reducercontract.Result {
	t.Helper()
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         loader,
		Writer:             &recordingSupplyChainImpactWriter{},
		Instruments:        h.inst,
		EvidenceBudget:     budget,
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-9778",
		ScopeID:      scopeID,
		GenerationID: "generation-9778",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return result
}

// gauges reads the two #9778 gauges. ratioSeen is false when the ratio gauge
// reported no sample.
func (h headroomHarness) gauges(t *testing.T) (partial map[string]int64, ratio float64, ratioSeen bool) {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	partial = map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			switch m.Name {
			case "eshu_dp_supply_chain_impact_partial_scopes":
				for _, dp := range m.Data.(metricdata.Gauge[int64]).DataPoints {
					cause, _ := dp.Attributes.Value("cause")
					partial[cause.AsString()] = dp.Value
				}
			case "eshu_dp_supply_chain_impact_evidence_budget_ratio":
				for _, dp := range m.Data.(metricdata.Gauge[float64]).DataPoints {
					ratio, ratioSeen = dp.Value, true
				}
			}
		}
	}
	return partial, ratio, ratioSeen
}

// TestSupplyChainImpactBudgetTruncatedPassRaisesHeadroomGauges plants a pass
// that spends the evidence budget and pins that an operator can read it from
// the gauges (#9778): the scope counts as partial for evidence_budget, and the
// budget ratio crosses 1. The counter cannot answer either question.
func TestSupplyChainImpactBudgetTruncatedPassRaisesHeadroomGauges(t *testing.T) {
	t.Parallel()

	h := newHeadroomHarness(t)
	result := h.pass(t, "vuln-intel://osv/npm/stuck", expandingImpactLoader(), 3)

	partial, ratio, seen := h.gauges(t)
	if partial[telemetry.SupplyChainImpactPartialCauseBudget] != 1 || partial[telemetry.SupplyChainImpactPartialCauseRounds] != 0 {
		t.Fatalf("partial_scopes = %v, want evidence_budget=1 and active_expansion_rounds=0", partial)
	}
	// The pass loaded 4 expansion envelopes against a budget of 3: the load that
	// crosses the limit is kept, so the ratio is above 1.
	if !seen || math.Abs(ratio-4.0/3.0) > 1e-9 {
		t.Fatalf("budget_ratio = %v (seen %v), want 4/3", ratio, seen)
	}
	if got := result.SubSignals["evidence_partial"]; got != 1 {
		t.Fatalf("SubSignals[evidence_partial] = %v, want 1", got)
	}
	if got := result.SubSignals["evidence_budget_ratio"]; math.Abs(got-4.0/3.0) > 1e-9 {
		t.Fatalf("SubSignals[evidence_budget_ratio] = %v, want 4/3", got)
	}
}

// TestSupplyChainImpactRoundCappedPassCountsUnderItsOwnCause pins that the two
// partial causes stay apart in the gauge.
func TestSupplyChainImpactRoundCappedPassCountsUnderItsOwnCause(t *testing.T) {
	t.Parallel()

	h := newHeadroomHarness(t)
	h.pass(t, "vuln-intel://osv/npm/rounds", expandingImpactLoader(), 0)

	partial, _, _ := h.gauges(t)
	if partial[telemetry.SupplyChainImpactPartialCauseRounds] != 1 || partial[telemetry.SupplyChainImpactPartialCauseBudget] != 0 {
		t.Fatalf("partial_scopes = %v, want active_expansion_rounds=1 and evidence_budget=0", partial)
	}
}

// TestSupplyChainImpactCompletePassReleasesThePartialScope pins that the gauge
// answers "stuck right now": a later complete pass of the same scope clears it.
func TestSupplyChainImpactCompletePassReleasesThePartialScope(t *testing.T) {
	t.Parallel()

	h := newHeadroomHarness(t)
	const scope = "vuln-intel://osv/npm/recovers"
	h.pass(t, scope, expandingImpactLoader(), 3)

	complete := expandingImpactLoader()
	complete.activeForFilter = nil
	result := h.pass(t, scope, complete, 0)
	if got := result.SubSignals["evidence_partial"]; got != 0 {
		t.Fatalf("SubSignals[evidence_partial] = %v on a complete pass, want 0", got)
	}

	partial, _, _ := h.gauges(t)
	if partial[telemetry.SupplyChainImpactPartialCauseBudget] != 0 || partial[telemetry.SupplyChainImpactPartialCauseRounds] != 0 {
		t.Fatalf("partial_scopes = %v after a complete pass, want all zero", partial)
	}
}

// TestSupplyChainImpactSuppressionTailPassIsNotPartialForTheGauge pins that the
// gauge follows the retraction contract: a suppression-tail truncation does not
// stop retraction, so the scope is not stuck.
func TestSupplyChainImpactSuppressionTailPassIsNotPartialForTheGauge(t *testing.T) {
	t.Parallel()

	h := newHeadroomHarness(t)
	loader := expandingImpactLoader()
	loader.activeForFilter = nil
	loader.activeTruncated = true
	h.pass(t, "vuln-intel://osv/npm/tail", loader, 0)

	partial, _, _ := h.gauges(t)
	for cause, count := range partial {
		if count != 0 {
			t.Fatalf("partial_scopes[%s] = %d for a suppression-tail-only truncation, want 0", cause, count)
		}
	}
}
