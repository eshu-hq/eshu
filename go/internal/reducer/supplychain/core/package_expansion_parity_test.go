// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestPackageExpansionBoundEnrichmentOnlyScopeRefreshesWinnersRow is #11097
// acceptance criterion 3: one scope holds the advisory, another holds only an
// EPSS (or KEV) fact for the same CVE, and a third holds the consumer. When
// the EPSS score updates, the enrichment-only scope loads the seeded CVE's
// advisory + consumer without pulling unrelated CVEs on the same package, and
// its findings carry the new EPSS score and KEV flag.
func TestPackageExpansionBoundEnrichmentOnlyScopeRefreshesWinnersRow(t *testing.T) {
	t.Parallel()

	const (
		advisoryScope  = "vuln-intel://osv/npm/slot-01"
		unrelatedScope = "vuln-intel://osv/npm/slot-02"
		epssScope      = "vuln-intel://first-epss/shard-01"
		repoScope      = "git-repository-scope:repo://example/web"
		repoID         = "repo://example/web"
		pkgID          = "npm://registry.npmjs.org/express"
		cveTarget      = "CVE-2026-5001"
		cveUnrelated   = "CVE-2026-5002"
	)

	cveRec := withScopeID(vulnerabilityCVEFact("cve-5001", cveTarget, 7.5), advisoryScope)
	affTarget := withScopeID(vulnerabilityAffectedPackageFact("aff-5001", cveTarget, pkgID, "npm", "express", "4.17.0", "4.19.2"), advisoryScope)
	cveOther := withScopeID(vulnerabilityCVEFact("cve-5002", cveUnrelated, 6.5), unrelatedScope)
	affOther := withScopeID(vulnerabilityAffectedPackageFact("aff-5002", cveUnrelated, pkgID, "npm", "express", "4.17.0", "4.19.2"), unrelatedScope)
	cons := withScopeID(packageConsumptionFactWithRange("cons-express", pkgID, repoID, "4.17.0"), repoScope)
	kev := withScopeID(vulnerabilityKEVFact("kev-5001", cveTarget), epssScope)

	runEPSSPass := func(score string) []SupplyChainImpactFinding {
		t.Helper()
		epss := withScopeID(vulnerabilityEPSSFact("epss-5001-"+score, cveTarget, score, "0.9500"), epssScope)
		universe := []facts.Envelope{cveRec, affTarget, cveOther, affOther, cons, epss, kev}
		initial := []facts.Envelope{epss, kev}
		stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
		h := SupplyChainImpactHandler{
			FactLoader:            stub,
			PackageExpansionBound: &SupplyChainImpactPackageExpansionBound{},
		}
		loaded, trunc, bounds, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
			context.Background(), initial, supplyChainImpactFilter(initial),
			newSupplyChainImpactEvidenceBudget(0), nil, epssScope,
		)
		if err != nil {
			t.Fatalf("EPSS pass load error = %v", err)
		}
		if trunc.partial() {
			t.Fatalf("EPSS pass must converge, truncation = %+v", trunc)
		}
		if bounds.pkg.Loads > maxSupplyChainImpactActiveEvidenceLoads {
			t.Fatalf("EPSS pass loads = %d, want <= %d", bounds.pkg.Loads, maxSupplyChainImpactActiveEvidenceLoads)
		}
		// Unrelated CVE sharing pkgID must stay out of the EPSS scope's pass.
		if containsID(loadedFactIDs(loaded), "aff-5002") {
			t.Fatalf("EPSS pass leaked unrelated CVE sharing %s: %v", pkgID, loadedFactIDs(loaded))
		}
		return BuildSupplyChainImpactFindings(loaded)
	}

	before := runEPSSPass("0.1200")
	if len(before) != 1 || before[0].CVEID != cveTarget {
		t.Fatalf("before findings = %v, want single %s finding", findingCVEIDs(before), cveTarget)
	}
	if before[0].EPSSProbability != "0.1200" {
		t.Fatalf("before EPSSProbability = %q, want 0.1200", before[0].EPSSProbability)
	}
	if !before[0].KnownExploited {
		t.Fatal("before KnownExploited = false, want true")
	}

	after := runEPSSPass("0.8700")
	if len(after) != 1 || after[0].CVEID != cveTarget {
		t.Fatalf("after findings = %v, want single %s finding", findingCVEIDs(after), cveTarget)
	}
	if after[0].EPSSProbability != "0.8700" {
		t.Fatalf("after EPSSProbability = %q, want 0.8700 (updated EPSS score must propagate)", after[0].EPSSProbability)
	}
	if !after[0].KnownExploited {
		t.Fatal("after KnownExploited = false, want true")
	}
	if supplyChainImpactFindingID(after[0]) != supplyChainImpactFindingID(before[0]) {
		t.Fatalf("finding_id drifted across EPSS refresh: %q vs %q", supplyChainImpactFindingID(before[0]), supplyChainImpactFindingID(after[0]))
	}
}

