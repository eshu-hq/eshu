// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Criteria-expansion parity shim for #11087 (acceptance criterion 2): the
// differential the design record
// (docs/internal/design/11087-criteria-expansion-bound.md) requires before
// production wiring. Oracle = Build over the full synthetic universe
// envelopes (complete evidence, no loader); the bounded arm runs the
// until-stable load through the production-shaped stub, then Build over the
// loaded set. Comparison is at the final persisted-finding boundary (Build
// already applies the owned-anchor gate) on full finding keys including
// order. Store spelling is canonical throughout (exact-match storage binds
// both arms identically); cross-spelling classification is proved at the
// split level by
// TestCriteriaExpansionBoundMatchesClassifyNormalization.

package core

import (
	"context"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// criteriaParityUniverse is the QA-shaped synthetic corpus: the 9-hop chain
// plus wildcard super-connector, with anchored components at hop>=2 criteria,
// a hop-B seed pair, a case-variant pair, a native-package CVE, and a
// non-vulnerable row. all is the oracle input; initial is the scope-owned
// evidence both loader arms start from.
type criteriaParityUniverse struct {
	chain criteriaChainUniverse
	all   []facts.Envelope
	// initial is scope-owned: every envelope here is present in all arms
	// from round zero, exactly like a real scope's own SBOM and findings.
	initial []facts.Envelope
	// nonVulnCriteria is borne only by a non-vulnerable row: it must never
	// derive, never reach a probe, and never form a finding.
	nonVulnCriteria string
	// seedOnlyCriteria rides the seed arm: no component bears it, so the
	// product probe must never request it.
	seedOnlyCriteria string
}

func buildCriteriaParityUniverse() criteriaParityUniverse {
	const (
		digestT2 = "sha256:parity-anchor-t2"
		repoT2   = "repo://example/t2-service"
		digestB  = "sha256:parity-anchor-b"
		repoB    = "repo://example/b-service"
		digestK  = "sha256:parity-anchor-k"
		repoK    = "repo://example/k-service"
		digestN  = "sha256:parity-anchor-n"
		repoN    = "repo://example/n-service"
		cveB     = "CVE-2026-0301"
		critB    = "cpe:2.3:a:seeded:gadget:1.0:*:*:*:*:*:*:*"
		cveK     = "CVE-2026-0502"
		critK    = "CPE:2.3:A:CASE:WIDGET:9.9:*:*:*:*:*:*:*"
		cveN     = "CVE-2026-0601"
		critN    = "cpe:2.3:a:native:product:1.0:*:*:*:*:*:*:*"
		pkgN     = "pkg:npm/native-victim"
		cveNV    = "CVE-2026-0001"
		critNV   = "cpe:2.3:a:never:requested:0.0:*:*:*:*:*:*:*"
	)
	c := buildCriteriaChainUniverse()
	u := criteriaParityUniverse{
		chain:            c,
		nonVulnCriteria:  critNV,
		seedOnlyCriteria: critB,
	}
	addAll := func(envs ...facts.Envelope) { u.all = append(u.all, envs...) }
	addInitial := func(envs ...facts.Envelope) {
		u.all = append(u.all, envs...)
		u.initial = append(u.initial, envs...)
	}
	addAll(c.cves...)
	addAll(c.products...)
	addAll(c.components...)
	// Hop-A anchor kit: comp-t2 is in-scope, so C_T2 drives both probes in
	// the bounded arm and pulls the T2/T3 rows directly.
	addInitial(sbomComponentCPEImpactFact("comp-t2", "doc-1", c.componentCPE))
	addInitial(sbomAttachmentImpactFact("att-t2", "doc-1", digestT2))
	addInitial(containerImageIdentityImpactFact("img-t2", digestT2, repoT2))
	// Hop B: seed CVE-B's criteria pulls comp-B through the component probe.
	// The attachment and image are scope-owned evidence; only the component
	// row itself is discovered.
	addAll(vulnerabilityCVEFact("cve-b", cveB, 9.0))
	addAll(vulnerabilityAffectedProductFact("prod-b", cveB, critB, "match-b", true))
	addAll(sbomComponentCPEImpactFact("comp-b", "doc-b", critB))
	addInitial(vulnerabilityCVEFact("cve-b", cveB, 9.0))
	addInitial(vulnerabilityAffectedProductFact("prod-b", cveB, critB, "match-b", true))
	addInitial(sbomAttachmentImpactFact("att-b", "doc-b", digestB))
	addInitial(containerImageIdentityImpactFact("img-b", digestB, repoB))
	// Case variant: non-canonical but consistent spelling on both sides of
	// the join flows through the loop and classifies identically.
	addAll(vulnerabilityCVEFact("cve-k", cveK, 7.0))
	addAll(vulnerabilityAffectedProductFact("prod-k", cveK, critK, "match-k", true))
	addAll(sbomComponentCPEImpactFact("comp-k", "doc-k", critK))
	addInitial(vulnerabilityCVEFact("cve-k", cveK, 7.0))
	// prod-k is loader-discovered through the product probe, never initial:
	// the in-scope component keeps its criteria on the both-probes arm.
	addInitial(sbomComponentCPEImpactFact("comp-k", "doc-k", critK))
	addInitial(sbomAttachmentImpactFact("att-k", "doc-k", digestK))
	addInitial(containerImageIdentityImpactFact("img-k", digestK, repoK))
	// Native packages suppress the product path: CVE-N classifies through
	// its affected package and consumption in every arm.
	addAll(vulnerabilityCVEFact("cve-n", cveN, 8.0))
	addAll(vulnerabilityAffectedProductFact("prod-n", cveN, critN, "match-n", true))
	addInitial(vulnerabilityCVEFact("cve-n", cveN, 8.0))
	addInitial(vulnerabilityAffectedProductFact("prod-n", cveN, critN, "match-n", true))
	addInitial(vulnerabilityAffectedPackageFact("pkg-n", cveN, pkgN, "npm", "native-victim", "1.2.3", "1.3.0"))
	addInitial(packageConsumptionFactWithRange("cons-n", pkgN, repoN, "1.2.3"))
	// Non-vulnerable row for the seed: widens the CVE arm unconditionally
	// but emits no criteria.
	addInitial(vulnerabilityAffectedProductFact("prod-nv", cveNV, critNV, "match-nv", false))
	return u
}

// runParityArm runs the until-stable load over the parity universe through
// the production-shaped stub, with the provenance bound on or off.
func runParityArm(t *testing.T, u criteriaParityUniverse, bound bool, scoped *scopedSupplyChainImpactScope) ([]facts.Envelope, supplyChainImpactTruncation, supplyChainImpactCriteriaBoundOutcome, *stubSupplyChainImpactFactLoader) {
	t.Helper()
	stub := &stubSupplyChainImpactFactLoader{activeForFilter: criteriaParityLoader(u)}
	h := SupplyChainImpactHandler{FactLoader: stub}
	if bound {
		h.CriteriaExpansionBound = &SupplyChainImpactCriteriaExpansionBound{}
	}
	initialEnvelopes := append([]facts.Envelope(nil), u.initial...)
	initial := supplyChainImpactFilter(initialEnvelopes)
	if scoped != nil {
		initial = restrictSupplyChainImpactFilter(initial, scoped)
	}
	loaded, truncation, outcome, err := h.loadActiveSupplyChainImpactFactsUntilStable(
		context.Background(), initialEnvelopes, initial,
		newSupplyChainImpactEvidenceBudget(0), scoped, "")
	if err != nil {
		t.Fatalf("loadActiveSupplyChainImpactFactsUntilStable() error = %v", err)
	}
	assertRoundsWithinCap(t, bound, outcome, truncation)
	return loaded, truncation, outcome, stub
}

// criteriaParityLoader serves the parity universe through the production
// binding (branches.go): the vulnerability_cve_id and vulnerability_advisory_id
// probes return every vulnerability row of the listed CVE, the alias probes
// return CVE records only, the affected_product_criteria probe matches the
// component-origin list, the sbom_component_cpe probe matches the union, and
// attachments and images answer the digest and document arms the
// resolved-digest stage derives. The anchor kit is scope-owned (initial), the
// stub only answers the loop's re-requests.
func criteriaParityLoader(u criteriaParityUniverse) func(SupplyChainImpactFactFilter) []facts.Envelope {
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
		digests := map[string]struct{}{}
		for _, d := range filter.SubjectDigests {
			digests[d] = struct{}{}
		}
		documents := map[string]struct{}{}
		for _, d := range filter.DocumentIDs {
			documents[d] = struct{}{}
		}
		var out []facts.Envelope
		for _, env := range u.all {
			switch env.FactKind {
			case chain.VulnerabilityCVEFactKind:
				id := supplyChainCVEID(env.Payload)
				if _, ok := cveOK[id]; ok {
					out = append(out, env)
					continue
				}
				if _, ok := aliasOK[id]; ok {
					out = append(out, env)
				}
			case chain.VulnerabilityAffectedProductFactKind, chain.VulnerabilityAffectedPackageFactKind:
				if _, ok := cveOK[supplyChainCVEID(env.Payload)]; ok {
					out = append(out, env)
					continue
				}
				if env.FactKind == chain.VulnerabilityAffectedProductFactKind {
					if _, ok := productOK[payloadcore.PayloadStr(env.Payload, "criteria")]; ok {
						out = append(out, env)
					}
				}
			case chain.SBOMComponentFactKind:
				if _, ok := componentOK[payloadcore.PayloadStr(env.Payload, "cpe")]; ok {
					out = append(out, env)
				}
			case reducercontract.SBOMAttestationAttachmentFactKind:
				if _, ok := documents[payloadcore.PayloadStr(env.Payload, "document_id")]; ok {
					out = append(out, env)
					continue
				}
				if _, ok := digests[payloadcore.PayloadStr(env.Payload, "subject_digest")]; ok {
					out = append(out, env)
				}
			case reducercontract.ContainerImageIdentityFactKind:
				if _, ok := digests[payloadcore.PayloadStr(env.Payload, "digest")]; ok {
					out = append(out, env)
				}
			}
		}
		return out
	}
}

