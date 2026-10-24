// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// statsSupplyChainImpactFactLoader is the #8880 loader surface: it answers
// with per-statement accounting and records whether the plain method ran.
type statsSupplyChainImpactFactLoader struct {
	stubSupplyChainImpactFactLoader
	statsCalls int
	plainCalls int
}

func (s *statsSupplyChainImpactFactLoader) ListActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	s.plainCalls++
	return s.stubSupplyChainImpactFactLoader.ListActiveSupplyChainImpactFacts(ctx, filter)
}

func (s *statsSupplyChainImpactFactLoader) ListActiveSupplyChainImpactFactsWithStats(
	ctx context.Context,
	filter SupplyChainImpactFactFilter,
) ([]facts.Envelope, SupplyChainImpactActiveEvidenceLoadStats, error) {
	s.statsCalls++
	envelopes, truncated, err := s.stubSupplyChainImpactFactLoader.ListActiveSupplyChainImpactFacts(ctx, filter)
	return envelopes, SupplyChainImpactActiveEvidenceLoadStats{
		Branches: []SupplyChainImpactActiveEvidenceBranchStat{
			{Branch: "vulnerability_cve_id", Rows: 3, Duration: 2 * time.Millisecond},
			{Branch: SupplyChainImpactActiveEvidencePayloadPage, Rows: 3, Duration: time.Millisecond},
		},
		ResolvedFactIDs:      3,
		PayloadPages:         1,
		GuardRows:            1,
		SuppressionTruncated: truncated,
	}, err
}

// TestSupplyChainImpactHandlerLogsActiveEvidenceStatementStats pins ruling
// 4.3: a loader that reports statement accounting is preferred, and the
// pass's "reducer execution succeeded" sub-signals and sub-durations carry the
// per-statement rows and seconds, resolved IDs, payload pages, and guard rows.
func TestSupplyChainImpactHandlerLogsActiveEvidenceStatementStats(t *testing.T) {
	t.Parallel()

	loader := &statsSupplyChainImpactFactLoader{stubSupplyChainImpactFactLoader: stubSupplyChainImpactFactLoader{
		scopeFacts: []facts.Envelope{
			vulnerabilityCVEFact("cve-1", "CVE-2026-0042", 8.4),
			vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0042", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
		},
	}}
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         loader,
		Writer:             &recordingSupplyChainImpactWriter{},
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID: "intent-active-evidence-stats", ScopeID: "vuln-intel://osv/npm/example",
		GenerationID: "generation-impact", SourceSystem: "vulnerability_intelligence",
		Domain: reducercontract.DomainSupplyChainImpact, Cause: "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if loader.statsCalls == 0 || loader.plainCalls != 0 {
		t.Fatalf("stats calls = %d, plain calls = %d; want the stats surface preferred", loader.statsCalls, loader.plainCalls)
	}
	loads := float64(loader.statsCalls)
	for key, want := range map[string]float64{
		"active_evidence_loads":                            loads,
		"active_evidence_resolved_fact_ids":                3 * loads,
		"active_evidence_payload_pages":                    loads,
		"active_evidence_guard_rows":                       loads,
		"active_evidence_branch_vulnerability_cve_id_rows": 3 * loads,
		"active_evidence_branch_payload_page_rows":         3 * loads,
	} {
		if got := result.SubSignals[key]; got != want {
			t.Fatalf("SubSignals[%s] = %v, want %v", key, got, want)
		}
	}
	if got, want := result.SubDurations["active_evidence_branch_vulnerability_cve_id"], (2*time.Millisecond).Seconds()*loads; got != want {
		t.Fatalf("SubDurations[active_evidence_branch_vulnerability_cve_id] = %v, want %v", got, want)
	}
}

// TestSupplyChainImpactActiveEvidenceTallyIsNilBudgetSafe pins that a stage
// called without a budget (stage-level tests) records nothing and panics on
// nothing.
func TestSupplyChainImpactActiveEvidenceTallyIsNilBudgetSafe(t *testing.T) {
	t.Parallel()

	var budget *supplyChainImpactEvidenceBudget
	budget.recordActiveEvidence(SupplyChainImpactActiveEvidenceLoadStats{ResolvedFactIDs: 1})
	signals, durations := map[string]float64{}, map[string]float64{}
	supplyChainImpactActiveEvidenceTally{}.mergeInto(signals, durations)
	if len(signals) != 0 || len(durations) != 0 {
		t.Fatalf("an empty tally emitted %v / %v, want nothing", signals, durations)
	}
}