// osSiblingKey captures the #11097 AC #4 contract fields for an OS finding.
type osSiblingKey struct {
	FindingID       string
	CVEID           string
	AdvisoryID      string
	Status          SupplyChainImpactStatus
	FixedVersion    string
	AdvisoryRetract bool
	MatchReason     string
}

func projectOSSiblingKeys(findings []SupplyChainImpactFinding) []osSiblingKey {
	out := make([]osSiblingKey, 0, len(findings))
	for _, f := range findings {
		out = append(out, osSiblingKey{
			FindingID:       supplyChainImpactFindingID(f),
			CVEID:           f.CVEID,
			AdvisoryID:      f.AdvisoryID,
			Status:          f.Status,
			FixedVersion:    f.FixedVersion,
			AdvisoryRetract: f.AdvisoryRetraction != nil,
			MatchReason:     f.MatchReason,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FindingID != out[j].FindingID {
			return out[i].FindingID < out[j].FindingID
		}
		return out[i].AdvisoryID < out[j].AdvisoryID
	})
	return out
}

// TestPackageExpansionBoundOSSiblingAcrossTwoBatchScopesParity is #11097
// acceptance criterion 4: two distro records (DSA and DLA) for one OS package
// and one upstream CVE live in two separate batch/slot scopes. Because the
// own-scope 1-hop OS exemption keeps own-scope OS affected_package facts on
// PackageIDs/PURLs, sibling consolidation produces byte-identical finding_id,
// advisory_id, verdict, and retraction before and after, while a second
// unrelated OS package named only on the foreign sibling bulletin does NOT
// chain into Round 3+.
func TestPackageExpansionBoundOSSiblingAcrossTwoBatchScopesParity(t *testing.T) {
	t.Parallel()

	const (
		scopeDSA     = "vuln-intel://osv/debian/slot-01"
		scopeDLA     = "vuln-intel://osv/debian/slot-02"
		scopeOtherOS = "vuln-intel://osv/debian/slot-03"
		scanScope    = "scanner-worker://image/debian-12"
		upstreamCVE  = "CVE-2026-6001"
		otherOSCVE   = "CVE-2026-6099"
		pkgOpenSSL   = "os://debian/openssl"
		purlOpenSSL  = "pkg:deb/debian/openssl"
		pkgCurl      = "os://debian/curl"
		purlCurl     = "pkg:deb/debian/curl"
	)

	cveDSA := withScopeID(facts.Envelope{
		FactID:   "cve-dsa-6001",
		FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":      "DSA-6001-1",
			"advisory_id": "DSA-6001-1",
			"source":      "osv",
			"cvss_score":  8.1,
			"modified_at": "2026-06-02T00:00:00Z",
			"upstream":    []any{upstreamCVE},
		},
	}, scopeDSA)
	affDSA := withScopeID(facts.Envelope{
		FactID:   "aff-dsa-6001",
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":      "DSA-6001-1",
			"advisory_id": "DSA-6001-1",
			"source":      "osv",
			"ecosystem":   "os",
			"package_id":  pkgOpenSSL,
			"purl":        purlOpenSSL,
			"package":     "openssl",
			"ranges": []any{
				map[string]any{
					"type":       "ECOSYSTEM",
					"ecosystem":  "Debian:12",
					"introduced": "0",
					"fixed":      "3.0.14-1~deb12u2",
					"events":     []any{map[string]any{"introduced": "0"}, map[string]any{"fixed": "3.0.14-1~deb12u2"}},
				},
			},
		},
	}, scopeDSA)

	// Sibling record in a second batch scope naming the same package + upstream
	// CVE, plus a second unrelated OS package (curl) to prove foreign OS
	// bulletins loaded in Round 2 do not chain into Round 3+.
	cveDLA := withScopeID(facts.Envelope{
		FactID:   "cve-dla-6001",
		FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":      "DLA-6001-1",
			"advisory_id": "DLA-6001-1",
			"source":      "osv",
			"cvss_score":  7.5,
			"modified_at": "2026-06-01T00:00:00Z",
			"upstream":    []any{upstreamCVE},
		},
	}, scopeDLA)
	// Intentionally test blank ecosystem with pkg:deb/... PURL to prove PURL
	// fallback classification matches osPackageFamilyFromPURL.
	affDLA := withScopeID(facts.Envelope{
		FactID:   "aff-dla-6001",
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":      "DLA-6001-1",
			"advisory_id": "DLA-6001-1",
			"source":      "osv",
			"ecosystem":   "",
			"package_id":  pkgOpenSSL,
			"purl":        purlOpenSSL,
			"package":     "openssl",
			"ranges": []any{
				map[string]any{
					"type":       "ECOSYSTEM",
					"ecosystem":  "Debian:12",
					"introduced": "0",
					"fixed":      "3.0.14-1~deb12u1",
					"events":     []any{map[string]any{"introduced": "0"}, map[string]any{"fixed": "3.0.14-1~deb12u1"}},
				},
			},
		},
	}, scopeDLA)
	affDLACurl := withScopeID(facts.Envelope{
		FactID:   "aff-dla-curl",
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":      "DLA-6001-1",
			"advisory_id": "DLA-6001-1",
			"source":      "osv",
			"ecosystem":   "os",
			"package_id":  pkgCurl,
			"purl":        purlCurl,
			"package":     "curl",
		},
	}, scopeDLA)

	// Unrelated OS advisory on curl in a third scope: must NOT be pulled when
	// running scopeDSA (foreign DLA's curl must not chain!).
	cveOtherOS := withScopeID(facts.Envelope{
		FactID:   "cve-other-curl",
		FactKind: chain.VulnerabilityCVEFactKind,
		Payload: map[string]any{
			"cve_id":      "DSA-6099-1",
			"advisory_id": "DSA-6099-1",
			"source":      "osv",
			"cvss_score":  9.0,
			"upstream":    []any{otherOSCVE},
		},
	}, scopeOtherOS)
	affOtherOS := withScopeID(facts.Envelope{
		FactID:   "aff-other-curl",
		FactKind: chain.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":      "DSA-6099-1",
			"advisory_id": "DSA-6099-1",
			"source":      "osv",
			"ecosystem":   "os",
			"package_id":  pkgCurl,
			"purl":        purlCurl,
			"package":     "curl",
		},
	}, scopeOtherOS)

	installedOpenSSL := osPackageFact("os-pkg-openssl", scanScope, map[string]any{
		"distro":                 "debian",
		"distro_version":         "12",
		"package_manager":        "dpkg",
		"name":                   "openssl",
		"source_package":         "openssl",
		"arch":                   "amd64",
		"repository_class":       "vendor",
		"vendor_advisory_source": "debian",
		"installed_version_raw":  "3.0.14-1~deb12u1",
		"purl":                   "pkg:deb/debian/openssl@3.0.14-1~deb12u1?arch=amd64&distro=debian-12",
	})

	universe := []facts.Envelope{cveDSA, affDSA, cveDLA, affDLA, affDLACurl, cveOtherOS, affOtherOS}

	runScope := func(scopeID string, initial []facts.Envelope, bound bool) ([]osSiblingKey, []facts.Envelope, supplyChainImpactPackageBoundOutcome) {
		t.Helper()
		stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
		h := SupplyChainImpactHandler{FactLoader: stub}
		if bound {
			h.PackageExpansionBound = &SupplyChainImpactPackageExpansionBound{}
		}
		loaded, trunc, bounds, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
			context.Background(), append([]facts.Envelope(nil), initial...),
			supplyChainImpactFilter(initial), newSupplyChainImpactEvidenceBudget(0), nil, scopeID,
		)
		if err != nil {
			t.Fatalf("scope %s (bound=%v) load error = %v", scopeID, bound, err)
		}
		if trunc.partial() {
			t.Fatalf("scope %s (bound=%v) partial = %+v", scopeID, bound, trunc)
		}
		if bound && bounds.pkg.Loads > maxSupplyChainImpactActiveEvidenceLoads {
			t.Fatalf("scope %s loads = %d, want <= %d", scopeID, bounds.pkg.Loads, maxSupplyChainImpactActiveEvidenceLoads)
		}
		withInstalled := append(append([]facts.Envelope(nil), loaded...), installedOpenSSL)
		return projectOSSiblingKeys(BuildSupplyChainImpactFindings(withInstalled)), loaded, bounds.pkg
	}

	// From scopeDSA (DSA-6001-1): sibling DLA-6001-1 MUST load in both arms,
	// yielding byte-identical finding_id, advisory_id, and verdict, while
	// aff-other-curl MUST NOT load in the bounded arm.
	unboundedDSA, _, _ := runScope(scopeDSA, []facts.Envelope{cveDSA, affDSA}, false)
	boundedDSA, boundedDSALoaded, _ := runScope(scopeDSA, []facts.Envelope{cveDSA, affDSA}, true)
	if len(boundedDSA) == 0 {
		t.Fatal("expected non-empty OS finding from scopeDSA")
	}
	if !reflect.DeepEqual(unboundedDSA, boundedDSA) {
		t.Fatalf("OS sibling findings from scopeDSA differ before vs after:\nunbounded=%#v\nbounded=%#v", unboundedDSA, boundedDSA)
	}
	if !containsID(loadedFactIDs(boundedDSALoaded), "aff-dla-6001") {
		t.Fatalf("scopeDSA bounded load must pull sibling aff-dla-6001, got %v", loadedFactIDs(boundedDSALoaded))
	}
	if containsID(loadedFactIDs(boundedDSALoaded), "aff-other-curl") {
		t.Fatalf("foreign DLA curl package must not chain into scopeOtherOS, got %v", loadedFactIDs(boundedDSALoaded))
	}

	// From scopeDLA (blank ecosystem + pkg:deb PURL): sibling DSA-6001-1 MUST
	// load and decide identically before vs after.
	unboundedDLA, _, _ := runScope(scopeDLA, []facts.Envelope{cveDLA, affDLA}, false)
	boundedDLA, boundedDLALoaded, _ := runScope(scopeDLA, []facts.Envelope{cveDLA, affDLA}, true)
	if !reflect.DeepEqual(unboundedDLA, boundedDLA) {
		t.Fatalf("OS sibling findings from scopeDLA differ before vs after:\nunbounded=%#v\nbounded=%#v", unboundedDLA, boundedDLA)
	}
	if !containsID(loadedFactIDs(boundedDLALoaded), "aff-dsa-6001") {
		t.Fatalf("scopeDLA bounded load must pull sibling aff-dsa-6001, got %v", loadedFactIDs(boundedDLALoaded))
	}
}

