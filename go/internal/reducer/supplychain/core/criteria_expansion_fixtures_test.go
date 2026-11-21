// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Shared-criteria chain fixture for #11087: seed CVE-S shares versioned
// criteria C_SEED with CVE-T1; T1 shares C_T1 with T2; ...; T9 shares the
// versionless wildcard C_WILD with three sibling CVEs. A component bears
// T2's outgoing criteria C_T2 but is NOT in the initial envelopes: it is
// reachable only through the component probe.
type criteriaChainUniverse struct {
	seedCVE          string
	chain            []string // T1..T9 CVE ids in order
	wildcardSiblings []string
	seedCriteria     string
	linkCriteria     []string // C_T1..C_T9, link i-1 -> i
	wildcard         string
	componentCPE     string
	products         []facts.Envelope
	cves             []facts.Envelope
	components       []facts.Envelope
}

func buildCriteriaChainUniverse() criteriaChainUniverse {
	u := criteriaChainUniverse{
		seedCVE:          "CVE-2026-0001",
		wildcardSiblings: []string{"CVE-2026-0111", "CVE-2026-0112", "CVE-2026-0113"},
		seedCriteria:     "cpe:2.3:a:seed:widget:1.0:*:*:*:*:*:*:*",
		wildcard:         "cpe:2.3:a:kernel:linux_kernel:*:*:*:*:*:*:*:*",
	}
	for i := 1; i <= 9; i++ {
		u.chain = append(u.chain, "CVE-2026-"+itoa4(100+i))
	}
	for i := 1; i <= 9; i++ {
		u.linkCriteria = append(u.linkCriteria, "cpe:2.3:a:chain:lib:"+itoa4(i)+".0:*:*:*:*:*:*:*")
	}
	u.componentCPE = u.linkCriteria[1] // C_T2: T2's outgoing criteria.
	u.cves = append(u.cves, vulnerabilityCVEFact("cve-seed", u.seedCVE, 9.8))
	u.products = append(u.products, vulnerabilityAffectedProductFact(
		"prod-seed", u.seedCVE, u.seedCriteria, "match-seed", true))
	prev := u.seedCriteria
	for i, cve := range u.chain {
		out := u.linkCriteria[i]
		if i == len(u.chain)-1 {
			out = u.wildcard
		}
		u.cves = append(u.cves, vulnerabilityCVEFact("cve-"+cve, cve, 7.5))
		u.products = append(u.products,
			vulnerabilityAffectedProductFact("prod-"+cve+"-in", cve, prev, "match-"+cve+"-in", true),
			vulnerabilityAffectedProductFact("prod-"+cve+"-out", cve, out, "match-"+cve+"-out", true))
		prev = u.linkCriteria[i]
	}
	for _, cve := range u.wildcardSiblings {
		u.cves = append(u.cves, vulnerabilityCVEFact("cve-"+cve, cve, 5.0))
		u.products = append(u.products, vulnerabilityAffectedProductFact(
			"prod-"+cve, cve, u.wildcard, "match-"+cve, true))
	}
	u.components = append(u.components, sbomComponentCPEImpactFact("comp-t2", "doc-1", u.componentCPE))
	return u
}

