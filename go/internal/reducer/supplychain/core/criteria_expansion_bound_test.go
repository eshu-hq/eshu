// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// TestCriteriaExpansionBoundSuppressesTransitiveChain is the #11087 RED-first
// regression: a 9-hop versioned shared-criteria chain ending in a versionless
// wildcard fan-out must converge with only the seed CVE's rows once the
// provenance bound is on. Unbounded it walks the whole chain (and truncates at
// the 8-round cap); bounded it stops after the seed round because no
// non-seed, non-component criteria may drive the product probe.
func TestCriteriaExpansionBoundSuppressesTransitiveChain(t *testing.T) {
	t.Parallel()

	loaded, truncation, _, _ := runCriteriaChain(t, false)
	ids := loadedFactIDs(loaded)
	if !containsID(ids, "prod-CVE-2026-0101-in") {
		t.Fatalf("unbounded load missing transitive row prod-CVE-2026-0101-in, got %v", ids)
	}
	if !containsID(ids, "comp-t2") {
		t.Fatalf("unbounded load missing discovered component comp-t2, got %v", ids)
	}
	if !truncation.rounds {
		t.Fatalf("unbounded 9-hop chain should hit the round cap, truncation = %+v", truncation)
	}

	bounded, btruncation, outcome, stub := runCriteriaChain(t, true)
	bids := loadedFactIDs(bounded)
	if !containsID(bids, "prod-seed") {
		t.Fatalf("bounded load missing seed row prod-seed, got %v", bids)
	}
	if !containsID(bids, "cve-seed") {
		t.Fatalf("bounded load missing seed cve record, got %v", bids)
	}
	for _, id := range bids {
		if id != "prod-seed" && id != "cve-seed" {
			t.Fatalf("bounded load leaked transitive row %q, got %v", id, bids)
		}
	}
	if btruncation.partial() {
		t.Fatalf("bounded load should converge, truncation = %+v", btruncation)
	}
	// Nothing transitive derives here, so nothing is suppressed: the seed
	// criteria rides the seed-only arm from the first derivation and no
	// in-scope component ever puts a transitive criteria on the product arm.
	// Suppression counting itself is proved by the anchored parity universe,
	// where an in-scope component pulls transitive rows into derivation.
	if outcome.SuppressedTransitiveCriteria != 0 {
		t.Fatalf("bounded load should suppress nothing without an in-scope component, outcome = %+v", outcome)
	}
	if len(stub.filters) == 0 {
		t.Fatalf("expected recorded filters")
	}
	t.Logf("bounded requests=%d suppressed=%d", len(stub.filters), outcome.SuppressedTransitiveCriteria)
}

// TestCriteriaExpansionBoundDropsOnlyUnanchoredTransitiveFindings resolves the
// apparent finding-set delta at the classify boundary. The unbounded load
// reaches transitive product rows the bounded load never requests, but in this
// fixture no transitive row can anchor: no component, digest, repository, or
// OS-package evidence joins it, so the owned-anchor gate
// (supplyChainImpactFindingHasOwnedAnchor) drops it in BOTH arms. The
// post-gate finding sets are therefore equal (both empty here), while the
// loaded sets differ. Anchored transitive findings are the subject of the
// parity shim (criteria_expansion_parity_test.go), where they survive the
// gate in both arms.
func TestCriteriaExpansionBoundDropsOnlyUnanchoredTransitiveFindings(t *testing.T) {
	t.Parallel()

	loaded, _, _, _ := runCriteriaChain(t, false)
	bounded, _, _, _ := runCriteriaChain(t, true)
	if len(loaded) <= len(bounded) {
		t.Fatalf("unbounded load should reach strictly more rows, unbounded=%d bounded=%d", len(loaded), len(bounded))
	}
	unboundedFindings := BuildSupplyChainImpactFindings(loaded)
	boundedFindings := BuildSupplyChainImpactFindings(bounded)
	if len(unboundedFindings) != 0 || len(boundedFindings) != 0 {
		t.Fatalf("unanchored transitive rows must drop post-gate in both arms, unbounded=%v bounded=%v",
			findingCVEIDs(unboundedFindings), findingCVEIDs(boundedFindings))
	}
}