// TestPackageExpansionBoundFleetStoredRowReduction proves the #11097 fleet
// invariant across multiple vulnerability scopes sharing one package: the
// distinct winners set (union of findings across all scopes) is identical with
// and without the bound, while total stored finding rows drop from N*N to N
// (every scope stores only its own seeded findings).
func TestPackageExpansionBoundFleetStoredRowReduction(t *testing.T) {
	t.Parallel()

	const (
		repoScope = "git-repository-scope:repo://example/monorepo"
		repoID    = "repo://example/monorepo"
		sharedPkg = "npm://registry.npmjs.org/minimist"
	)

	cves := []string{"CVE-2026-7001", "CVE-2026-7002", "CVE-2026-7003", "CVE-2026-7004"}
	cons := withScopeID(packageConsumptionFactWithRange("cons-minimist", sharedPkg, repoID, "1.2.5"), repoScope)
	universe := []facts.Envelope{cons}
	scopeInitials := map[string][]facts.Envelope{}
	for _, cve := range cves {
		scopeID := "vuln-intel://osv/npm/" + cve
		rec := withScopeID(vulnerabilityCVEFact("cve-"+cve, cve, 8.0), scopeID)
		aff := withScopeID(vulnerabilityAffectedPackageFact("aff-"+cve, cve, sharedPkg, "npm", "minimist", "1.2.5", "1.2.6"), scopeID)
		universe = append(universe, rec, aff)
		scopeInitials[scopeID] = []facts.Envelope{rec, aff}
	}

	runFleet := func(bound bool) (totalRows int, distinctKeys map[string]SupplyChainImpactStatus) {
		distinctKeys = map[string]SupplyChainImpactStatus{}
		for scopeID, initial := range scopeInitials {
			stub := &stubSupplyChainImpactFactLoader{activeForFilter: packageBoundLoader(universe)}
			h := SupplyChainImpactHandler{FactLoader: stub}
			if bound {
				h.PackageExpansionBound = &SupplyChainImpactPackageExpansionBound{}
			}
			loaded, trunc, bounds, err := h.loadActiveSupplyChainImpactFactsUntilStableBounded(
				context.Background(), append([]facts.Envelope(nil), initial...),
				supplyChainImpactFilter(initial), newSupplyChainImpactEvidenceBudget(0), nil, scopeID,
			)
			if err != nil {
				t.Fatalf("scope %s (bound=%v) error = %v", scopeID, bound, err)
			}
			if trunc.partial() {
				t.Fatalf("scope %s (bound=%v) partial = %+v", scopeID, bound, trunc)
			}
			if bound && bounds.pkg.Loads > maxSupplyChainImpactActiveEvidenceLoads {
				t.Fatalf("scope %s loads = %d, want <= %d", scopeID, bounds.pkg.Loads, maxSupplyChainImpactActiveEvidenceLoads)
			}
			findings := BuildSupplyChainImpactFindings(loaded)
			totalRows += len(findings)
			for _, f := range findings {
				distinctKeys[supplyChainImpactFindingID(f)] = f.Status
			}
		}
		return totalRows, distinctKeys
	}

	unboundedRows, unboundedDistinct := runFleet(false)
	boundedRows, boundedDistinct := runFleet(true)

	if unboundedRows != len(cves)*len(cves) {
		t.Fatalf("unbounded fleet rows = %d, want %d (N*N cross-scope copies)", unboundedRows, len(cves)*len(cves))
	}
	if boundedRows != len(cves) {
		t.Fatalf("bounded fleet rows = %d, want %d (1 owned copy per scope)", boundedRows, len(cves))
	}
	if !reflect.DeepEqual(unboundedDistinct, boundedDistinct) {
		t.Fatalf("distinct winners set drifted:\nunbounded=%v\nbounded=%v", unboundedDistinct, boundedDistinct)
	}
	t.Logf("fleet stored rows: unbounded=%d bounded=%d distinct_winners=%d", unboundedRows, boundedRows, len(boundedDistinct))
}

