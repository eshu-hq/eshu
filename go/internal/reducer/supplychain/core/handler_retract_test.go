// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestSupplyChainImpactHandlerReportsRetractedFindings proves the #6831
// retraction is visible to an operator: the writer's FactsRetracted lands on
// eshu_dp_supply_chain_impact_findings_retracted_total (domain label only)
// and on the findings_retracted sub-signal, and a complete (untruncated)
// evidence load is handed to the writer as authoritative.
func TestSupplyChainImpactHandlerReportsRetractedFindings(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	loader := &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0042", 8.4),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0042", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
	}
	writer := &recordingSupplyChainImpactWriter{retracted: 3}
	handler := SupplyChainImpactHandler{
		FactLoader:  loader,
		Writer:      writer,
		Instruments: inst,
		Now:         func() time.Time { return time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC) },
	}

	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-6831",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-6831",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if writer.write.PartialEvidence {
		t.Fatal("PartialEvidence = true for a complete evidence load; superseded findings would never be retracted")
	}
	if got := result.SubSignals["findings_retracted"]; got != 3 {
		t.Fatalf("SubSignals[findings_retracted] = %v, want 3", got)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_findings_retracted_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if dp.Attributes.Len() != 1 {
					t.Fatalf("retraction counter labels = %v, want only domain", dp.Attributes.ToSlice())
				}
				total += dp.Value
			}
		}
	}
	if total != 3 {
		t.Fatalf("eshu_dp_supply_chain_impact_findings_retracted_total = %d, want 3", total)
	}
}
