// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// expandingImpactLoader returns a loader whose active-evidence reads keep
// finding one new package per round, so the expansion never settles by itself:
// only the round cap or the evidence budget can stop it.
func expandingImpactLoader() *stubSupplyChainImpactFactLoader {
	calls := 0
	return &stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-7154", 8.1),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-7154", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
		activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
			if len(filter.PackageIDs) == 0 {
				return nil
			}
			calls++
			return []facts.Envelope{
				packageRegistryPackageImpactFact(
					"package-expansion-"+string(rune('a'+calls)),
					"pkg:npm/expansion-"+string(rune('a'+calls)),
				),
			}
		},
	}
}

type truncationHarness struct {
	writer *recordingSupplyChainImpactWriter
	result reducercontract.Result
	reader *sdkmetric.ManualReader
	logs   *bytes.Buffer
}

func runTruncationPass(t *testing.T, loader *stubSupplyChainImpactFactLoader, budget int) truncationHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	logs := &bytes.Buffer{}
	writer := &recordingSupplyChainImpactWriter{}
	handler := SupplyChainImpactHandler{
		FactLoader:     loader,
		Writer:         writer,
		Instruments:    inst,
		Logger:         slog.New(slog.NewJSONHandler(logs, nil)),
		EvidenceBudget: budget,
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-7154",
		ScopeID:      "vuln-intel://osv/npm/example",
		GenerationID: "generation-7154",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return truncationHarness{writer: writer, result: result, reader: reader, logs: logs}
}

// truncatedCounter returns eshu_dp_supply_chain_impact_evidence_truncated_total
// by its reason label and fails if a data point carries any label but domain
// and reason.
func (h truncationHarness) truncatedCounter(t *testing.T) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	byReason := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != "eshu_dp_supply_chain_impact_evidence_truncated_total" {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if dp.Attributes.Len() != 2 {
					t.Fatalf("truncation counter labels = %v, want only domain and reason", dp.Attributes.ToSlice())
				}
				reason, _ := dp.Attributes.Value("reason")
				byReason[reason.AsString()] += dp.Value
			}
		}
	}
	return byReason
}

func (h truncationHarness) warnLines(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		if entry["level"] == "WARN" {
			out = append(out, entry)
		}
	}
	return out
}

// TestSupplyChainImpactRoundCapSkipsRetractionAndSignals pins #7154: the
// 8-round expansion cap stays an identity-affecting truncation (evidence
// reachable only through later rounds was never loaded), so the pass must not
// retract, and an operator must be able to see which scope is stuck.
func TestSupplyChainImpactRoundCapSkipsRetractionAndSignals(t *testing.T) {
	t.Parallel()

	h := runTruncationPass(t, expandingImpactLoader(), 0)
	if !h.writer.write.PartialEvidence {
		t.Fatal("PartialEvidence = false after the round cap; the pass would retract findings it never reached")
	}
	if got := h.result.SubSignals["evidence_truncated_rounds"]; got != 1 {
		t.Fatalf("SubSignals[evidence_truncated_rounds] = %v, want 1", got)
	}
	if got := h.result.SubSignals["evidence_truncated_budget"]; got != 0 {
		t.Fatalf("SubSignals[evidence_truncated_budget] = %v, want 0", got)
	}
	counter := h.truncatedCounter(t)
	if counter["active_expansion_rounds"] != 1 || len(counter) != 1 {
		t.Fatalf("truncation counter = %v, want exactly one active_expansion_rounds", counter)
	}
	warns := h.warnLines(t)
	if len(warns) != 1 {
		t.Fatalf("WARN lines = %d, want 1: %s", len(warns), h.logs.String())
	}
	for key, want := range map[string]any{
		"cause":         "active_expansion_rounds",
		"scope_id":      "vuln-intel://osv/npm/example",
		"generation_id": "generation-7154",
		"intent_id":     "intent-7154",
	} {
		if warns[0][key] != want {
			t.Fatalf("WARN %s = %v, want %v", key, warns[0][key], want)
		}
	}
}

// TestSupplyChainImpactEvidenceBudgetSkipsRetractionAndSignals pins #7154: the
// per-intent evidence budget is the one remaining valve, and spending it makes
// the pass partial with its own cause.
func TestSupplyChainImpactEvidenceBudgetSkipsRetractionAndSignals(t *testing.T) {
	t.Parallel()

	h := runTruncationPass(t, expandingImpactLoader(), 3)
	if !h.writer.write.PartialEvidence {
		t.Fatal("PartialEvidence = false after the evidence budget was spent")
	}
	if got := h.result.SubSignals["evidence_truncated_budget"]; got != 1 {
		t.Fatalf("SubSignals[evidence_truncated_budget] = %v, want 1", got)
	}
	if got := h.result.SubSignals["evidence_truncated_rounds"]; got != 0 {
		t.Fatalf("SubSignals[evidence_truncated_rounds] = %v, want 0 (the budget stopped it first)", got)
	}
	if got := h.result.SubSignals["evidence_expansion_envelopes"]; got != 4 {
		t.Fatalf("SubSignals[evidence_expansion_envelopes] = %v, want 4 (the load that crossed the limit is kept)", got)
	}
	counter := h.truncatedCounter(t)
	if counter["evidence_budget"] != 1 || len(counter) != 1 {
		t.Fatalf("truncation counter = %v, want exactly one evidence_budget", counter)
	}
	if warns := h.warnLines(t); len(warns) != 1 || warns[0]["cause"] != "evidence_budget" {
		t.Fatalf("WARN lines = %v, want one evidence_budget line", warns)
	}
}

// TestSupplyChainImpactSuppressionTailIsNotEvidenceTruncation pins that a
// suppression-tail-only truncation stays out of the operator counter and the
// WARN log: it does not stop retraction, so a scope showing it is not stuck.
func TestSupplyChainImpactSuppressionTailIsNotEvidenceTruncation(t *testing.T) {
	t.Parallel()

	loader := expandingImpactLoader()
	loader.activeForFilter = nil
	loader.activeTruncated = true
	h := runTruncationPass(t, loader, 0)
	if h.writer.write.PartialEvidence {
		t.Fatal("PartialEvidence = true for a suppression-tail truncation")
	}
	if counter := h.truncatedCounter(t); len(counter) != 0 {
		t.Fatalf("truncation counter = %v, want none for a suppression-tail-only truncation", counter)
	}
	if warns := h.warnLines(t); len(warns) != 0 {
		t.Fatalf("WARN lines = %v, want none", warns)
	}
}
