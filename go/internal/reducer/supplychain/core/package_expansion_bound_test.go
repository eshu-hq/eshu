// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// packageBoundLoader simulates the production storage split (#11097):
//   - affected_package_package_id and affected_package_purl match owned
//     PackageIDs / PURLs only (never SeedPackageIDs / SeedPURLs);
//   - consumer, SBOM, registry, and suppression probes match the union of
//     owned and seed lists;
//   - vulnerability_cve_id, vulnerability_advisory_id, and alias probes match
//     CVEIDs / AdvisoryIDs / AliasIDs as in production.
func packageBoundLoader(universe []facts.Envelope) func(SupplyChainImpactFactFilter) []facts.Envelope {
	return func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		toSet := func(lists ...[]string) map[string]struct{} {
			set := map[string]struct{}{}
			for _, list := range lists {
				for _, value := range list {
					if value != "" {
						set[value] = struct{}{}
					}
				}
			}
			return set
		}
		ownedPkg := toSet(filter.PackageIDs)
		ownedPURL := toSet(filter.PURLs)
		unionPkg := toSet(filter.PackageIDs, filter.SeedPackageIDs)
		unionPURL := toSet(filter.PURLs, filter.SeedPURLs)
		cveOK := toSet(filter.CVEIDs, filter.AdvisoryIDs)
		aliasOK := toSet(filter.AliasIDs)
		upstreamOK := toSet(filter.UpstreamCVEIDs)
		docOK := toSet(filter.DocumentIDs)
		digestOK := toSet(filter.SubjectDigests)
		repoOK := toSet(filter.RepositoryIDs)

		var out []facts.Envelope
		for _, env := range universe {
			switch env.FactKind {
			case chain.VulnerabilityCVEFactKind:
				id := supplyChainCVEID(env.Payload)
				adv := payloadcore.PayloadStr(env.Payload, "advisory_id")
				if _, ok := cveOK[id]; ok {
					out = append(out, env)
				} else if _, ok := cveOK[adv]; ok {
					out = append(out, env)
				} else if _, ok := aliasOK[id]; ok {
					out = append(out, env)
				} else if _, ok := aliasOK[adv]; ok {
					out = append(out, env)
				}
			case chain.VulnerabilityAffectedPackageFactKind:
				id := supplyChainCVEID(env.Payload)
				adv := payloadcore.PayloadStr(env.Payload, "advisory_id")
				purl := payloadcore.PayloadStr(env.Payload, "purl")
				pkgID := canonicalSupplyChainAffectedPackageID(payloadcore.PayloadStr(env.Payload, "package_id"), purl)
				if _, ok := cveOK[id]; ok {
					out = append(out, env)
				} else if _, ok := cveOK[adv]; ok {
					out = append(out, env)
				} else if _, ok := ownedPkg[pkgID]; ok {
					out = append(out, env)
				} else if _, ok := ownedPURL[purl]; ok {
					out = append(out, env)
				}
			case chain.VulnerabilityEPSSScoreFactKind, chain.VulnerabilityKnownExploitedFactKind:
				id := supplyChainCVEID(env.Payload)
				if _, ok := cveOK[id]; ok {
					out = append(out, env)
				} else if _, ok := upstreamOK[id]; ok {
					out = append(out, env)
				}
			case correlation.PackageConsumptionFactKind:
				pkgID := payloadcore.PayloadStr(env.Payload, "package_id")
				repoID := payloadcore.PayloadStr(env.Payload, "repository_id")
				if _, ok := unionPkg[pkgID]; ok {
					out = append(out, env)
				} else if _, ok := repoOK[repoID]; ok {
					out = append(out, env)
				}
			case chain.SBOMComponentFactKind:
				pkgID := payloadcore.PayloadStr(env.Payload, "package_id")
				purl := payloadcore.PayloadStr(env.Payload, "purl")
				docID := payloadcore.PayloadStr(env.Payload, "document_id")
				if _, ok := unionPkg[pkgID]; ok {
					out = append(out, env)
				} else if _, ok := unionPURL[purl]; ok {
					out = append(out, env)
				} else if _, ok := docOK[docID]; ok {
					out = append(out, env)
				}
			case reducercontract.SBOMAttestationAttachmentFactKind:
				if _, ok := docOK[payloadcore.PayloadStr(env.Payload, "document_id")]; ok {
					out = append(out, env)
				} else if _, ok := digestOK[payloadcore.PayloadStr(env.Payload, "subject_digest")]; ok {
					out = append(out, env)
				}
			case reducercontract.ContainerImageIdentityFactKind:
				if _, ok := digestOK[payloadcore.PayloadStr(env.Payload, "digest")]; ok {
					out = append(out, env)
				}
			case chain.VulnerabilitySuppressionFactKind:
				if scope := payloadcore.PayloadMap(env.Payload, "scope"); scope != nil {
					if _, ok := cveOK[payloadcore.PayloadStr(scope, "cve_id")]; ok {
						out = append(out, env)
					} else if _, ok := unionPkg[payloadcore.PayloadStr(scope, "package_id")]; ok {
						out = append(out, env)
					} else if _, ok := unionPURL[payloadcore.PayloadStr(scope, "purl")]; ok {
						out = append(out, env)
					}
				}
			}
		}
		return out
	}
}

