// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"strings"
	"testing"
)

// runRouteServesDataChecks runs the full #5584 gate — map cross-check,
// structural registry verification, anti-poison scan, and signature
// closure — against the given maps and returns every finding. Extracted so
// the honest-state test and the BITES tests exercise the SAME production
// checks (golang-engineering: negative tests must break the real path, not
// a copy).
func runRouteServesDataChecks(
	t *testing.T,
	backing map[string]routeServesDataBacking,
	registry map[string]routeServesDataSource,
	signatures map[string]domainDataSignature,
) []string {
	t.Helper()
	repoRoot := kindConsumerGateRepoRoot(t)
	var findings []string
	findings = append(findings, crossCheckRouteServesDataMap(backing, registry)...)
	findings = append(findings, verifyRouteServesDataRegistry(repoRoot, registry, signatures)...)
	findings = append(findings, verifyRouteServesDataScan(repoRoot, registry, signatures)...)
	findings = append(findings, verifyDomainSignaturesClosed(backing, signatures)...)
	return findings
}

// TestRouteServesDataRegistryHonestStateGreen is the #5584 D1
// generalization gate: routeServesDataBackingMap is no longer
// self-certifying — every route's ServedDomains must exactly match the
// handler-derived, source-verified routeServesDataRegistry, every registry
// citation must exist verbatim in the cited file, and no route's read path
// may contain a foreign domain's signature without a reviewed disclosure.
func TestRouteServesDataRegistryHonestStateGreen(t *testing.T) {
	findings := runRouteServesDataChecks(t, routeServesDataBackingMap, routeServesDataRegistry, domainDataSignatures)
	for _, f := range findings {
		t.Errorf("%s", f)
	}
}

// poisonedBackingCopy returns a deep copy of backing with foreignDomain
// appended to route's ServedDomains.
func poisonedBackingCopy(backing map[string]routeServesDataBacking, route, foreignDomain string) map[string]routeServesDataBacking {
	out := make(map[string]routeServesDataBacking, len(backing))
	for r, b := range backing {
		domains := append([]string(nil), b.ServedDomains...)
		if r == route {
			domains = append(domains, foreignDomain)
		}
		out[r] = routeServesDataBacking{ServedDomains: domains}
	}
	return out
}

// TestRouteServesDataRegistryBITES_PoisonedMapGoesRed proves the exact
// bypass codex flagged on #5583 is closed for EVERY route: appending any
// foreign domain to any route's ServedDomains — the documented remediation
// that used to be able to paper over a real misrouting — now contradicts
// the handler-derived registry and turns the gate RED. The oracle is the
// registry cross-check, whose own claims are verified against real handler
// source, not the map itself.
func TestRouteServesDataRegistryBITES_PoisonedMapGoesRed(t *testing.T) {
	for _, route := range sortedKeys(routeServesDataBackingMap) {
		served := map[string]bool{}
		for _, d := range routeServesDataBackingMap[route].ServedDomains {
			served[d] = true
		}
		for _, foreign := range sortedKeys(domainDataSignatures) {
			if served[foreign] {
				continue
			}
			poisoned := poisonedBackingCopy(routeServesDataBackingMap, route, foreign)
			findings := crossCheckRouteServesDataMap(poisoned, routeServesDataRegistry)
			if len(findings) == 0 {
				t.Fatalf("BITES FAILED: poisoning routeServesDataBackingMap[%q].ServedDomains with %q was not detected — the map is self-certifying again", route, foreign)
			}
			if !strings.Contains(strings.Join(findings, "\n"), foreign) {
				t.Fatalf("BITES: poisoning %q with %q was detected but no finding names the poisoned domain: %v", route, foreign, findings)
			}
		}
	}
}

