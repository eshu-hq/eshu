// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite/testutil"
)

// negateRetractionIntent is the one (scope, generation) both passes of the
// composed proof run under, so the second pass sees the first pass's rows as
// its own generation's rows, the case the replace-set retraction covers.
func negateRetractionIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-9696-retract",
		ScopeID:      "vuln-intel://nvd/cve",
		GenerationID: "generation-9696-retract",
		SourceSystem: "vulnerability_intelligence",
		Domain:       reducercontract.DomainSupplyChainImpact,
		Cause:        "vulnerability evidence observed",
	}
}

func negateRetractionFacts(cveID string, logic nvdLogicalContext) []facts.Envelope {
	scopeFacts := []facts.Envelope{
		vulnerabilityCVEFact("cve-retract", cveID, 8.6),
		nvdAffectedProductFactWithLogic("product-retract", cveID, testImpactProductCriteria, testImpactMatchCriteriaID, logic),
	}
	return append(scopeFacts, ownedSBOMProductEvidence()...)
}

// TestNegatedProductsRetractThePreviouslyWrittenFindingThroughTheWriter proves
// 1.7 composes with the retraction path of #9695 end to end through the real
// handler and the real Postgres impact writer (over its transaction fake): a
// CVE whose product match used to produce an affected_derived finding, and
// whose only product match is now negated, writes no finding, and the writer
// retracts the row the earlier pass left behind and reports its CVE as changed
// (#9696).
func TestNegatedProductsRetractThePreviouslyWrittenFindingThroughTheWriter(t *testing.T) {
	t.Parallel()

	const cveID = "CVE-2026-0960"
	plain := nvdLogicalContext{configurationOperator: "OR", nodeOperator: "OR"}
	negated := nvdLogicalContext{configurationOperator: "OR", configurationNegate: true, nodeOperator: "OR"}
	now := func() time.Time { return time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC) }

	// Pass 1: the match is not negated, so the handler derives a product finding.
	first := &recordingSupplyChainImpactWriter{}
	firstIssuer := newTestImpactFencingTokenIssuer()
	firstHandler := SupplyChainImpactHandler{
		FencingTokenIssuer: firstIssuer,
		FactLoader:         &stubSupplyChainImpactFactLoader{scopeFacts: negateRetractionFacts(cveID, plain)},
		Writer:             first,
		Now:                now,
	}
	if _, err := firstHandler.Handle(context.Background(), negateRetractionIntent()); err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	if got := len(first.write.Findings); got != 1 {
		t.Fatalf("first pass findings = %d, want 1: %#v", got, first.write.Findings)
	}
	priorFinding := first.write.Findings[0]
	assertSupplyChainImpactStatus(t, priorFinding, SupplyChainImpactAffectedDerived)

	// Pass 2: the same CVE, its only product match now negated, through the real
	// writer. The store still holds the row pass 1 wrote, stamped with pass 1's
	// fencing token.
	beginner := newFakeImpactBeginner(&testutil.FakeExecer{})
	prior := storedLike(first.write, priorFinding)
	prior.fencingToken = first.write.FencingToken
	beginner.state.stored = []fakeStoredFinding{prior}
	// The fake store does not run SQL; tell it the retraction tombstoned the one
	// stale row, as Postgres would for the NOT IN keep-set statement.
	beginner.state.rowsAffected = map[string]int64{retractSupersededSupplyChainImpactFindingsQuery: 1}
	secondIssuer := newTestImpactFencingTokenIssuer()
	secondIssuer.next.Store(first.write.FencingToken)
	secondHandler := SupplyChainImpactHandler{
		FencingTokenIssuer: secondIssuer,
		FactLoader:         &stubSupplyChainImpactFactLoader{scopeFacts: negateRetractionFacts(cveID, negated)},
		Writer:             PostgresSupplyChainImpactWriter{DB: beginner, Now: now},
		Now:                now,
	}

	result, err := secondHandler.Handle(context.Background(), negateRetractionIntent())
	if err != nil {
		t.Fatalf("second Handle() error = %v", err)
	}

	if got := result.SubSignals["negated_affected_products_excluded"]; got != 1 {
		t.Fatalf("SubSignals[negated_affected_products_excluded] = %v, want 1", got)
	}
	if got := result.SubSignals["findings_retracted"]; got != 1 {
		t.Fatalf("SubSignals[findings_retracted] = %v, want 1 (the pass-1 row)", got)
	}
	if got := result.CanonicalWrites; got != 0 {
		t.Fatalf("CanonicalWrites = %d, want 0: a CVE with only negated matches writes no finding", got)
	}

	// The retraction statement ran with a keep set that does not hold the stale
	// row, so the writer tombstones it.
	var retractions [][]string
	for _, call := range beginner.state.all {
		if call.Query != retractSupersededSupplyChainImpactFindingsQuery {
			continue
		}
		keep, ok := call.Args[3].([]string)
		if !ok {
			t.Fatalf("retraction keep-set arg = %T, want []string", call.Args[3])
		}
		retractions = append(retractions, keep)
	}
	if len(retractions) != 1 {
		t.Fatalf("retraction statements = %d, want 1", len(retractions))
	}
	if slices.Contains(retractions[0], prior.factID) {
		t.Fatalf("keep set %v still holds the pass-1 finding %q; the writer would not retract it", retractions[0], prior.factID)
	}
	if beginner.state.commits != 1 || beginner.state.rollbacks != 0 {
		t.Fatalf("commits/rollbacks = %d/%d, want 1/0", beginner.state.commits, beginner.state.rollbacks)
	}
}
