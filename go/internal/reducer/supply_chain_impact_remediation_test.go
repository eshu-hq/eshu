// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestBuildSupplyChainImpactFindingsRemediationDirectUpgradeAllowed proves that
// an npm direct dependency whose manifest range already admits the first
// patched version reports an exact-confidence "direct_upgrade_allowed"
// remediation so callers can recommend a safe upgrade without changing the
// manifest range.
func TestBuildSupplyChainImpactFindingsRemediationDirectUpgradeAllowed(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-allowed", "CVE-2026-90001", 7.5),
		vulnerabilityAffectedPackageRangeFact(
			"affected-allowed",
			"CVE-2026-90001",
			"pkg:npm/example",
			"npm",
			"example",
			"1.3.0",
		),
		packageConsumptionFactWithChain(
			"consume-allowed",
			"pkg:npm/example",
			testImpactRepositoryID,
			"^1.2.0",
			[]string{"example"},
			1,
			true,
		),
	})

	finding := supplyChainImpactFindingsByCVE(findings)["CVE-2026-90001"]
	remediation := finding.Remediation
	if remediation.Reason != "direct_upgrade_allowed" {
		t.Fatalf("Reason = %q, want direct_upgrade_allowed", remediation.Reason)
	}
	if remediation.Confidence != "exact" {
		t.Fatalf("Confidence = %q, want exact", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "1.3.0" {
		t.Fatalf("FirstPatchedVersion = %q, want 1.3.0", remediation.FirstPatchedVersion)
	}
	if remediation.ManifestAllowsFix != "allowed" {
		t.Fatalf("ManifestAllowsFix = %q, want allowed", remediation.ManifestAllowsFix)
	}
	if remediation.ManifestRange != "^1.2.0" {
		t.Fatalf("ManifestRange = %q, want ^1.2.0", remediation.ManifestRange)
	}
	if remediation.Direct == nil || !*remediation.Direct {
		t.Fatalf("Direct = %#v, want true for direct dependency", remediation.Direct)
	}
	if remediation.ParentPackage != "" {
		t.Fatalf("ParentPackage = %q, want empty for direct dependency", remediation.ParentPackage)
	}
	if remediation.Ecosystem != "npm" {
		t.Fatalf("Ecosystem = %q, want npm", remediation.Ecosystem)
	}
}

// TestBuildSupplyChainImpactFindingsRemediationDirectRangeBlocked proves that
// an npm direct dependency whose manifest range cannot admit the first patched
// version reports an exact-confidence "direct_range_blocked" remediation so the
// caller knows the manifest itself must be bumped before the upgrade.
func TestBuildSupplyChainImpactFindingsRemediationDirectRangeBlocked(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-blocked", "CVE-2026-90002", 8.1),
		vulnerabilityAffectedPackageRangeFact(
			"affected-blocked",
			"CVE-2026-90002",
			"pkg:npm/example",
			"npm",
			"example",
			"2.0.0",
		),
		packageConsumptionFactWithChain(
			"consume-blocked",
			"pkg:npm/example",
			testImpactRepositoryID,
			"~1.2.0",
			[]string{"example"},
			1,
			true,
		),
	})

	finding := supplyChainImpactFindingsByCVE(findings)["CVE-2026-90002"]
	remediation := finding.Remediation
	if remediation.Reason != "direct_range_blocked" {
		t.Fatalf("Reason = %q, want direct_range_blocked", remediation.Reason)
	}
	if remediation.Confidence != "exact" {
		t.Fatalf("Confidence = %q, want exact", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "2.0.0" {
		t.Fatalf("FirstPatchedVersion = %q, want 2.0.0", remediation.FirstPatchedVersion)
	}
	if remediation.ManifestAllowsFix != "blocked" {
		t.Fatalf("ManifestAllowsFix = %q, want blocked", remediation.ManifestAllowsFix)
	}
	if remediation.Direct == nil || !*remediation.Direct {
		t.Fatalf("Direct = %#v, want true for direct dependency", remediation.Direct)
	}
	if remediation.ParentPackage != "" {
		t.Fatalf("ParentPackage = %q, want empty for direct dependency", remediation.ParentPackage)
	}
}

// TestBuildSupplyChainImpactFindingsRemediationTransitiveParentRequired proves
// that a transitive npm dependency surfaces the parent package the caller must
// upgrade and keeps confidence partial because Eshu does not own the parent's
// manifest range.
func TestBuildSupplyChainImpactFindingsRemediationTransitiveParentRequired(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-transitive", "CVE-2026-90003", 9.1),
		vulnerabilityAffectedPackageRangeFact(
			"affected-transitive",
			"CVE-2026-90003",
			"pkg:npm/fsevents",
			"npm",
			"fsevents",
			"2.3.4",
		),
		packageConsumptionFactWithChain(
			"consume-transitive",
			"pkg:npm/fsevents",
			testImpactRepositoryID,
			"2.3.3",
			[]string{"vite", "rollup", "fsevents"},
			3,
			false,
		),
	})

	finding := supplyChainImpactFindingsByCVE(findings)["CVE-2026-90003"]
	remediation := finding.Remediation
	if remediation.Reason != "transitive_parent_upgrade_required" {
		t.Fatalf("Reason = %q, want transitive_parent_upgrade_required", remediation.Reason)
	}
	if remediation.Confidence != "partial" {
		t.Fatalf("Confidence = %q, want partial", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "2.3.4" {
		t.Fatalf("FirstPatchedVersion = %q, want 2.3.4", remediation.FirstPatchedVersion)
	}
	if remediation.ParentPackage != "rollup" {
		t.Fatalf("ParentPackage = %q, want rollup (parent of fsevents in chain)", remediation.ParentPackage)
	}
	if remediation.Direct == nil || *remediation.Direct {
		t.Fatalf("Direct = %#v, want false for transitive dependency", remediation.Direct)
	}
	if remediation.ManifestAllowsFix != "unknown" {
		t.Fatalf("ManifestAllowsFix = %q, want unknown for transitive parent path", remediation.ManifestAllowsFix)
	}
}

// TestBuildSupplyChainImpactFindingsRemediationNoPatchedVersion proves that an
// npm finding whose advisory carries no fixed-version branches reports an
// unknown-confidence "no_patched_version" remediation so callers do not
// fabricate an upgrade candidate that does not exist.
func TestBuildSupplyChainImpactFindingsRemediationNoPatchedVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-no-fix", "CVE-2026-90004", 6.5),
		vulnerabilityAffectedPackageNoFixedVersionFact(
			"affected-no-fix",
			"CVE-2026-90004",
			"pkg:npm/example",
			"npm",
			"example",
		),
		packageConsumptionFactWithChain(
			"consume-no-fix",
			"pkg:npm/example",
			testImpactRepositoryID,
			"1.2.3",
			[]string{"example"},
			1,
			true,
		),
	})

	finding := supplyChainImpactFindingsByCVE(findings)["CVE-2026-90004"]
	remediation := finding.Remediation
	if remediation.Reason != "no_patched_version" {
		t.Fatalf("Reason = %q, want no_patched_version", remediation.Reason)
	}
	if remediation.Confidence != "unknown" {
		t.Fatalf("Confidence = %q, want unknown", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "" {
		t.Fatalf("FirstPatchedVersion = %q, want empty when no fix exists", remediation.FirstPatchedVersion)
	}
	if remediation.ManifestAllowsFix != "unknown" {
		t.Fatalf("ManifestAllowsFix = %q, want unknown when no fix exists", remediation.ManifestAllowsFix)
	}
	if !containsRemediationMissingEvidence(remediation, "fixed_version_missing") {
		t.Fatalf("MissingEvidence = %#v, want fixed_version_missing", remediation.MissingEvidence)
	}
}

// TestBuildSupplyChainImpactFindingsRemediationMultiplePatchedBranches proves
// that when an advisory publishes fixed-version branches across multiple
// majors and the lockfile pins an exact installed version, the remediation
// keeps the lowest patched version that satisfies the observed major and
// downgrades confidence to partial so callers see the branch ambiguity
// rather than silently committing to a single major bump. The exact
// installed version comes from the lockfile-anchored consumption fact;
// missing installed versions follow the installed_version_missing reason
// (covered by a separate test).
func TestBuildSupplyChainImpactFindingsRemediationMultiplePatchedBranches(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-multi-ghsa", "CVE-2026-90005", 7.0),
		multiSourceAffectedPackageFact(
			"affected-multi-ghsa",
			"CVE-2026-90005",
			"pkg:npm/example",
			"npm",
			"example",
			"ghsa",
			"GHSA-multi-1",
			[]string{"1.3.5"},
			"<1.3.5",
		),
		vulnerabilityCVEFact("cve-multi-osv", "CVE-2026-90005", 7.0),
		multiSourceAffectedPackageFact(
			"affected-multi-osv",
			"CVE-2026-90005",
			"pkg:npm/example",
			"npm",
			"example",
			"osv",
			"GHSA-multi-2",
			[]string{"2.0.0"},
			"<2.0.0",
		),
		packageConsumptionFactWithChain(
			"consume-multi",
			"pkg:npm/example",
			testImpactRepositoryID,
			"1.2.3",
			[]string{"example"},
			1,
			true,
		),
	})

	finding := supplyChainImpactFindingsByCVE(findings)["CVE-2026-90005"]
	remediation := finding.Remediation
	if remediation.Reason != "multiple_patched_branches" {
		t.Fatalf("Reason = %q, want multiple_patched_branches", remediation.Reason)
	}
	if remediation.Confidence != "partial" {
		t.Fatalf("Confidence = %q, want partial", remediation.Confidence)
	}
	if remediation.FirstPatchedVersion != "1.3.5" {
		t.Fatalf("FirstPatchedVersion = %q, want lowest patched version that stays within observed major (1.3.5)", remediation.FirstPatchedVersion)
	}
	branchVersions := remediationBranchVersions(remediation.PatchedVersionBranches)
	wantVersions := map[string]bool{"1.3.5": true, "2.0.0": true}
	if !reflect.DeepEqual(branchVersions, wantVersions) {
		t.Fatalf("PatchedVersionBranches = %#v, want {1.3.5, 2.0.0}", branchVersions)
	}
}