// criteriaParityKey is the verdict's full finding key: CVE, advisory,
// criteria, status, severity, digest, repository, evidence fact ids, and
// group members. Build sorts findings deterministically, so slices of keys
// compare with order.
type criteriaParityKey struct {
	CVE             string
	Advisory        string
	Criteria        string
	Status          SupplyChainImpactStatus
	Severity        float64
	Digest          string
	Repository      string
	EvidenceFactIDs []string
	GroupMembers    []string
}

func projectCriteriaParityKeys(findings []SupplyChainImpactFinding) []criteriaParityKey {
	keys := make([]criteriaParityKey, 0, len(findings))
	for _, f := range findings {
		evidence := append([]string(nil), f.EvidenceFactIDs...)
		sort.Strings(evidence)
		members := append([]string(nil), f.AdvisoryGroupMembers...)
		sort.Strings(members)
		keys = append(keys, criteriaParityKey{
			CVE:             f.CVEID,
			Advisory:        f.AdvisoryID,
			Criteria:        f.ProductCriteria,
			Status:          f.Status,
			Severity:        f.CVSSScore,
			Digest:          f.SubjectDigest,
			Repository:      f.RepositoryID,
			EvidenceFactIDs: evidence,
			GroupMembers:    members,
		})
	}
	return keys
}