func itoa4(n int) string {
	s := ""
	for i := 0; i < 4; i++ {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}

// criteriaChainLoader emulates the production storage binding
// (branches.go): the vulnerability_cve_id and vulnerability_advisory_id probes
// return every vulnerability row (cve, affected_package, affected_product,
// signals) of the listed CVE, the alias probes return CVE records only, the
// affected_product_criteria probe matches the component-origin list only, and
// the sbom_component_cpe probe matches the union of both lists. Package and
// consumption discovery arms are out of scope for this seam: the bound does
// not touch them, and the fixtures carry that evidence in the initial set.
// With the bound seam off, derivation leaves every criteria in
// ProductCriteria, so this reproduces today's fan-out exactly.
func criteriaChainLoader(u criteriaChainUniverse) func(SupplyChainImpactFactFilter) []facts.Envelope {
	return func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		cveOK := map[string]struct{}{}
		for _, id := range filter.CVEIDs {
			cveOK[id] = struct{}{}
		}
		for _, id := range filter.AdvisoryIDs {
			cveOK[id] = struct{}{}
		}
		aliasOK := map[string]struct{}{}
		for _, id := range filter.AliasIDs {
			aliasOK[id] = struct{}{}
		}
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
		for _, env := range u.cves {
			id := supplyChainCVEID(env.Payload)
			if _, ok := cveOK[id]; ok {
				out = append(out, env)
				continue
			}
			if _, ok := aliasOK[id]; ok {
				out = append(out, env)
			}
		}
		for _, env := range u.products {
			if _, ok := cveOK[supplyChainCVEID(env.Payload)]; ok {
				out = append(out, env)
				continue
			}
			if _, ok := productOK[payloadcore.PayloadStr(env.Payload, "criteria")]; ok {
				out = append(out, env)
			}
		}
		for _, env := range u.components {
			if _, ok := componentOK[payloadcore.PayloadStr(env.Payload, "cpe")]; ok {
				out = append(out, env)
			}
		}
		return out
	}
}

func criteriaChainInitial(u criteriaChainUniverse) []facts.Envelope {
	return []facts.Envelope{
		vulnerabilityCVEFact("cve-seed", u.seedCVE, 9.8),
		vulnerabilityAffectedProductFact("prod-seed", u.seedCVE, u.seedCriteria, "match-seed", true),
	}
}

func loadedFactIDs(envelopes []facts.Envelope) []string {
	ids := make([]string, 0, len(envelopes))
	for _, env := range envelopes {
		ids = append(ids, env.FactID)
	}
	sort.Strings(ids)
	return ids
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func findingCVEIDs(findings []SupplyChainImpactFinding) []string {
	set := map[string]struct{}{}
	for _, f := range findings {
		set[f.CVEID] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for id := range set {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func runCriteriaChain(t *testing.T, bound bool) ([]facts.Envelope, supplyChainImpactTruncation, supplyChainImpactCriteriaBoundOutcome, *stubSupplyChainImpactFactLoader) {
	t.Helper()
	u := buildCriteriaChainUniverse()
	stub := &stubSupplyChainImpactFactLoader{activeForFilter: criteriaChainLoader(u)}
	h := SupplyChainImpactHandler{FactLoader: stub}
	if bound {
		h.CriteriaExpansionBound = &SupplyChainImpactCriteriaExpansionBound{}
	}
	initial := supplyChainImpactFilter(criteriaChainInitial(u))
	loaded, truncation, outcome, err := h.loadActiveSupplyChainImpactFactsUntilStable(
		context.Background(), criteriaChainInitial(u), initial,
		newSupplyChainImpactEvidenceBudget(0), nil, "")
	if err != nil {
		t.Fatalf("loadActiveSupplyChainImpactFactsUntilStable() error = %v", err)
	}
	assertRoundsWithinCap(t, bound, outcome, truncation)
	return loaded, truncation, outcome, stub
}

// assertRoundsWithinCap pins the #11087 round contract on every bounded
// fixture: the pass converges inside maxSupplyChainImpactActiveEvidenceLoads
// and never stops on the round cap. Unbounded arms are exempt, since hitting
// the cap is exactly the defect the bound removes.
func assertRoundsWithinCap(t *testing.T, bound bool, outcome supplyChainImpactCriteriaBoundOutcome, truncation supplyChainImpactTruncation) {
	t.Helper()
	if !bound {
		return
	}
	if outcome.Loads > maxSupplyChainImpactActiveEvidenceLoads || truncation.rounds {
		t.Fatalf("bounded pass used %d loads (cap %d), truncation = %+v", outcome.Loads, maxSupplyChainImpactActiveEvidenceLoads, truncation)
	}
	t.Logf("bounded pass converged in %d loads (cap %d)", outcome.Loads, maxSupplyChainImpactActiveEvidenceLoads)
}