// TestBuildSupplyChainImpactFindingsRemediationPersistsVulnerableRange proves
// that the reducer captures the source-reported vulnerable range on the
// finding payload so list-route callers see vulnerable_range, not only the
// explain route. Co-pilot flagged that the docs and OpenAPI advertise this
// field on every finding row even though the original reducer build never
// persisted it.
func TestBuildSupplyChainImpactFindingsRemediationPersistsVulnerableRange(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-vr", "CVE-2026-90009", 7.5),
		multiSourceAffectedPackageFact(
			"affected-vr",
			"CVE-2026-90009",
			"pkg:npm/example",
			"npm",
			"example",
			"ghsa",
			"GHSA-vr-1",
			[]string{"1.3.0"},
			"<1.3.0",
		),
		packageConsumptionFactWithChain(
			"consume-vr",
			"pkg:npm/example",
			testImpactRepositoryID,
			"^1.2.0",
			[]string{"example"},
			1,
			true,
		),
	})

	finding := supplyChainImpactFindingsByCVE(findings)["CVE-2026-90009"]
	if finding.VulnerableRange != "<1.3.0" {
		t.Fatalf("Finding.VulnerableRange = %q, want <1.3.0 captured from advisory provenance", finding.VulnerableRange)
	}
	if finding.Remediation.VulnerableRange != "<1.3.0" {
		t.Fatalf("Remediation.VulnerableRange = %q, want <1.3.0 mirrored from finding", finding.Remediation.VulnerableRange)
	}
	payload := supplyChainImpactPayload(SupplyChainImpactWrite{
		ScopeID:      "scope-vr",
		GenerationID: "gen-vr",
		SourceSystem: "reducer",
	}, finding)
	if got, want := payload["vulnerable_range"], "<1.3.0"; got != want {
		t.Fatalf("payload[vulnerable_range] = %#v, want %q", got, want)
	}
	remediation, ok := payload["remediation"].(map[string]any)
	if !ok {
		t.Fatalf("payload[remediation] = %#v, want map", payload["remediation"])
	}
	if got, want := remediation["vulnerable_range"], "<1.3.0"; got != want {
		t.Fatalf("payload[remediation].vulnerable_range = %#v, want %q", got, want)
	}
}

