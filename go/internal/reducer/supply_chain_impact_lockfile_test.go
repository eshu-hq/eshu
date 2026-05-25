package reducer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsUsesOwnedLockfileVersion(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-vite", "CVE-2026-39365", 5.3),
		vulnerabilityAffectedPackageRangeFact(
			"affected-vite",
			"CVE-2026-39365",
			"pkg:npm/vite",
			"npm",
			"vite",
			"6.4.2",
		),
		packageVersionFact("registry-version-vite", "pkg:npm/vite", "pkg:npm/vite@8.0.0", "8.0.0"),
		packageConsumptionFactWithRange("consume-vite", "pkg:npm/vite", testImpactRepositoryID, "5.4.21"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-39365"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.ObservedVersion != "5.4.21" {
		t.Fatalf("ObservedVersion = %q, want owned package-lock version 5.4.21", got.ObservedVersion)
	}
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
	if got.RuntimeReachability != "package_manifest" {
		t.Fatalf("RuntimeReachability = %q, want package_manifest", got.RuntimeReachability)
	}
	assertNotContainsString(t, got.MissingEvidence, "image or SBOM attachment evidence missing")
	path := strings.Join(got.EvidencePath, " -> ")
	if strings.Contains(path, facts.PackageRegistryPackageVersionFactKind) {
		t.Fatalf("EvidencePath = %#v, must not treat registry versions as installed versions", got.EvidencePath)
	}
	if !strings.Contains(path, packageConsumptionCorrelationFactKind) {
		t.Fatalf("EvidencePath = %#v, want package consumption evidence", got.EvidencePath)
	}
}

func TestBuildSupplyChainImpactFindingsExposesDependencyChain(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-fsevents", "CVE-2026-47138", 9.1),
		vulnerabilityAffectedPackageRangeFact(
			"affected-fsevents",
			"CVE-2026-47138",
			"pkg:npm/fsevents",
			"npm",
			"fsevents",
			"2.3.4",
		),
		packageConsumptionFactWithChain(
			"consume-fsevents",
			"pkg:npm/fsevents",
			testImpactRepositoryID,
			"2.3.3",
			[]string{"vite", "rollup", "fsevents"},
			3,
			false,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-47138"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if !reflect.DeepEqual(got.DependencyPath, []string{"vite", "rollup", "fsevents"}) {
		t.Fatalf("DependencyPath = %#v, want vite -> rollup -> fsevents", got.DependencyPath)
	}
	if got.DependencyDepth != 3 {
		t.Fatalf("DependencyDepth = %d, want 3", got.DependencyDepth)
	}
	if got.DirectDependency == nil || *got.DirectDependency {
		t.Fatalf("DirectDependency = %#v, want false for transitive lockfile dependency", got.DirectDependency)
	}
}

func TestBuildSupplyChainImpactFindingsLeavesRangeDependencyPossiblyAffected(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-vite", "CVE-2026-39365", 5.3),
		vulnerabilityAffectedPackageRangeFact(
			"affected-vite",
			"CVE-2026-39365",
			"pkg:npm/vite",
			"npm",
			"vite",
			"6.4.2",
		),
		packageConsumptionFactWithRange("consume-vite", "pkg:npm/vite", testImpactRepositoryID, "^5.4.11"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-39365"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want blank for non-exact manifest range", got.ObservedVersion)
	}
	if got.RepositoryID != testImpactRepositoryID {
		t.Fatalf("RepositoryID = %q, want %q", got.RepositoryID, testImpactRepositoryID)
	}
}

func TestBuildSupplyChainImpactFindingsMarksOwnedFixedVersionKnownFixed(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-vite", "CVE-2026-39365", 5.3),
		vulnerabilityAffectedPackageRangeFact(
			"affected-vite",
			"CVE-2026-39365",
			"pkg:npm/vite",
			"npm",
			"vite",
			"6.4.2",
		),
		packageConsumptionFactWithRange("consume-vite", "pkg:npm/vite", testImpactRepositoryID, "6.4.2"),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-39365"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactNotAffectedKnownFixed)
	if got.ObservedVersion != "6.4.2" {
		t.Fatalf("ObservedVersion = %q, want owned package-lock fixed version 6.4.2", got.ObservedVersion)
	}
}

func TestExactManifestDependencyVersionRejectsNonVersionSpecifiers(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"^8.20.0",
		"~8.20.0",
		">=8.20.0",
		"8.x",
		"latest",
		"workspace:^8.20.0",
		"file:../ws",
		"npm:ws@8.20.0",
		"github:websockets/ws",
		"https://registry.npmjs.org/ws/-/ws-8.20.0.tgz",
	} {
		if got, ok := exactManifestDependencyVersion(raw); ok {
			t.Fatalf("exactManifestDependencyVersion(%q) = %q, true; want rejected", raw, got)
		}
	}

	if got, ok := exactManifestDependencyVersion("8.20.0"); !ok || got != "8.20.0" {
		t.Fatalf("exactManifestDependencyVersion(8.20.0) = %q, %v; want 8.20.0, true", got, ok)
	}
}

func vulnerabilityAffectedPackageRangeFact(
	factID string,
	cveID string,
	packageID string,
	ecosystem string,
	name string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":       cveID,
			"package_id":   packageID,
			"ecosystem":    ecosystem,
			"package_name": name,
			"fixed_versions": []any{
				fixedVersion,
			},
			"affected_ranges": []any{
				map[string]any{
					"type": "SEMVER",
					"events": []any{
						map[string]any{"introduced": "0"},
						map[string]any{"fixed": fixedVersion},
					},
				},
			},
		},
	}
}

func packageConsumptionFactWithRange(
	factID string,
	packageID string,
	repositoryID string,
	dependencyRange string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: packageConsumptionCorrelationFactKind,
		Payload: map[string]any{
			"package_id":        packageID,
			"relationship_kind": "consumption",
			"repository_id":     repositoryID,
			"dependency_range":  dependencyRange,
			"canonical_writes":  1,
			"evidence_fact_ids": []any{"manifest-lock-1"},
		},
	}
}

func packageConsumptionFactWithChain(
	factID string,
	packageID string,
	repositoryID string,
	dependencyRange string,
	dependencyPath []string,
	dependencyDepth int,
	directDependency bool,
) facts.Envelope {
	payloadPath := make([]any, 0, len(dependencyPath))
	for _, item := range dependencyPath {
		payloadPath = append(payloadPath, item)
	}
	envelope := packageConsumptionFactWithRange(factID, packageID, repositoryID, dependencyRange)
	envelope.Payload["dependency_path"] = payloadPath
	envelope.Payload["dependency_depth"] = dependencyDepth
	envelope.Payload["direct_dependency"] = directDependency
	return envelope
}
