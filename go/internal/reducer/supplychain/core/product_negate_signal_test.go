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

func handleSupplyChainImpactForSignal(t *testing.T, scopeFacts []facts.Envelope) reducercontract.Result {
	t.Helper()

	handler := SupplyChainImpactHandler{
		FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
		FactLoader:         &stubSupplyChainImpactFactLoader{scopeFacts: scopeFacts},
		Writer:             &recordingSupplyChainImpactWriter{},
		Now:                func() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) },
	}
	result, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-9696",
		ScopeID:      "vuln-intel://nvd/cve",
		GenerationID: "generation-9696",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	return result
}

func TestSupplyChainImpactHandlerReportsNegatedProductsExcluded(t *testing.T) {
	t.Parallel()

	negated := nvdLogicalContext{configurationOperator: "OR", configurationNegate: true, nodeOperator: "OR"}
	plain := nvdLogicalContext{configurationOperator: "OR", nodeOperator: "OR"}

	for _, tc := range []struct {
		name  string
		logic []nvdLogicalContext
		want  float64
	}{
		{name: "two negated matches", logic: []nvdLogicalContext{negated, negated}, want: 2},
		{name: "one negated among plain", logic: []nvdLogicalContext{plain, negated, plain}, want: 1},
		{name: "no negated match reports zero", logic: []nvdLogicalContext{plain, plain}, want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			scopeFacts := []facts.Envelope{vulnerabilityCVEFact("cve-1", "CVE-2026-0950", 8.6)}
			for i, logic := range tc.logic {
				scopeFacts = append(scopeFacts, nvdAffectedProductFactWithLogic(
					"product-"+string(rune('a'+i)), "CVE-2026-0950",
					"cpe:2.3:a:example:server"+string(rune('a'+i))+":1.0:*:*:*:*:*:*:*",
					"match-"+string(rune('a'+i)), logic))
			}

			result := handleSupplyChainImpactForSignal(t, scopeFacts)

			// The literal is the operator-facing contract; do not route it through the constant.
			got, ok := result.SubSignals["negated_affected_products_excluded"]
			if !ok {
				t.Fatal("SubSignals[\"negated_affected_products_excluded\"] missing; an operator cannot tell a clean pass from an unreported one")
			}
			if got != tc.want {
				t.Fatalf("SubSignals[\"negated_affected_products_excluded\"] = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildAuthorizedImpactFindingsKeepsNegatedExclusionCount proves the count
// survives the authorized-scan wrapper on both of its paths: the static-only
// return and the return after authorized scan findings are built (#9696).
func TestBuildAuthorizedImpactFindingsKeepsNegatedExclusionCount(t *testing.T) {
	t.Parallel()

	negated := nvdLogicalContext{configurationOperator: "OR", configurationNegate: true, nodeOperator: "OR"}
	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-1", "CVE-2026-0951", 8.6),
		nvdAffectedProductFactWithLogic("product-a", "CVE-2026-0951", testImpactProductCriteria, testImpactMatchCriteriaID, negated),
	}

	for _, tc := range []struct {
		name       string
		authorized *AuthorizedSupplyChainImpactLoad
	}{
		{name: "static only", authorized: nil},
		{name: "authorized scan path", authorized: &AuthorizedSupplyChainImpactLoad{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, _, excluded, err := buildAuthorizedSupplyChainImpactFindings(supplyChainImpactLoadedEvidence{
				envelopes:    envelopes,
				asAuthorized: tc.authorized,
			})
			if err != nil {
				t.Fatalf("buildAuthorizedSupplyChainImpactFindings() error = %v", err)
			}
			if excluded != 1 {
				t.Fatalf("negated products excluded = %d, want 1", excluded)
			}
		})
	}
}