func withScopeID(env facts.Envelope, scopeID string) facts.Envelope {
	env.ScopeID = scopeID
	return env
}

// TestPackageExpansionBoundSuppressesForeignVulnerabilityFanOut proves the
// core #11097 invariant: a vulnerability scope for CVE-A naming package P
// discovers consumers of P through SeedPackageIDs/SeedPURLs without pulling
// unrelated CVE-B (which also names P) or CVE-C (via a foreign repository's
// other package Q). Unbounded, the pass pulls CVE-B and CVE-C and stores
// copies of all three findings; bounded, it stores only CVE-A and suppresses
// the foreign repository's other package Q.
func TestPackageExpansionBoundSuppressesForeignVulnerabilityFanOut(t *testing.T) {
	t.Parallel()

	const (
		scopeA    = "vuln-intel://osv/npm/slot-01"
		scopeB    = "vuln-intel://osv/npm/slot-02"
		scopeC    = "vuln-intel://osv/npm/slot-03"
		repoScope = "git-repository-scope:repo://example/app"
		repoID    = "repo://example/app"
		pkgP      = "npm://registry.npmjs.org/shared-lib"
		pkgQ      = "npm://registry.npmjs.org/other-lib"
		cveA      = "CVE-2026-1001"
		cveB      = "CVE-2026-1002"
		cveC      = "CVE-2026-1003"
	)

	cveRecA := withScopeID(vulnerabilityCVEFact("cve-a", cveA, 9.0), scopeA)
	affA := withScopeID(vulnerabilityAffectedPackageFact("aff-a", cveA, pkgP, "npm", "shared-lib", "1.0.0", "1.2.0"), scopeA)
	cveRecB := withScopeID(vulnerabilityCVEFact("cve-b", cveB, 8.0), scopeB)
	affB := withScopeID(vulnerabilityAffectedPackageFact("aff-b", cveB, pkgP, "npm", "shared-lib", "1.0.0", "1.2.0"), scopeB)
	cveRecC := withScopeID(vulnerabilityCVEFact("cve-c", cveC, 7.0), scopeC)
	affC := withScopeID(vulnerabilityAffectedPackageFact("aff-c", cveC, pkgQ, "npm", "other-lib", "2.0.0", "2.1.0"), scopeC)
	consP := withScopeID(packageConsumptionFactWithRange("cons-p", pkgP, repoID, "1.0.0"), repoScope)
	consQ := withScopeID(packageConsumptionFactWithRange("cons-q", pkgQ, repoID, "2.0.0"), repoScope)

	universe := []facts.Envelope{cveRecA, affA, cveRecB, affB, cveRecC, affC, consP, consQ}
	initial := []facts.Envelope{cveRecA, affA}

	run := func(bound bool) ([]facts.Envelope, []SupplyChainImpactFinding, supplyChainImpactPackageBoundOutcome, supplyChainImpactTruncation, *stubSupplyChainImpactFactLoader) {
		stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
		h := SupplyChainImpactHandler{FactLoader: stub}
		if bound {
			h.PackageExpansionBound = &SupplyChainImpactPackageExpansionBound{}
		}
		loaded, trunc, bounds, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
			context.Background(), append([]facts.Envelope(nil), initial...),
			supplyChainImpactFilter(initial), newSupplyChainImpactEvidenceBudget(0), nil, scopeA,
		)
		if err != nil {
			t.Fatalf("loadActiveSupplyChainImpactFactsUntilStableBounded(bound=%v) error = %v", bound, err)
		}
		return loaded, BuildSupplyChainImpactFindings(loaded), bounds.pkg, trunc, stub
	}

	_, unboundedFindings, _, _, _ := run(false)
	if len(unboundedFindings) != 3 {
		t.Fatalf("unbounded pass should leak all 3 findings (A, B, C), got %d: %v", len(unboundedFindings), findingCVEIDs(unboundedFindings))
	}

	boundedLoaded, boundedFindings, outcome, trunc, stub := run(true)
	if trunc.partial() {
		t.Fatalf("bounded pass must converge, truncation = %+v", trunc)
	}
	if outcome.Loads > maxSupplyChainImpactActiveEvidenceLoads {
		t.Fatalf("bounded loads = %d, want <= %d", outcome.Loads, maxSupplyChainImpactActiveEvidenceLoads)
	}
	if got := findingCVEIDs(boundedFindings); !slices.Equal(got, []string{cveA}) {
		t.Fatalf("bounded findings = %v, want only seed [%s] (no foreign CVE-B or CVE-C copies)", got, cveA)
	}
	// consQ arrived via repository_scope for repoID; its foreign package Q must
	// be suppressed rather than promoted into PackageIDs or SeedPackageIDs.
	if outcome.SuppressedTransitivePackages == 0 {
		t.Fatalf("expected foreign repository package Q to be suppressed, outcome = %+v", outcome)
	}
	for _, f := range stub.filters {
		if slices.Contains(f.PackageIDs, pkgP) || slices.Contains(f.PackageIDs, pkgQ) {
			t.Fatalf("seed/foreign packages must never ride owned PackageIDs, filters = %+v", stub.filters)
		}
		if slices.Contains(f.SeedPackageIDs, pkgQ) {
			t.Fatalf("foreign consumer package Q must never ride SeedPackageIDs, filters = %+v", stub.filters)
		}
	}
	if ids := loadedFactIDs(boundedLoaded); containsID(ids, "aff-b") || containsID(ids, "aff-c") {
		t.Fatalf("bounded load leaked foreign advisory facts, got %v", ids)
	}
}