// TestCriteriaExpansionBoundParityAcrossFinalFindings is the AC2 differential:
// the same findings, on full keys and in order, with and without the bound.
// The unbounded arm must stop at the round cap (truncation conversion: it
// goes partial on the chain), the bounded arm must complete, and its
// post-gate findings must equal the oracle built over the full universe.
// Anchored hop-A findings (T2/T3 via the in-scope component), hop B, the
// case-variant pair, and the native-package CVE are all non-vacuous: the
// oracle holds them, so equality proves the bound kept them.
func TestCriteriaExpansionBoundParityAcrossFinalFindings(t *testing.T) {
	t.Parallel()

	u := buildCriteriaParityUniverse()
	oracle := projectCriteriaParityKeys(BuildSupplyChainImpactFindings(u.all))
	if len(oracle) == 0 {
		t.Fatal("oracle must hold anchored findings for a non-vacuous parity")
	}
	oracleCVEs := map[string]struct{}{}
	for _, key := range oracle {
		oracleCVEs[key.CVE] = struct{}{}
	}
	for _, want := range []string{"CVE-2026-0102", "CVE-2026-0103", "CVE-2026-0301", "CVE-2026-0502", "CVE-2026-0601"} {
		if _, ok := oracleCVEs[want]; !ok {
			t.Fatalf("oracle must anchor %s (hop A, hop B, case, native), got %v", want, oracleCVEs)
		}
	}

	unboundedLoaded, unboundedTruncation, _, stubU := runParityArm(t, u, false, nil)
	if !unboundedTruncation.rounds {
		t.Fatalf("unbounded chain walk should hit the round cap, truncation = %+v", unboundedTruncation)
	}
	unbounded := projectCriteriaParityKeys(BuildSupplyChainImpactFindings(unboundedLoaded))

	boundedLoaded, boundedTruncation, boundedOutcome, stubB := runParityArm(t, u, true, nil)
	if boundedTruncation.partial() {
		t.Fatalf("bounded load should converge, truncation = %+v", boundedTruncation)
	}
	if boundedOutcome.SuppressedTransitiveCriteria == 0 {
		t.Fatalf("bounded load should count suppressed transitive criteria, outcome = %+v", boundedOutcome)
	}
	bounded := projectCriteriaParityKeys(BuildSupplyChainImpactFindings(boundedLoaded))
	t.Logf("loaded oracle=%d unbounded=%d bounded=%d suppressed=%d findings oracle=%d unbounded=%d bounded=%d",
		len(u.all), len(unboundedLoaded), len(boundedLoaded),
		boundedOutcome.SuppressedTransitiveCriteria, len(oracle), len(unbounded), len(bounded))

	assertCriteriaNeverRequested(t, stubB, u.nonVulnCriteria, "non-vulnerable criteria")
	assertCriteriaNeverRequested(t, stubU, u.nonVulnCriteria, "non-vulnerable criteria")
	// Hop B starts on the component-only arm: the first derivation must not
	// put the undiscovered component's criteria on the product probe. (The
	// promotion after the component arrives is pinned by
	// TestCriteriaExpansionBoundSeedsAliasClosure.)
	if len(stubB.filters) == 0 {
		t.Fatal("bounded load recorded no filters")
	}
	if slices.Contains(stubB.filters[0].ProductCriteria, u.seedOnlyCriteria) {
		t.Fatalf("seed criteria %q on the product probe before its component loads, first filter = %+v", u.seedOnlyCriteria, stubB.filters[0])
	}
	// No component ever bears the native CVE's criteria, so it must never
	// reach the product probe in any round.
	for _, filter := range stubB.filters {
		if slices.Contains(filter.ProductCriteria, "cpe:2.3:a:native:product:1.0:*:*:*:*:*:*:*") {
			t.Fatalf("component-less seed criteria reached the product probe, filters = %+v", stubB.filters)
		}
	}

	if !reflect.DeepEqual(oracle, bounded) {
		t.Fatalf("bounded findings differ from oracle:\noracle=%#v\nbounded=%#v", oracle, bounded)
	}
}