// TestCriteriaExpansionBoundPreservesComponentHops proves both required
// one-hop directions survive the bound: (A) a scope component's CPE pulls
// the matching product rows even for an unseeded CVE, and (B) a seeded CVE's
// criteria pulls the matching component even though no component was loaded.
func TestCriteriaExpansionBoundPreservesComponentHops(t *testing.T) {
	t.Parallel()

	const (
		cveX  = "CVE-2026-0201"
		critX = "cpe:2.3:a:scope:toaster:2.0:*:*:*:*:*:*:*"
		critY = "cpe:2.3:a:other:blender:3.0:*:*:*:*:*:*:*"
	)
	comp := sbomComponentCPEImpactFact("comp-x", "doc-x", critX)
	prodX := vulnerabilityAffectedProductFact("prod-x", cveX, critX, "match-x", true)
	prodY := vulnerabilityAffectedProductFact("prod-y", cveX, critY, "match-y", true)
	cveRecX := vulnerabilityCVEFact("cve-x", cveX, 6.0)
	universeProducts := []facts.Envelope{prodX, prodY}
	universeComponents := []facts.Envelope{comp}
	universeCVEs := []facts.Envelope{cveRecX}

	stub := &stubSupplyChainImpactFactLoader{activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		productOK := map[string]struct{}{}
		for _, c := range filter.ProductCriteria {
			productOK[c] = struct{}{}
		}
		componentOK := map[string]struct{}{}
		for _, c := range filter.ProductCriteria {
			componentOK[c] = struct{}{}
		}
		for _, c := range filter.SeedProductCriteria {
			componentOK[c] = struct{}{}
		}
		var out []facts.Envelope
		for _, env := range universeProducts {
			if _, ok := productOK[payloadcore.PayloadStr(env.Payload, "criteria")]; ok {
				out = append(out, env)
			}
		}
		for _, env := range universeComponents {
			if _, ok := componentOK[payloadcore.PayloadStr(env.Payload, "cpe")]; ok {
				out = append(out, env)
			}
		}
		for _, env := range universeCVEs {
			for _, id := range filter.CVEIDs {
				if supplyChainCVEID(env.Payload) == id {
					out = append(out, env)
				}
			}
		}
		return out
	}}

	// Hop A: the scope owns a component bearing critX; CVE-X is not seeded.
	h := SupplyChainImpactHandler{
		FactLoader:             stub,
		CriteriaExpansionBound: &SupplyChainImpactCriteriaExpansionBound{},
	}
	initialEnvelopes := []facts.Envelope{comp}
	initial := supplyChainImpactFilter(initialEnvelopes)
	loaded, truncation, _, err := h.loadActiveSupplyChainImpactFactsUntilStable(
		context.Background(), initialEnvelopes, initial,
		newSupplyChainImpactEvidenceBudget(0), nil, "")
	if err != nil {
		t.Fatalf("hop A load error = %v", err)
	}
	ids := loadedFactIDs(loaded)
	if !containsID(ids, "prod-x") {
		t.Fatalf("hop A: component CPE should pull matching product row, got %v", ids)
	}
	if containsID(ids, "prod-y") {
		t.Fatalf("hop A: unseeded CVE's other-criteria row must not load, got %v", ids)
	}
	if truncation.partial() {
		t.Fatalf("hop A should converge, truncation = %+v", truncation)
	}

	// Hop B: seed CVE-S's criteria pulls a component bearing it.
	const (
		cveS  = "CVE-2026-0301"
		critS = "cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*"
	)
	compS := sbomComponentCPEImpactFact("comp-s", "doc-s", critS)
	stub2 := &stubSupplyChainImpactFactLoader{activeForFilter: func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		componentOK := map[string]struct{}{}
		for _, c := range filter.ProductCriteria {
			componentOK[c] = struct{}{}
		}
		for _, c := range filter.SeedProductCriteria {
			componentOK[c] = struct{}{}
		}
		if _, ok := componentOK[critS]; ok {
			return []facts.Envelope{compS}
		}
		return nil
	}}
	h2 := SupplyChainImpactHandler{
		FactLoader:             stub2,
		CriteriaExpansionBound: &SupplyChainImpactCriteriaExpansionBound{},
	}
	seedEnvelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-s", cveS, 9.0),
		vulnerabilityAffectedProductFact("prod-s", cveS, critS, "match-s", true),
	}
	initial2 := supplyChainImpactFilter(seedEnvelopes)
	loaded2, _, _, err := h2.loadActiveSupplyChainImpactFactsUntilStable(
		context.Background(), seedEnvelopes, initial2,
		newSupplyChainImpactEvidenceBudget(0), nil, "")
	if err != nil {
		t.Fatalf("hop B load error = %v", err)
	}
	if ids := loadedFactIDs(loaded2); !containsID(ids, "comp-s") {
		t.Fatalf("hop B: seeded criteria should pull matching component, got %v", ids)
	}
}