// TestPackageExpansionBoundPreservesOwnedConsumerAndOwnDigestSBOM proves that
// owned consumer facts (own-scope PackageConsumption, own-scope SBOMComponent,
// and an SBOM component in a document attached to an own-scope image digest)
// stay on PackageIDs/PURLs and pull every matching vulnerability, while a
// foreign SBOM document or foreign suppression never promotes foreign packages.
func TestPackageExpansionBoundPreservesOwnedConsumerAndOwnDigestSBOM(t *testing.T) {
	t.Parallel()

	const (
		imageScope   = "oci-image://example/app"
		sbomScope    = "sbom://example/app"
		foreignScope = "sbom://example/other"
		vulnScope    = "vuln-intel://osv/npm/slot-01"
		ownDigest    = "sha256:own-image-digest"
		ownDoc       = "doc-own-sbom"
		foreignDoc   = "doc-foreign-sbom"
		pkgOwn       = "npm://registry.npmjs.org/own-dep"
		purlOwn      = "pkg:npm/own-dep"
		pkgForeign   = "npm://registry.npmjs.org/foreign-dep"
		purlForeign  = "pkg:npm/foreign-dep"
		cveOwn       = "CVE-2026-2001"
		cveForeign   = "CVE-2026-2002"
	)

	img := withScopeID(containerImageIdentityImpactFact("img-own", ownDigest, "repo://example/app"), imageScope)
	attOwn := withScopeID(sbomAttachmentImpactFact("att-own", ownDoc, ownDigest), sbomScope)
	compOwn := withScopeID(sbomComponentImpactFactWithPackageIDVersion("comp-own", ownDoc, purlOwn, pkgOwn, "1.0.0"), sbomScope)
	compForeign := withScopeID(sbomComponentImpactFactWithPackageIDVersion("comp-foreign", foreignDoc, purlForeign, pkgForeign, "1.0.0"), foreignScope)
	cveRecOwn := withScopeID(vulnerabilityCVEFact("cve-own", cveOwn, 8.5), vulnScope)
	affOwn := withScopeID(vulnerabilityAffectedPackageFact("aff-own", cveOwn, pkgOwn, "npm", "own-dep", "1.0.0", "1.1.0"), vulnScope)
	cveRecForeign := withScopeID(vulnerabilityCVEFact("cve-foreign", cveForeign, 7.5), vulnScope)
	affForeign := withScopeID(vulnerabilityAffectedPackageFact("aff-foreign", cveForeign, pkgForeign, "npm", "foreign-dep", "1.0.0", "1.1.0"), vulnScope)
	suppForeign := withScopeID(facts.Envelope{
		FactID:   "supp-foreign",
		FactKind: chain.VulnerabilitySuppressionFactKind,
		Payload: map[string]any{
			"scope": map[string]any{
				"cve_id":     cveOwn,
				"package_id": pkgForeign,
				"purl":       purlForeign,
			},
		},
	}, foreignScope)

	universe := []facts.Envelope{img, attOwn, compOwn, compForeign, cveRecOwn, affOwn, cveRecForeign, affForeign, suppForeign}
	initial := []facts.Envelope{img}

	stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
	h := SupplyChainImpactHandler{
		FactLoader:            stub,
		PackageExpansionBound: &SupplyChainImpactPackageExpansionBound{},
	}
	loaded, trunc, bounds, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
		context.Background(), initial, supplyChainImpactFilter(initial),
		newSupplyChainImpactEvidenceBudget(0), nil, imageScope,
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	if trunc.partial() {
		t.Fatalf("image pass must converge, truncation = %+v", trunc)
	}
	if bounds.pkg.Loads > maxSupplyChainImpactActiveEvidenceLoads {
		t.Fatalf("loads = %d, want <= %d", bounds.pkg.Loads, maxSupplyChainImpactActiveEvidenceLoads)
	}
	findings := BuildSupplyChainImpactFindings(loaded)
	if got := findingCVEIDs(findings); !slices.Equal(got, []string{cveOwn}) {
		t.Fatalf("own-digest SBOM findings = %v, want [%s]", got, cveOwn)
	}
	// suppForeign arrived via cveOwn, carrying pkgForeign/purlForeign: must be
	// suppressed, never promoted into PackageIDs or SeedPackageIDs.
	for _, f := range stub.filters {
		if slices.Contains(f.PackageIDs, pkgForeign) || slices.Contains(f.SeedPackageIDs, pkgForeign) {
			t.Fatalf("foreign suppression package must not drive probes, filters = %+v", stub.filters)
		}
		if slices.Contains(f.PURLs, purlForeign) || slices.Contains(f.SeedPURLs, purlForeign) {
			t.Fatalf("foreign suppression PURL must not drive probes, filters = %+v", stub.filters)
		}
	}
	if bounds.pkg.SuppressedTransitivePackages == 0 {
		t.Fatalf("expected foreign suppression package/PURL to be counted as suppressed, outcome = %+v", bounds.pkg)
	}
}

