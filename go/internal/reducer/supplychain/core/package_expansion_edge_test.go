// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
)

// TestPackageExpansionBoundOwnedSBOMVersionedPURLKeepsVersionlessSuppression is
// the F1 regression: an owned SBOM component carries a version-qualified PURL
// ("pkg:npm/express@4.17.0"), Round 1 loads an affected_package row carrying
// the versionless PURL ("pkg:npm/express"), and Round 2 must keep the
// versionless PURL on owned PURLs so a suppression scoped to
// {purl: "pkg:npm/express"} loads and suppresses the finding with zero false
// transitive suppressions.
func TestPackageExpansionBoundOwnedSBOMVersionedPURLKeepsVersionlessSuppression(t *testing.T) {
	t.Parallel()

	const (
		sbomScope = "sbom://example/web"
		vulnScope = "vuln-intel://osv/npm/slot-01"
		suppScope = "operator:vulnerability_suppressions"
		docID     = "doc-web-1"
		digest    = "sha256:web-img-1"
		repoID    = "repo://example/web"
		pkgID     = "npm://registry.npmjs.org/express"
		verPURL   = "pkg:npm/express@4.17.0"
		basePURL  = "pkg:npm/express"
		cveID     = "CVE-2026-8101"
	)

	comp := withScopeID(sbomComponentImpactFactWithPackageIDVersion("comp-express", docID, verPURL, pkgID, "4.17.0"), sbomScope)
	att := withScopeID(sbomAttachmentImpactFact("att-web", docID, digest), sbomScope)
	img := withScopeID(containerImageIdentityImpactFact("img-web", digest, repoID), sbomScope)
	cveRec := withScopeID(vulnerabilityCVEFact("cve-8101", cveID, 8.5), vulnScope)
	aff := withScopeID(facts.Envelope{
		FactID:   "aff-8101",
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":        cveID,
			"advisory_id":   cveID,
			"source":        "osv",
			"ecosystem":     "npm",
			"package_id":    pkgID,
			"purl":          basePURL,
			"package":       "express",
			"fixed_version": "4.19.2",
			"ranges": []any{map[string]any{
				"type": "SEMVER", "introduced": "4.0.0", "fixed": "4.19.2",
				"events": []any{map[string]any{"introduced": "4.0.0"}, map[string]any{"fixed": "4.19.2"}},
			}},
		},
	}, vulnScope)
	supp := withScopeID(suppressionFactEnvelope(
		"supp-express-base-purl",
		chain.VulnerabilitySuppressionSourcePolicy,
		chain.VulnerabilitySuppressionJustificationNotAffected,
		"secops@example.com",
		"2026-06-01T00:00:00Z",
		"",
		map[string]any{"purl": basePURL},
	), suppScope)
	// A second vulnerability naming express ONLY via versionless purl (empty
	// package_id) proves Round 2 keeps basePURL on owned PURLs (not suppressed
	// and not demoted to SeedPURLs).
	const cvePURLOnly = "CVE-2026-8102"
	cveRec2 := withScopeID(vulnerabilityCVEFact("cve-8102", cvePURLOnly, 7.2), vulnScope)
	affPURLOnly := withScopeID(facts.Envelope{
		FactID:   "aff-8102-purl-only",
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id": cvePURLOnly, "advisory_id": cvePURLOnly, "source": "osv",
			"ecosystem": "npm", "purl": basePURL, "package": "express",
		},
	}, vulnScope)

	universe := []facts.Envelope{comp, att, img, cveRec, aff, cveRec2, affPURLOnly, supp}
	initial := []facts.Envelope{comp, att, img}
	stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
	h := SupplyChainImpactHandler{
		FactLoader:            stub,
		PackageExpansionBound: &SupplyChainImpactPackageExpansionBound{},
	}
	loaded, trunc, bounds, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
		context.Background(), initial, supplyChainImpactFilter(initial),
		newSupplyChainImpactEvidenceBudget(0), nil, sbomScope,
	)
	if err != nil || trunc.partial() {
		t.Fatalf("load failed: err=%v trunc=%+v", err, trunc)
	}
	if !containsID(loadedFactIDs(loaded), "supp-express-base-purl") {
		t.Fatalf("versionless PURL suppression was dropped, loaded=%v filters=%+v", loadedFactIDs(loaded), stub.filters)
	}
	if !containsID(loadedFactIDs(loaded), "aff-8102-purl-only") {
		t.Fatalf("versionless PURL advisory was dropped from owned PURLs arm, loaded=%v filters=%+v", loadedFactIDs(loaded), stub.filters)
	}
	if bounds.pkg.SuppressedTransitivePackages != 0 {
		t.Fatalf("owned versionless PURL must not count as suppressed transitive, got %d", bounds.pkg.SuppressedTransitivePackages)
	}
	suppressions, quarantined, err := BuildVulnerabilitySuppressions(loaded)
	if err != nil || len(quarantined) != 0 || len(suppressions) != 1 {
		t.Fatalf("BuildVulnerabilitySuppressions: err=%v quarantined=%d suppressions=%d", err, len(quarantined), len(suppressions))
	}
	if suppressions[0].Scope.PURL != basePURL {
		t.Fatalf("loaded suppression Scope.PURL = %q, want %q", suppressions[0].Scope.PURL, basePURL)
	}
	// Prove suppression evaluation against a finding carrying the versionless PURL.
	findings := []SupplyChainImpactFinding{{CVEID: cveID, PackageID: pkgID, PURL: basePURL}}
	applySupplyChainImpactSuppressions(findings, suppressions, time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC))
	if findings[0].Suppression.State != SupplyChainSuppressionStateNotAffected {
		t.Fatalf("expected not_affected finding via versionless PURL, got %+v", findings[0].Suppression)
	}
}

