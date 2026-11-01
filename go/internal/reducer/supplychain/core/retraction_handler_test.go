// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestHandlerPublishesRetractedFindingAndReportsItToOperators proves the whole
// reducer pass for a rejected advisory (#9695): the writer receives the finding
// in the advisory_retracted state with its explanation, and an operator can see
// the retraction on the evidence summary, the sub-signal, and the suppression
// decision counter without opening a payload.
func TestHandlerPublishesRetractedFindingAndReportsItToOperators(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	scopeFacts := append(
		[]facts.Envelope{retractionCVEFact("nvd-cve", "CVE-2026-3001", "nvd", "CVE-2026-3001", "Rejected", "CVE-2026-3002")},
		retractionPackageEvidence("CVE-2026-3001", "nvd", "CVE-2026-3001")...,
	)
	// A second, live CVE in the same pass proves the count is per finding and
	// the live finding stays active.
	scopeFacts = append(scopeFacts,
		vulnerabilityCVEFact("cve-live", "CVE-2026-3003", 8.0),
		vulnerabilityAffectedPackageFact("affected-live", "CVE-2026-3003", "pkg:npm/live", "npm", "live", "1.0.0", "1.0.1"),
		packageConsumptionFactWithRange("consume-live", "pkg:npm/live", testImpactRepositoryID, "1.0.0"),
	)
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         &stubSupplyChainImpactFactLoader{scopeFacts: scopeFacts},
		Writer:             writer,
		Instruments:        inst,
		Now:                func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) },
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-9695",
		ScopeID:      "vuln-intel://nvd/cve",
		GenerationID: "generation-9695",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}

	states := map[string]SupplyChainSuppressionState{}
	for _, finding := range writer.write.Findings {
		states[finding.CVEID] = finding.Suppression.State
	}
	if states["CVE-2026-3001"] != SupplyChainSuppressionStateAdvisoryRetracted {
		t.Fatalf("rejected finding state = %q, want advisory_retracted: %v", states["CVE-2026-3001"], states)
	}
	if states["CVE-2026-3003"] != SupplyChainSuppressionStateActive {
		t.Fatalf("live finding state = %q, want active: %v", states["CVE-2026-3003"], states)
	}
	if got := result.SubSignals["advisory_retracted_findings"]; got != 1 {
		t.Fatalf("SubSignals[advisory_retracted_findings] = %v, want 1", got)
	}
	if !strings.Contains(result.EvidenceSummary, "suppression_advisory_retracted=1") {
		t.Fatalf("EvidenceSummary = %q, want suppression_advisory_retracted=1", result.EvidenceSummary)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var retracted int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_suppression_decisions_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if outcome, _ := dp.Attributes.Value("outcome"); outcome.AsString() == "advisory_retracted" {
					retracted += dp.Value
				}
			}
		}
	}
	if retracted != 1 {
		t.Fatalf("suppression decision counter outcome=advisory_retracted = %d, want 1", retracted)
	}
}