// TestCriteriaExpansionBoundParityScopedPass proves the bound changes nothing
// under a stamp: bounded and unbounded scoped passes converge with identical
// full-key findings. The stamp blocks the chain CVEs from the CVE arm while
// criteria arms flow, so hop-A rows load in both arms but no transitive CVE
// record does.
func TestCriteriaExpansionBoundParityScopedPass(t *testing.T) {
	t.Parallel()

	u := buildCriteriaParityUniverse()
	seeds := []string{"CVE-2026-0001", "CVE-2026-0301", "CVE-2026-0502", "CVE-2026-0601"}
	scoped := &scopedSupplyChainImpactScope{cves: append([]string(nil), seeds...)}

	unboundedLoaded, unboundedTruncation, _, _ := runParityArm(t, u, false, scoped)
	if unboundedTruncation.partial() {
		t.Fatalf("scoped unbounded load should converge, truncation = %+v", unboundedTruncation)
	}
	unbounded := projectCriteriaParityKeys(BuildSupplyChainImpactFindings(unboundedLoaded))

	boundedLoaded, boundedTruncation, _, _ := runParityArm(t, u, true, scoped)
	if boundedTruncation.partial() {
		t.Fatalf("scoped bounded load should converge, truncation = %+v", boundedTruncation)
	}
	bounded := projectCriteriaParityKeys(BuildSupplyChainImpactFindings(boundedLoaded))
	t.Logf("scoped loaded unbounded=%d bounded=%d findings unbounded=%d bounded=%d",
		len(unboundedLoaded), len(boundedLoaded), len(unbounded), len(bounded))

	if len(bounded) == 0 {
		t.Fatal("scoped parity must stay non-vacuous")
	}
	if !reflect.DeepEqual(unbounded, bounded) {
		t.Fatalf("scoped findings differ with the bound on:\nunbounded=%#v\nbounded=%#v", unbounded, bounded)
	}
}