// TestSupplyChainImpactRemediationPayloadRoundTrip proves that the writer
// serializes the remediation block onto the canonical fact payload so the
// read model and explain surface can decode it back without losing the
// confidence, reason, manifest-allows-fix, parent-package, or patched-branch
// metadata callers depend on for remediation explanations.
func TestSupplyChainImpactRemediationPayloadRoundTrip(t *testing.T) {
	t.Parallel()

	finding := SupplyChainImpactFinding{
		CVEID:            "CVE-2026-90007",
		PackageID:        "pkg:npm/example",
		Ecosystem:        "npm",
		ObservedVersion:  "1.2.3",
		RequestedRange:   "^1.2.0",
		FixedVersion:     "1.3.0",
		DependencyPath:   []string{"example"},
		DependencyDepth:  1,
		DirectDependency: boolPtr(true),
		Remediation: SupplyChainImpactRemediation{
			Ecosystem:           "npm",
			CurrentVersion:      "1.2.3",
			VulnerableRange:     "<1.3.0",
			FirstPatchedVersion: "1.3.0",
			ManifestRange:       "^1.2.0",
			ManifestAllowsFix:   "allowed",
			Direct:              boolPtr(true),
			Confidence:          "exact",
			Reason:              "direct_upgrade_allowed",
			PatchedVersionBranches: []FixedVersionBranch{
				{Version: "1.3.0", Source: "ghsa"},
			},
		},
	}
	payload := supplyChainImpactPayload(SupplyChainImpactWrite{
		IntentID:     "intent-rem",
		ScopeID:      "scope-rem",
		GenerationID: "gen-rem",
		SourceSystem: "reducer",
	}, finding)
	raw, ok := payload["remediation"].(map[string]any)
	if !ok {
		t.Fatalf("payload[remediation] = %#v, want map", payload["remediation"])
	}
	if got, want := raw["reason"], "direct_upgrade_allowed"; got != want {
		t.Fatalf("remediation reason = %#v, want %q", got, want)
	}
	if got, want := raw["confidence"], "exact"; got != want {
		t.Fatalf("remediation confidence = %#v, want %q", got, want)
	}
	if got, want := raw["manifest_allows_fix"], "allowed"; got != want {
		t.Fatalf("remediation manifest_allows_fix = %#v, want %q", got, want)
	}
	if got, want := raw["first_patched_version"], "1.3.0"; got != want {
		t.Fatalf("remediation first_patched_version = %#v, want %q", got, want)
	}
	if got, want := raw["ecosystem"], "npm"; got != want {
		t.Fatalf("remediation ecosystem = %#v, want %q", got, want)
	}
}