// TestCriteriaExpansionBoundSeedsAliasClosure proves alias/upstream-discovered
// CVEs count as seeded (identity closure, not criteria transitivity): once
// the seeded record's alias pulls CVE-A's record in, A's product criteria may
// drive the component probe.
func TestCriteriaExpansionBoundSeedsAliasClosure(t *testing.T) {
	t.Parallel()

	const (
		cveS  = "CVE-2026-0401"
		cveA  = "CVE-2026-0402"
		critA = "cpe:2.3:a:aliased:thing:1.0:*:*:*:*:*:*:*"
		critS = "cpe:2.3:a:seeded:thing:1.0:*:*:*:*:*:*:*"
	)
	cveRecS := facts.Envelope{
		FactID:   "cve-s",
		FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id": cveS, "advisory_id": cveS, "cvss_score": 9.0,
			"aliases": []any{cveS, cveA},
		},
	}
	cveRecA := vulnerabilityCVEFact("cve-a", cveA, 8.0)
	prodS := vulnerabilityAffectedProductFact("prod-s", cveS, critS, "match-s", true)
	prodA := vulnerabilityAffectedProductFact("prod-a", cveA, critA, "match-a", true)
	compA := sbomComponentCPEImpactFact("comp-a", "doc-a", critA)

	stub := &stubSupplyChainImpactFactLoader{activeForFilter: criteriaChainLoader(criteriaChainUniverse{
		products:   []facts.Envelope{prodS, prodA},
		cves:       []facts.Envelope{cveRecS, cveRecA},
		components: []facts.Envelope{compA},
	})}
	h := SupplyChainImpactHandler{
		FactLoader:             stub,
		CriteriaExpansionBound: &SupplyChainImpactCriteriaExpansionBound{},
	}
	seedEnvelopes := []facts.Envelope{cveRecS, prodS}
	initial := supplyChainImpactFilter(seedEnvelopes)
	loaded, truncation, _, err := h.loadActiveSupplyChainImpactFactsUntilStable(
		context.Background(), seedEnvelopes, initial,
		newSupplyChainImpactEvidenceBudget(0), nil, "")
	if err != nil {
		t.Fatalf("alias closure load error = %v", err)
	}
	ids := loadedFactIDs(loaded)
	if !containsID(ids, "cve-a") {
		t.Fatalf("alias arm should load aliased record, got %v", ids)
	}
	if !containsID(ids, "comp-a") {
		t.Fatalf("alias-seeded criteria should pull matching component, got %v", ids)
	}
	// CVE-A is seeded through alias identity, so its own product row arrives
	// through the same-CVE arm (the vulnerability_cve_id probe covers
	// affected_product), exactly as production binds it. The bound only keeps
	// that row's criteria off the product probe until the matching component
	// itself loads: critA may ride ProductCriteria only in a round derived
	// after comp-A arrived (its document rides the same derivation).
	if !containsID(ids, "prod-a") {
		t.Fatalf("seeded CVE's own product row arrives through the CVE arm, got %v", ids)
	}
	for _, filter := range stub.filters {
		if containsID(filter.ProductCriteria, critA) && !containsID(filter.DocumentIDs, "doc-a") {
			t.Fatalf("alias-seeded criteria must not drive the product probe before its component loads, filters = %+v", stub.filters)
		}
	}
	if truncation.partial() {
		t.Fatalf("alias closure should converge, truncation = %+v", truncation)
	}
}