// TestCriteriaExpansionBoundAsymmetricAliasResidual is the P2 gate: the ONLY
// allowed delta is alias-group composition. CVE-T lists seed S in its
// aliases; S does not list T. The alias pass files T's record and product row
// under S's group at the index level, so the oracle holds two findings for
// the one CVE key S (one per criteria), both carrying members [S,T]. In the
// bounded arm T's rows never load, S stays ungrouped with only its own row,
// and no other key moves.
func TestCriteriaExpansionBoundAsymmetricAliasResidual(t *testing.T) {
	t.Parallel()

	const (
		cveS  = "CVE-2026-0701"
		critS = "cpe:2.3:a:scope:toaster:7.0:*:*:*:*:*:*:*"
		cveT  = "CVE-2026-0702"
		critT = "cpe:2.3:a:transitive:blender:7.0:*:*:*:*:*:*:*"
		docS  = "doc-asym-s"
		docT  = "doc-asym-t"
		digS  = "sha256:parity-asym-s"
		digT  = "sha256:parity-asym-t"
		repoS = "repo://example/asym-s"
		repoT = "repo://example/asym-t"
	)
	cveRecS := vulnerabilityCVEFact("cve-asym-s", cveS, 9.0)
	// Asymmetry: T lists S, S lists only itself.
	cveRecT := facts.Envelope{
		FactID:   "cve-asym-t",
		FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id": cveT, "advisory_id": cveT, "cvss_score": 7.0,
			"aliases": []any{cveT, cveS},
		},
	}
	prodS := vulnerabilityAffectedProductFact("prod-asym-s", cveS, critS, "match-asym-s", true)
	prodT := vulnerabilityAffectedProductFact("prod-asym-t", cveT, critT, "match-asym-t", true)
	compS := sbomComponentCPEImpactFact("comp-asym-s", docS, critS)
	compT := sbomComponentCPEImpactFact("comp-asym-t", docT, critT)
	attS := sbomAttachmentImpactFact("att-asym-s", docS, digS)
	attT := sbomAttachmentImpactFact("att-asym-t", docT, digT)
	imgS := containerImageIdentityImpactFact("img-asym-s", digS, repoS)
	imgT := containerImageIdentityImpactFact("img-asym-t", digT, repoT)
	u := criteriaParityUniverse{
		all: []facts.Envelope{cveRecS, cveRecT, prodS, prodT, compS, compT, attS, attT, imgS, imgT},
		initial: []facts.Envelope{
			cveRecS, prodS, compS, attS, imgS, attT, imgT,
		},
		nonVulnCriteria: "cpe:2.3:a:never:requested:0.0:*:*:*:*:*:*:*",
	}

	oracle := BuildSupplyChainImpactFindings(u.all)
	// Index-level folding: T's record and row file under S's CVE key, so the
	// oracle holds two findings for S (one per criteria), both grouped.
	oracleByRow := map[string]SupplyChainImpactFinding{}
	for _, f := range oracle {
		oracleByRow[f.CVEID+"\x00"+f.ProductCriteria] = f
	}
	sOwnOracle, ok := oracleByRow[cveS+"\x00"+critS]
	if !ok {
		t.Fatalf("oracle must hold the anchored seed row, got %v", findingCVEIDs(oracle))
	}
	if got := append([]string(nil), sOwnOracle.AdvisoryGroupMembers...); len(got) != 2 {
		t.Fatalf("oracle seed row must carry the asymmetric group [S T], got %v", got)
	}
	sFoldedOracle, ok := oracleByRow[cveS+"\x00"+critT]
	if !ok {
		t.Fatalf("oracle must hold the folded transitive row under the seed key, got %v", findingCVEIDs(oracle))
	}
	if got := append([]string(nil), sFoldedOracle.AdvisoryGroupMembers...); len(got) != 2 {
		t.Fatalf("oracle folded row must carry the asymmetric group [S T], got %v", got)
	}
	if len(oracle) != 2 {
		t.Fatalf("oracle must hold exactly the seed row and the folded row, got %v", findingCVEIDs(oracle))
	}

	boundedLoaded, boundedTruncation, _, _ := runParityArm(t, u, true, nil)
	if boundedTruncation.partial() {
		t.Fatalf("bounded asymmetric load should converge, truncation = %+v", boundedTruncation)
	}
	bounded := BuildSupplyChainImpactFindings(boundedLoaded)
	if len(bounded) != 1 {
		t.Fatalf("bounded load must keep exactly the seed row, got %v", findingCVEIDs(bounded))
	}
	sBounded := bounded[0]
	if sBounded.CVEID != cveS || sBounded.ProductCriteria != critS {
		t.Fatalf("bounded finding must be the seed row, got %v %q", sBounded.CVEID, sBounded.ProductCriteria)
	}
	if len(sBounded.AdvisoryGroupMembers) != 0 {
		t.Fatalf("bounded seed row must stay ungrouped, got %v", sBounded.AdvisoryGroupMembers)
	}
	// The shared row is fully equal except the group-composition family,
	// which the asymmetric alias drives.
	stripGroup := func(f SupplyChainImpactFinding) SupplyChainImpactFinding {
		f.AdvisoryGroupMembers = nil
		f.AdvisoryGroupEPSSSource = ""
		f.AdvisoryGroupKEVSource = ""
		return f
	}
	if !reflect.DeepEqual(stripGroup(sOwnOracle), stripGroup(sBounded)) {
		t.Fatalf("seed row differs beyond group composition:\noracle=%#v\nbounded=%#v", sOwnOracle, sBounded)
	}
	// The only absent row is the folded transitive one: its CVE key survives
	// in the bounded arm (same S row), so the absence is group composition
	// (the unloaded row), never a lost CVE. No bounded-only row may exist.
	for _, f := range bounded {
		if _, ok := oracleByRow[f.CVEID+"\x00"+f.ProductCriteria]; !ok {
			t.Fatalf("bounded-only row %q %q has no oracle counterpart", f.CVEID, f.ProductCriteria)
		}
	}
}

// assertCriteriaNeverRequested proves a criteria value no derivation emitted:
// neither the product nor the seed arm may carry it in any round.
func assertCriteriaNeverRequested(t *testing.T, stub *stubSupplyChainImpactFactLoader, criteria, what string) {
	t.Helper()
	for _, filter := range stub.filters {
		if slices.Contains(filter.ProductCriteria, criteria) || slices.Contains(filter.SeedProductCriteria, criteria) {
			t.Fatalf("%s %q reached a probe, filters = %+v", what, criteria, stub.filters)
		}
	}
}