func containsRemediationMissingEvidence(r SupplyChainImpactRemediation, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range r.MissingEvidence {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func remediationBranchVersions(branches []FixedVersionBranch) map[string]bool {
	out := make(map[string]bool, len(branches))
	for _, branch := range branches {
		out[strings.TrimSpace(branch.Version)] = true
	}
	return out
}

// vulnerabilityAffectedPackageNoFixedVersionFact builds a
// vulnerability.affected_package fixture. advisory_id is set equal to cveID:
// every real collector source always sets it, so a fixture without it was
// never realistic collector output.
func vulnerabilityAffectedPackageNoFixedVersionFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":       cveID,
			"advisory_id":  cveID,
			"package_id":   packageID,
			"ecosystem":    ecosystem,
			"package_name": name,
			"affected_ranges": []any{
				map[string]any{
					"type": "SEMVER",
					"events": []any{
						map[string]any{"introduced": "0"},
					},
				},
			},
		},
	}
}

func multiSourceAffectedPackageFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	source string,
	advisoryID string,
	fixedVersions []string,
	affectedRange string,
) facts.Envelope {
	fixed := make([]any, 0, len(fixedVersions))
	for _, version := range fixedVersions {
		fixed = append(fixed, version)
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"package_id":     packageID,
			"ecosystem":      ecosystem,
			"package_name":   name,
			"source":         source,
			"advisory_id":    advisoryID,
			"fixed_versions": fixed,
			"affected_range": affectedRange,
		},
	}
}