// TestPackageExpansionBoundMatchesASCIICaseAndWhitespaceNormalization proves
// package/PURL provenance matching folds ASCII case and surrounding space like
// packageAnchorEqual (scope.go:297) while preserving the stored spelling and
// keeping Unicode lookalikes (Kelvin sign U+212A) distinct (#9584).
func TestPackageExpansionBoundMatchesASCIICaseAndWhitespaceNormalization(t *testing.T) {
	t.Parallel()

	const ownScope = "sbom://example/norm"
	envelopes := []facts.Envelope{
		withScopeID(sbomComponentImpactFactWithPackageIDVersion(
			"comp-norm", "doc-1", "  PKG:NPM/Express@4.17.0  ", "  NPM://Registry.NPMJS.Org/Express  ", "4.17.0",
		), ownScope),
	}
	filter := SupplyChainImpactFactFilter{
		PackageIDs: []string{"npm://registry.npmjs.org/express", "npm://registry.npmjs.org/\u212aey"},
		PURLs:      []string{"pkg:npm/express", "pkg:npm/express@4.17.0"},
	}
	state := newPackageExpansionBoundState(filter, nil, ownScope)
	next, suppressed := state.apply(filter, envelopes, envelopes, SupplyChainImpactFactFilter{})
	if !slices.Equal(next.PackageIDs, []string{"npm://registry.npmjs.org/express"}) {
		t.Fatalf("owned PackageIDs = %v, want exact stored spelling for ASCII-folded match only", next.PackageIDs)
	}
	if !slices.Equal(next.PURLs, []string{"pkg:npm/express", "pkg:npm/express@4.17.0"}) {
		t.Fatalf("owned PURLs = %v, want both versionless and versioned stored spellings", next.PURLs)
	}
	if suppressed != 1 {
		t.Fatalf("suppressed = %d, want 1 (unowned Kelvin-sign lookalike suppressed)", suppressed)
	}
}