// TestPackageExpansionBoundSeedsAliasAndAdvisoryClosure proves that alias
// closure and advisory_id-only seeds stay seeded so aliased affected packages
// discover consumers on SeedPackageIDs while staying off PackageIDs.
func TestPackageExpansionBoundSeedsAliasAndAdvisoryClosure(t *testing.T) {
	t.Parallel()

	const (
		scopeA    = "vuln-intel://ghsa/window-1"
		scopeB    = "vuln-intel://osv/npm/slot-2"
		repoScope = "git-repository-scope:repo://example/svc"
		repoID    = "repo://example/svc"
		ghsaID    = "GHSA-aaaa-bbbb-cccc"
		cveID     = "CVE-2026-3001"
		pkgA      = "npm://registry.npmjs.org/alias-pkg"
	)

	cveGHSA := withScopeID(facts.Envelope{
		FactID:   "cve-ghsa",
		FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":      ghsaID,
			"advisory_id": ghsaID,
			"cvss_score":  8.0,
			"aliases":     []any{ghsaID, cveID},
		},
	}, scopeA)
	cveNVD := withScopeID(vulnerabilityCVEFact("cve-nvd", cveID, 8.2), scopeB)
	affNVD := withScopeID(vulnerabilityAffectedPackageFact("aff-nvd", cveID, pkgA, "npm", "alias-pkg", "1.0.0", "1.1.0"), scopeB)
	cons := withScopeID(packageConsumptionFactWithRange("cons-alias", pkgA, repoID, "1.0.0"), repoScope)

	universe := []facts.Envelope{cveGHSA, cveNVD, affNVD, cons}
	initial := []facts.Envelope{cveGHSA}

	stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
	h := SupplyChainImpactHandler{
		FactLoader:            stub,
		PackageExpansionBound: &SupplyChainImpactPackageExpansionBound{},
	}
	loaded, trunc, bounds, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
		context.Background(), initial, supplyChainImpactFilter(initial),
		newSupplyChainImpactEvidenceBudget(0), nil, scopeA,
	)
	if err != nil {
		t.Fatalf("load error = %v", err)
	}
	if trunc.partial() {
		t.Fatalf("alias closure pass must converge, truncation = %+v", trunc)
	}
	if !containsID(loadedFactIDs(loaded), "cons-alias") {
		t.Fatalf("alias-seeded affected package must discover consumer via SeedPackageIDs, loaded = %v", loadedFactIDs(loaded))
	}
	for _, f := range stub.filters {
		if slices.Contains(f.PackageIDs, pkgA) {
			t.Fatalf("alias-seeded language package must ride SeedPackageIDs, not owned PackageIDs, filters = %+v", stub.filters)
		}
	}
	if bounds.pkg.SeedCVECount < 2 {
		t.Fatalf("expected both %s and %s in seed set, got count %d", ghsaID, cveID, bounds.pkg.SeedCVECount)
	}
}