// TestCriteriaExpansionBoundMatchesClassifyNormalization proves the split uses
// the same comparison the classifier uses (componentMatchesAffectedProduct,
// purl_match.go:38: EqualFold over TrimSpace): a stored criteria differing
// only in case and surrounding space from a loaded component CPE is
// component-origin and must drive both probes, not be suppressed.
func TestCriteriaExpansionBoundMatchesClassifyNormalization(t *testing.T) {
	t.Parallel()

	const cveS = "CVE-2026-0501"
	stored := "CPE:2.3:A:Seeded:Gadget:1.0:*:*:*:*:*:*:*"
	borne := "  cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*  "
	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-s", cveS, 9.0),
		vulnerabilityAffectedProductFact("prod-s", cveS, stored, "match-s", true),
		sbomComponentCPEImpactFact("comp-s", "doc-s", borne),
	}
	initial := supplyChainImpactFilter(envelopes)
	bound := newCriteriaExpansionBoundState(initial, nil, "")
	next, _ := bound.apply(initial, envelopes, envelopes, SupplyChainImpactFactFilter{})
	// Derivation emits both spellings (product row and component CPE); both
	// must stay on the both-probes arm, keeping their stored spelling for
	// the storage exact match.
	for _, want := range []string{stored, "cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*"} {
		if !containsID(next.ProductCriteria, want) {
			t.Fatalf("case/space-variant component criteria must stay component-origin, ProductCriteria = %v", next.ProductCriteria)
		}
	}
	if len(next.SeedProductCriteria) != 0 {
		t.Fatalf("component-won criteria must not also ride the seed arm, SeedProductCriteria = %v", next.SeedProductCriteria)
	}
}

// TestCriteriaExpansionBoundSubSignalsRideEveryPass is the P5 wiring proof:
// the provenance-bound sub-signals ride every pass, zeros included when the
// seam is off, so bound-on and bound-off passes stay comparable. Success is
// judged on loads and pass-state conversion, never on suppressed > 0.
func TestCriteriaExpansionBoundSubSignalsRideEveryPass(t *testing.T) {
	t.Parallel()

	run := func(bound bool) map[string]float64 {
		t.Helper()
		loader := &stubSupplyChainImpactFactLoader{
			scopeFacts: []facts.Envelope{
				vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8),
				vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"),
			},
			active: []facts.Envelope{
				packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"),
			},
		}
		handler := SupplyChainImpactHandler{
			FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
			FactLoader:         loader,
			Writer:             &recordingSupplyChainImpactWriter{},
			Now:                func() time.Time { return time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC) },
		}
		if bound {
			handler.CriteriaExpansionBound = &SupplyChainImpactCriteriaExpansionBound{}
		}
		result, err := handler.Handle(context.Background(), reducercontract.Intent{
			IntentID:     "intent-bound-signals",
			ScopeID:      "vuln-intel://osv/npm/example",
			GenerationID: "generation-impact",
			SourceSystem: "vulnerability_intelligence",
			Domain:       reducercontract.DomainSupplyChainImpact,
			Cause:        "vulnerability evidence observed",
		})
		if err != nil {
			t.Fatalf("Handle() error = %v, want nil", err)
		}
		return result.SubSignals
	}

	off := run(false)
	for _, key := range []string{
		"criteria_bound_suppressed_transitive",
		"criteria_bound_loads",
		"criteria_bound_component_criteria",
		"criteria_bound_seed_criteria",
		"criteria_bound_seed_cves",
	} {
		value, ok := off[key]
		if !ok {
			t.Fatalf("seam-off SubSignals missing %q: bound-on and bound-off passes must stay comparable", key)
		}
		if value != 0 {
			t.Fatalf("seam-off SubSignals[%q] = %v, want 0", key, value)
		}
	}

	on := run(true)
	for _, key := range []string{
		"criteria_bound_suppressed_transitive",
		"criteria_bound_loads",
		"criteria_bound_component_criteria",
		"criteria_bound_seed_criteria",
		"criteria_bound_seed_cves",
	} {
		if _, ok := on[key]; !ok {
			t.Fatalf("bound-on SubSignals missing %q", key)
		}
	}
	if on["criteria_bound_loads"] == 0 {
		t.Fatalf("bound-on SubSignals[criteria_bound_loads] = 0, want the pass load count")
	}
	if on["criteria_bound_seed_cves"] == 0 {
		t.Fatalf("bound-on SubSignals[criteria_bound_seed_cves] = 0, want the seed CVE count")
	}
}