// TestPackageExpansionBoundForeignAlertAndSuppressionOnSeedPackageDoNotLeakCVE
// is the F5 regression:
//  1. On a seed-only vulnerability scope for CVE-A naming pkgP, foreign
//     security_alert and vulnerability.suppression rows loaded via
//     SeedPackageIDs for (pkgP, CVE-B) / (pkgP, CVE-C) must NOT widen
//     next.CVEIDs or store foreign CVE-B / CVE-C findings in CVE-A's scope.
//  2. On an owned repository scope that owns pkgP, a foreign security_alert
//     for (pkgP, CVE-B) MUST still widen CVEIDs and load CVE-B.
func TestPackageExpansionBoundForeignAlertAndSuppressionOnSeedPackageDoNotLeakCVE(t *testing.T) {
	t.Parallel()

	const (
		scopeA     = "vuln-intel://osv/npm/slot-a"
		scopeB     = "vuln-intel://osv/npm/slot-b"
		scopeC     = "vuln-intel://osv/npm/slot-c"
		alertScope = "security-alert://repo/example/app"
		suppScope  = "operator:vulnerability_suppressions"
		repoScope  = "git-repository-scope:repo://example/app"
		repoID     = "repo://example/app"
		pkgP       = "npm://registry.npmjs.org/shared-pkg"
		cveA       = "CVE-2026-8201"
		cveB       = "CVE-2026-8202"
		cveC       = "CVE-2026-8203"
	)

	cveRecA := withScopeID(vulnerabilityCVEFact("cve-a", cveA, 9.0), scopeA)
	affA := withScopeID(vulnerabilityAffectedPackageFact("aff-a", cveA, pkgP, "npm", "shared-pkg", "1.0.0", "1.1.0"), scopeA)
	cveRecB := withScopeID(vulnerabilityCVEFact("cve-b", cveB, 8.0), scopeB)
	affB := withScopeID(vulnerabilityAffectedPackageFact("aff-b", cveB, pkgP, "npm", "shared-pkg", "1.0.0", "1.1.0"), scopeB)
	cveRecC := withScopeID(vulnerabilityCVEFact("cve-c", cveC, 7.0), scopeC)
	affC := withScopeID(vulnerabilityAffectedPackageFact("aff-c", cveC, pkgP, "npm", "shared-pkg", "1.0.0", "1.1.0"), scopeC)
	consP := withScopeID(packageConsumptionFactWithRange("cons-p", pkgP, repoID, "1.0.0"), repoScope)
	foreignAlertB := withScopeID(facts.Envelope{
		FactID:   "alert-b",
		FactKind: facts.SecurityAlertRepositoryAlertFactKind,
		Payload: map[string]any{
			"package_id":    pkgP,
			"cve_id":        cveB,
			"repository_id": repoID,
			"state":         "open",
		},
	}, alertScope)
	foreignSuppC := withScopeID(facts.Envelope{
		FactID:   "supp-c",
		FactKind: chain.VulnerabilitySuppressionFactKind,
		Payload: map[string]any{
			"scope": map[string]any{
				"package_id": pkgP,
				"cve_id":     cveC,
			},
		},
	}, suppScope)

	universe := []facts.Envelope{cveRecA, affA, cveRecB, affB, cveRecC, affC, consP, foreignAlertB, foreignSuppC}
	loaderWithAlerts := func(filter SupplyChainImpactFactFilter) []facts.Envelope {
		out := packageBoundLoader(universe)(filter)
		unionPkg := map[string]struct{}{}
		for _, id := range filter.PackageIDs {
			unionPkg[id] = struct{}{}
		}
		for _, id := range filter.SeedPackageIDs {
			unionPkg[id] = struct{}{}
		}
		if _, ok := unionPkg[pkgP]; ok {
			out = append(out, foreignAlertB)
		}
		return out
	}

	// 1. Seed-only CVE-A pass must NOT widen to CVE-B or CVE-C.
	stubA := &stubSupplyChainImpactFactLoader{activeForFilter: loaderWithAlerts}
	hA := SupplyChainImpactHandler{
		FactLoader:            stubA,
		PackageExpansionBound: &SupplyChainImpactPackageExpansionBound{},
	}
	initialA := []facts.Envelope{cveRecA, affA}
	loadedA, truncA, _, err := hA.loadActiveSupplyChainImpactFactsUntilStableBounded(
		context.Background(), initialA, supplyChainImpactFilter(initialA),
		newSupplyChainImpactEvidenceBudget(0), nil, scopeA,
	)
	if err != nil || truncA.partial() {
		t.Fatalf("seed pass failed: err=%v trunc=%+v", err, truncA)
	}
	if ids := loadedFactIDs(loadedA); containsID(ids, "aff-b") || containsID(ids, "aff-c") {
		t.Fatalf("seed-only pass leaked foreign alert/suppression CVEs: %v", ids)
	}
	if got := findingCVEIDs(BuildSupplyChainImpactFindings(loadedA)); !slices.Equal(got, []string{cveA}) {
		t.Fatalf("seed-only pass findings = %v, want only [%s]", got, cveA)
	}

	// 2. Owned repo pass owning pkgP MUST still follow foreignAlertB to CVE-B.
	stubR := &stubSupplyChainImpactFactLoader{activeForFilter: loaderWithAlerts}
	hR := SupplyChainImpactHandler{
		FactLoader:            stubR,
		PackageExpansionBound: &SupplyChainImpactPackageExpansionBound{},
	}
	initialR := []facts.Envelope{consP}
	loadedR, truncR, _, err := hR.loadActiveSupplyChainImpactFactsUntilStableBounded(
		context.Background(), initialR, supplyChainImpactFilter(initialR),
		newSupplyChainImpactEvidenceBudget(0), nil, repoScope,
	)
	if err != nil || truncR.partial() {
		t.Fatalf("owned repo pass failed: err=%v trunc=%+v", err, truncR)
	}
	if !containsID(loadedFactIDs(loadedR), "cve-b") {
		t.Fatalf("owned repo pass must still follow foreign alert on owned package to CVE-B, got %v", loadedFactIDs(loadedR))
	}
}
