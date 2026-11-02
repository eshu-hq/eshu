// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"math"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// derivedFactsAuthorizedLoader is a protection-engaged authorized loader. It
// runs the static expansion once, as the real loader does, then returns a
// snapshot whose target carries derived OS-package, analysis and advisory facts.
type derivedFactsAuthorizedLoader struct {
	static *stubSupplyChainImpactFactLoader
}

func (l derivedFactsAuthorizedLoader) ListAuthorizedSupplyChainImpactEvidence(
	ctx context.Context,
	_ reducercontract.Intent,
	staticExpansion AuthorizedStaticSupplyChainImpactExpansion,
) (AuthorizedSupplyChainImpactLoad, error) {
	const (
		digest     = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
		targetID   = "image://derived/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		generation = "generation-a"
	)
	advisory := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	static, err := staticExpansion(ctx, l.static, advisory)
	if err != nil {
		return AuthorizedSupplyChainImpactLoad{}, err
	}
	scan := derivedPartitionScan("first", targetID, generation, digest, "3.0.11-1~deb12u2")
	authority := []AdvisoryAuthorityDependency{{
		Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1",
		AdvisoryScopeID: advisory[0].ScopeID, AdvisoryGenerationID: advisory[0].GenerationID,
		ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian",
		ContentProfile: "supply_chain_impact_v1",
	}}
	return AuthorizedSupplyChainImpactLoad{
		ProtectionEngaged: true,
		StaticEnvelopes:   static.Envelopes,
		Targets: []AuthorizedScanImpactEvidence{{
			ScanTargetID: targetID, ScanGenerationID: generation,
			OSPackages: []facts.Envelope{scan[0]}, Analyses: []facts.Envelope{scan[1]}, Advisories: advisory,
			AuthorityDependencies: authority, Complete: true,
		}},
		ScanPublications:     []ScanImpactPublicationWitness{{ScanTargetID: targetID, ScanGenerationID: generation, Complete: true}},
		SuppressionEnvelopes: static.SuppressionEnvelopes,
		Complete:             true,
	}, nil
}

// TestSupplyChainImpactProtectedLoadDerivedFactsAreNotBudgetEnvelopes pins that
// the headroom ratio reports only what the evidence budget charged. The
// protected-scan path adds derived OS-package, analysis and advisory facts after
// the static load; they are not charged to the budget and cannot make a pass
// partial, so they must not push the ratio past 1 or mark the scope partial
// (#9778).
func TestSupplyChainImpactProtectedLoadDerivedFactsAreNotBudgetEnvelopes(t *testing.T) {
	t.Parallel()

	h := newHeadroomHarness(t)
	loader := expandingImpactLoader()
	loader.activeForFilter = nil
	// The static load charges nothing against a budget of 2, while the target
	// holds at least 3 derived facts.
	handler := SupplyChainImpactHandler{
		FencingTokenIssuer:       newTestImpactFencingTokenIssuer(),
		AuthorizedEvidenceLoader: derivedFactsAuthorizedLoader{static: loader},
		Writer:                   &recordingSupplyChainImpactWriter{},
		Instruments:              h.inst,
		EvidenceBudget:           2,
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-9778-protected",
		ScopeID:      "vuln-intel://osv/npm/protected",
		GenerationID: "generation-9778",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := result.SubSignals["evidence_expansion_envelopes"]; got < 3 {
		t.Fatalf("evidence_expansion_envelopes = %v, want derived facts counted (>= 3) so the test exercises the protected path", got)
	}

	partial, ratio, seen := h.gauges(t)
	if !seen || math.Abs(ratio) > 1e-9 {
		t.Fatalf("budget_ratio = %v (seen %v), want 0: derived facts are not charged to the budget", ratio, seen)
	}
	if got := result.SubSignals["evidence_budget_ratio"]; math.Abs(got) > 1e-9 {
		t.Fatalf("SubSignals[evidence_budget_ratio] = %v, want 0", got)
	}
	for cause, count := range partial {
		if count != 0 {
			t.Fatalf("partial_scopes[%s] = %d for a pass with no truncation, want 0 (%v)", cause, count, partial)
		}
	}
	if got := result.SubSignals["evidence_partial"]; got != 0 {
		t.Fatalf("SubSignals[evidence_partial] = %v, want 0", got)
	}
}