// TestPackageExpansionBoundScopedPassPartitionsOSAndNonOSSeeds proves that a
// scoped rerun (#9779) keeps OS-class finding seeds on PackageIDs/PURLs (for
// sibling consolidation) while routing non-OS finding seeds to
// SeedPackageIDs/SeedPURLs (preventing foreign-CVE expansion during scoped
// holder reruns).
func TestPackageExpansionBoundScopedPassPartitionsOSAndNonOSSeeds(t *testing.T) {
	t.Parallel()

	const (
		holderScope = "vuln-intel://first-epss/shard-01"
		osPkg       = "os://debian/openssl"
		osPURL      = "pkg:deb/debian/openssl"
		npmPkg      = "npm://registry.npmjs.org/lodash"
		npmPURL     = "pkg:npm/lodash"
		cveOS       = "CVE-2026-4001"
		cveNPM      = "CVE-2026-4002"
	)

	scoped, ok := seedScopedSupplyChainImpactScope(
		nil,
		[]ScopedFindingPackage{
			{CVEID: cveOS, AdvisoryID: "DSA-9001-1", PackageID: osPkg, PURL: osPURL, PackageName: "openssl", Ecosystem: "os"},
			{CVEID: cveNPM, AdvisoryID: cveNPM, PackageID: npmPkg, PURL: npmPURL, PackageName: "lodash", Ecosystem: "npm"},
		},
		[]string{cveOS, cveNPM},
	)
	if !ok {
		t.Fatal("seedScopedSupplyChainImpactScope returned !ok")
	}

	h := SupplyChainImpactHandler{PackageExpansionBound: &SupplyChainImpactPackageExpansionBound{}}
	initial := h.scopedSupplyChainImpactFilter(nil, scoped)
	state := newPackageExpansionBoundState(initial, scoped, holderScope)
	next, _ := state.apply(initial, nil, nil, SupplyChainImpactFactFilter{})

	if !slices.Equal(next.PackageIDs, []string{osPkg}) {
		t.Fatalf("scoped owned PackageIDs = %v, want OS seed [%s]", next.PackageIDs, osPkg)
	}
	if !slices.Equal(next.PURLs, []string{osPURL}) {
		t.Fatalf("scoped owned PURLs = %v, want OS seed [%s]", next.PURLs, osPURL)
	}
	if !slices.Equal(next.SeedPackageIDs, []string{npmPkg}) {
		t.Fatalf("scoped SeedPackageIDs = %v, want non-OS seed [%s]", next.SeedPackageIDs, npmPkg)
	}
	if !slices.Equal(next.SeedPURLs, []string{npmPURL}) {
		t.Fatalf("scoped SeedPURLs = %v, want non-OS seed [%s]", next.SeedPURLs, npmPURL)
	}

	restricted := restrictSupplyChainImpactFilter(SupplyChainImpactFactFilter{
		SeedPackageIDs: []string{npmPkg, "npm://registry.npmjs.org/unseeded"},
		SeedPURLs:      []string{npmPURL, "pkg:npm/unseeded"},
	}, scoped)
	if !slices.Equal(restricted.SeedPackageIDs, []string{npmPkg}) {
		t.Fatalf("restrictSupplyChainImpactFilter SeedPackageIDs = %v, want [%s]", restricted.SeedPackageIDs, npmPkg)
	}
	if !slices.Equal(restricted.SeedPURLs, []string{npmPURL}) {
		t.Fatalf("restrictSupplyChainImpactFilter SeedPURLs = %v, want [%s]", restricted.SeedPURLs, npmPURL)
	}
}