// TestRouteServesDataRegistryBITES_PoisonedRegistryGoesRed proves the
// registry itself cannot be co-poisoned to launder a false map entry: a
// fabricated Served claim must survive verification against the REAL
// handler source, which it cannot, because the claimed store field does not
// exist on the handler struct and the claimed markers do not appear in the
// route's files. This is the map-independent tooth (mirrors the #5474
// kind_real_consumer rebuild: real source is the oracle).
func TestRouteServesDataRegistryBITES_PoisonedRegistryGoesRed(t *testing.T) {
	repoRoot := kindConsumerGateRepoRoot(t)

	// Seed the historical #5480 shape one layer deeper than the map:
	// claim GET /api/v0/images serves kubernetes_correlation, with the
	// evidence that would be TRUE for the kubernetes route.
	t.Run("fabricated_served_claim_fails_source_verification", func(t *testing.T) {
		poisoned := map[string]routeServesDataSource{}
		for r, e := range routeServesDataRegistry {
			poisoned[r] = e
		}
		images := poisoned["GET /api/v0/images"]
		images.Served = append(append([]routeServedDomain(nil), images.Served...), routeServedDomain{
			Domain:     "kubernetes_correlation",
			StoreField: "Correlations",
			StoreType:  "KubernetesCorrelationStore",
			Evidence: []routeReadEvidence{
				{File: "go/internal/query/images.go", Marker: "reducer_kubernetes_correlation"},
			},
		})
		poisoned["GET /api/v0/images"] = images

		findings := verifyRouteServesDataRegistry(repoRoot, poisoned, domainDataSignatures)
		joined := strings.Join(findings, "\n")
		if !strings.Contains(joined, "no field named \"Correlations\"") {
			t.Errorf("BITES FAILED: fabricated store field on ImageHandler was not rejected against real source: %v", findings)
		}
		if !strings.Contains(joined, "does not contain the cited marker") {
			t.Errorf("BITES FAILED: fabricated marker citation in images.go was not rejected against real source: %v", findings)
		}
	})

	// Dodge attempt: move a genuinely-served domain to MapOnly so it needs
	// no evidence. The anti-poison scan catches the contradiction — the
	// route's read path still contains the domain's signature while the
	// pair is no longer Served or Disclosed.
	t.Run("map_only_dodge_contradicts_scan", func(t *testing.T) {
		poisoned := map[string]routeServesDataSource{}
		for r, e := range routeServesDataRegistry {
			poisoned[r] = e
		}
		k8s := poisoned["GET /api/v0/kubernetes/correlations"]
		k8s.Served = nil
		k8s.MapOnly = []routeMapOnlyClaim{{Domain: "kubernetes_correlation", Reason: "dodge"}}
		poisoned["GET /api/v0/kubernetes/correlations"] = k8s

		findings := verifyRouteServesDataScan(repoRoot, poisoned, domainDataSignatures)
		joined := strings.Join(findings, "\n")
		if !strings.Contains(joined, "reducer_kubernetes_correlation") {
			t.Errorf("BITES FAILED: MapOnly dodge for a genuinely-served domain was not contradicted by the scan: %v", findings)
		}
	})

	// Dropped disclosure: removing the reviewed codeowners enrichment
	// disclosure must turn the scan RED, because the handler really does
	// wire ServiceCatalogCorrelationStore into listOwnership.
	t.Run("dropped_disclosure_contradicts_scan", func(t *testing.T) {
		poisoned := map[string]routeServesDataSource{}
		for r, e := range routeServesDataRegistry {
			poisoned[r] = e
		}
		codeowners := poisoned["GET /api/v0/codeowners/ownership"]
		var kept []routeDisclosure
		for _, d := range codeowners.Disclosed {
			if d.Domain != "service_catalog_correlation" {
				kept = append(kept, d)
			}
		}
		codeowners.Disclosed = kept
		poisoned["GET /api/v0/codeowners/ownership"] = codeowners

		findings := verifyRouteServesDataScan(repoRoot, poisoned, domainDataSignatures)
		joined := strings.Join(findings, "\n")
		if !strings.Contains(joined, "service_catalog_correlation") {
			t.Errorf("BITES FAILED: dropping the codeowners service-catalog disclosure was not contradicted by the scan: %v", findings)
		}
	})

	// Production stays green after all poisoned-copy runs.
	t.Run("production_green", func(t *testing.T) {
		findings := runRouteServesDataChecks(t, routeServesDataBackingMap, routeServesDataRegistry, domainDataSignatures)
		if len(findings) != 0 {
			t.Fatalf("production registry went red: %v", findings)
		}
	})
}