// TestCriteriaExpansionBoundSeamOffIsNoop pins the one-line rollback: a nil
// bound state returns every filter untouched and reports the zero outcome, so
// the until-stable loop and the storage union stay byte-identical to the
// prior unbounded semantics.
func TestCriteriaExpansionBoundSeamOffIsNoop(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-s", "CVE-2026-0001", 9.0),
		vulnerabilityAffectedProductFact("prod-s", "CVE-2026-0001", "cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*", "match-s", true),
		sbomComponentCPEImpactFact("comp-s", "doc-s", "cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*"),
	}
	filter := supplyChainImpactFilter(envelopes)
	var bound *criteriaExpansionBoundState
	next, suppressed := bound.apply(filter, envelopes, envelopes, SupplyChainImpactFactFilter{})
	if suppressed != 0 {
		t.Fatalf("nil bound suppressed %d criteria, want 0", suppressed)
	}
	if !reflect.DeepEqual(next, filter) {
		t.Fatalf("nil bound rewrote the filter:\ngot  %+v\nwant %+v", next, filter)
	}
	if outcome := bound.outcome(); outcome != (supplyChainImpactCriteriaBoundOutcome{}) {
		t.Fatalf("nil bound outcome = %+v, want zero", outcome)
	}
}

// TestCriteriaExpansionBoundScopedPassConverges proves a scoped (#9779) pass
// still completes with the bound on: the stamp seeds the bound, the scoped
// restrict still intersects only the CVE/package arms, and transitive rows
// stay out.
func TestCriteriaExpansionBoundScopedPassConverges(t *testing.T) {
	t.Parallel()

	u := buildCriteriaChainUniverse()
	stub := &stubSupplyChainImpactFactLoader{activeForFilter: criteriaChainLoader(u)}
	h := SupplyChainImpactHandler{
		FactLoader:             stub,
		CriteriaExpansionBound: &SupplyChainImpactCriteriaExpansionBound{},
	}
	scoped := &scopedSupplyChainImpactScope{cves: []string{u.seedCVE}}
	// The scope's own evidence may name an unseeded CVE's row; its criteria
	// must derive and then suppress (counted) without ever reaching a probe.
	const (
		unseededCVE      = "CVE-2026-0999"
		unseededCriteria = "cpe:2.3:a:unseeded:thing:1.0:*:*:*:*:*:*:*"
	)
	initialEnvelopes := append(append([]facts.Envelope(nil), criteriaChainInitial(u)...),
		vulnerabilityAffectedProductFact("prod-unseeded", unseededCVE, unseededCriteria, "match-unseeded", true))
	initial := restrictSupplyChainImpactFilter(
		supplyChainImpactFilter(initialEnvelopes), scoped)
	loaded, truncation, outcome, err := h.loadActiveSupplyChainImpactFactsUntilStable(
		context.Background(), initialEnvelopes, initial,
		newSupplyChainImpactEvidenceBudget(0), scoped, "")
	if err != nil {
		t.Fatalf("scoped bounded load error = %v", err)
	}
	ids := loadedFactIDs(loaded)
	if !containsID(ids, "prod-seed") {
		t.Fatalf("scoped bounded load missing seed row, got %v", ids)
	}
	for _, id := range ids {
		switch id {
		case "prod-seed", "cve-seed", "prod-unseeded":
		default:
			t.Fatalf("scoped bounded load leaked transitive row %q, got %v", id, ids)
		}
	}
	for _, filter := range stub.filters {
		if containsID(filter.ProductCriteria, unseededCriteria) {
			t.Fatalf("suppressed criteria must never reach the product probe, filters = %+v", stub.filters)
		}
	}
	if truncation.partial() {
		t.Fatalf("scoped bounded load should converge, truncation = %+v", truncation)
	}
	if outcome.SuppressedTransitiveCriteria == 0 {
		t.Fatalf("scoped bounded load should count suppressions, outcome = %+v", outcome)
	}
}