// TestPackageExpansionBoundOSSiblingRetractedVsLiveParity proves a withdrawn
// distro OS advisory in scope A pulls its live sibling in scope B via the
// own-scope 1-hop OS exemption so retraction and deciding advisory match
// identically before and after (#11097 AC #4).
func TestPackageExpansionBoundOSSiblingRetractedVsLiveParity(t *testing.T) {
	t.Parallel()
	const (
		scopeW   = "vuln-intel://osv/debian/slot-10"
		scopeL   = "vuln-intel://osv/debian/slot-11"
		pkgID    = "os://debian/openssl"
		purl     = "pkg:deb/debian/openssl"
		upstream = "CVE-2026-6101"
	)
	ranges := []any{map[string]any{
		"type": "ECOSYSTEM", "ecosystem": "Debian:12", "introduced": "0", "fixed": "3.0.14-1~deb12u2",
		"events": []any{map[string]any{"introduced": "0"}, map[string]any{"fixed": "3.0.14-1~deb12u2"}},
	}}
	cveW := withScopeID(facts.Envelope{
		FactID: "cve-dla-w", FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{"cve_id": "DLA-6101-1", "advisory_id": "DLA-6101-1", "source": "osv", "cvss_score": 7.5, "withdrawn_at": "2026-06-05T00:00:00Z", "upstream": []any{upstream}},
	}, scopeW)
	affW := withScopeID(facts.Envelope{
		FactID: "aff-dla-w", FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{"cve_id": "DLA-6101-1", "advisory_id": "DLA-6101-1", "source": "osv", "ecosystem": "os", "package_id": pkgID, "purl": purl, "package": "openssl", "ranges": ranges},
	}, scopeW)
	cveL := withScopeID(facts.Envelope{
		FactID: "cve-dsa-l", FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{"cve_id": "DSA-6101-1", "advisory_id": "DSA-6101-1", "source": "osv", "cvss_score": 8.0, "upstream": []any{upstream}},
	}, scopeL)
	affL := withScopeID(facts.Envelope{
		FactID: "aff-dsa-l", FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{"cve_id": "DSA-6101-1", "advisory_id": "DSA-6101-1", "source": "osv", "ecosystem": "os", "package_id": pkgID, "purl": purl, "package": "openssl", "ranges": ranges},
	}, scopeL)
	installed := osPackageFact("os-pkg-ret", "scanner-worker://image/debian-12", map[string]any{
		"distro": "debian", "distro_version": "12", "package_manager": "dpkg", "name": "openssl",
		"source_package": "openssl", "arch": "amd64", "repository_class": "vendor",
		"vendor_advisory_source": "debian", "installed_version_raw": "3.0.14-1~deb12u1",
		"purl": "pkg:deb/debian/openssl@3.0.14-1~deb12u1?arch=amd64&distro=debian-12",
	})
	universe := []facts.Envelope{cveW, affW, cveL, affL}
	initial := []facts.Envelope{cveW, affW}
	run := func(bound bool) []osSiblingKey {
		stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
		h := SupplyChainImpactHandler{FactLoader: stub}
		if bound {
			h.PackageExpansionBound = &SupplyChainImpactPackageExpansionBound{}
		}
		loaded, trunc, _, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
			context.Background(), append([]facts.Envelope(nil), initial...),
			supplyChainImpactFilter(initial), newSupplyChainImpactEvidenceBudget(0), nil, scopeW,
		)
		if err != nil || trunc.partial() {
			t.Fatalf("bound=%v err=%v trunc=%+v", bound, err, trunc)
		}
		return projectOSSiblingKeys(BuildSupplyChainImpactFindings(append(loaded, installed)))
	}
	unbounded, bounded := run(false), run(true)
	if len(bounded) != 1 || bounded[0].AdvisoryRetract || bounded[0].AdvisoryID != "DSA-6101-1" {
		t.Fatalf("expected live sibling DSA-6101-1 to prevent false retraction, got %+v", bounded)
	}
	if !slices.Equal(unbounded, bounded) {
		t.Fatalf("retracted-vs-live sibling mismatch:\nunbounded=%+v\nbounded=%+v", unbounded, bounded)
	}
}