// TestPackageExpansionBoundSubSignalsAndSeamOff proves all 7 package_bound_*
// sub-signals ride every pass (zeros when off) and a nil bound state is a
// byte-for-byte no-op (the one-line rollback).
func TestPackageExpansionBoundSubSignalsAndSeamOff(t *testing.T) {
	t.Parallel()

	keys := []string{
		"package_bound_suppressed_transitive",
		"package_bound_loads",
		"package_bound_owned_package_ids",
		"package_bound_seed_package_ids",
		"package_bound_owned_purls",
		"package_bound_seed_purls",
		"package_bound_seed_cves",
	}
	run := func(bound bool) map[string]float64 {
		t.Helper()
		loader := &stubSupplyChainImpactFactLoader{
			scopeFacts: []facts.Envelope{
				withScopeID(vulnerabilityCVEFact("cve-1", "CVE-2026-0001", 9.8), "vuln-intel://osv/npm/example"),
				withScopeID(vulnerabilityAffectedPackageFact("affected-1", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.2.3", "1.3.0"), "vuln-intel://osv/npm/example"),
			},
			active: []facts.Envelope{
				withScopeID(packageConsumptionFactWithRange("consume-1", testImpactPackageID, testImpactRepositoryID, "1.2.3"), "git-repository-scope:"+testImpactRepositoryID),
			},
		}
		handler := SupplyChainImpactHandler{
			FencingTokenIssuer: newTestImpactFencingTokenIssuer(),
			FactLoader:         loader,
			Writer:             &recordingSupplyChainImpactWriter{},
			Now:                func() time.Time { return time.Date(2026, 5, 24, 0, 0, 0, 0, time.UTC) },
		}
		if bound {
			handler.PackageExpansionBound = &SupplyChainImpactPackageExpansionBound{}
		}
		result, err := handler.Handle(context.Background(), reducercontract.Intent{
			IntentID:     "intent-pkg-bound-signals",
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
	for _, key := range keys {
		if value, ok := off[key]; !ok || value != 0 {
			t.Fatalf("seam-off SubSignals[%q] = %v (present=%v), want 0", key, value, ok)
		}
	}
	on := run(true)
	for _, key := range keys {
		if _, ok := on[key]; !ok {
			t.Fatalf("bound-on SubSignals missing %q", key)
		}
	}
	if on["package_bound_loads"] == 0 || on["package_bound_seed_cves"] == 0 {
		t.Fatalf("bound-on loads=%v seed_cves=%v, want both > 0", on["package_bound_loads"], on["package_bound_seed_cves"])
	}

	envelopes := []facts.Envelope{
		vulnerabilityCVEFact("cve-s", "CVE-2026-0001", 9.0),
		vulnerabilityAffectedPackageFact("aff-s", "CVE-2026-0001", testImpactPackageID, "npm", "example", "1.0.0", "1.1.0"),
	}
	filter := supplyChainImpactFilter(envelopes)
	var nilBound *packageExpansionBoundState
	next, suppressed := nilBound.apply(filter, envelopes, envelopes, SupplyChainImpactFactFilter{})
	if suppressed != 0 || !reflect.DeepEqual(next, filter) || nilBound.outcome() != (supplyChainImpactPackageBoundOutcome{}) {
		t.Fatalf("nil bound must be a no-op: suppressed=%d outcome=%+v", suppressed, nilBound.outcome())
	}
}
