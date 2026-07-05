// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsProvesJVMManifestParity(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name            string
		cveID           string
		packageID       string
		packageName     string
		relativePath    string
		packageManager  string
		section         string
		version         string
		affectedRange   string
		fixedVersion    string
		wantStatus      SupplyChainImpactStatus
		wantMatchReason string
	}{
		{
			name:            "maven dependencies affected",
			cveID:           "CVE-2026-101101",
			packageID:       "pkg:maven/org.apache.logging.log4j/log4j-core",
			packageName:     "org.apache.logging.log4j:log4j-core",
			relativePath:    "pom.xml",
			packageManager:  "maven",
			section:         "dependencies",
			version:         "2.14.1",
			affectedRange:   "[2.0.0,2.17.1)",
			fixedVersion:    "2.17.1",
			wantStatus:      SupplyChainImpactAffectedExact,
			wantMatchReason: supplyChainVersionReasonMavenRangeMatch,
		},
		{
			name:            "maven dependencyManagement known fixed",
			cveID:           "CVE-2026-101102",
			packageID:       "pkg:maven/com.fasterxml.jackson.core/jackson-databind",
			packageName:     "com.fasterxml.jackson.core:jackson-databind",
			relativePath:    "pom.xml",
			packageManager:  "maven",
			section:         "dependencyManagement",
			version:         "2.15.3",
			affectedRange:   "[2.13.0,2.15.3)",
			fixedVersion:    "2.15.3",
			wantStatus:      SupplyChainImpactNotAffectedKnownFixed,
			wantMatchReason: supplyChainVersionReasonMavenKnownFixed,
		},
		{
			name:            "gradle groovy affected",
			cveID:           "CVE-2026-101103",
			packageID:       "pkg:maven/org.springframework/spring-core",
			packageName:     "org.springframework:spring-core",
			relativePath:    "build.gradle",
			packageManager:  "gradle",
			section:         "implementation",
			version:         "5.3.20",
			affectedRange:   "[5.3.0,5.3.30)",
			fixedVersion:    "5.3.30",
			wantStatus:      SupplyChainImpactAffectedExact,
			wantMatchReason: supplyChainVersionReasonMavenRangeMatch,
		},
		{
			name:            "gradle kotlin affected",
			cveID:           "CVE-2026-101104",
			packageID:       "pkg:maven/io.netty/netty-codec-http2",
			packageName:     "io.netty:netty-codec-http2",
			relativePath:    "build.gradle.kts",
			packageManager:  "gradle",
			section:         "runtimeOnly",
			version:         "4.1.90.Final",
			affectedRange:   "[4.1.0.Final,4.1.100.Final)",
			fixedVersion:    "4.1.100.Final",
			wantStatus:      SupplyChainImpactAffectedExact,
			wantMatchReason: supplyChainVersionReasonMavenRangeMatch,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			findings := BuildSupplyChainImpactFindings([]facts.Envelope{
				vulnerabilityCVEFact("cve-"+tc.cveID, tc.cveID, 7.4),
				jvmAffectedPackageRangeFact(
					"affected-"+tc.cveID,
					tc.cveID,
					tc.packageID,
					tc.packageName,
					tc.affectedRange,
					tc.fixedVersion,
				),
				packageManifestDependencyFactWithMetadata(
					testImpactRepositoryID,
					"jvm-app",
					tc.relativePath,
					tc.packageName,
					tc.packageManager,
					tc.version,
					observedAt,
					map[string]any{
						"section":                     tc.section,
						"dependency_scope":            "compile",
						"dependency_resolution_state": "resolved",
						"direct_dependency":           true,
						"dependency_path_kind":        "manifest",
					},
				),
			})

			got := supplyChainImpactFindingsByCVE(findings)[tc.cveID]
			assertSupplyChainImpactStatus(t, got, tc.wantStatus)
			if got.ObservedVersion != tc.version {
				t.Fatalf("ObservedVersion = %q, want %q", got.ObservedVersion, tc.version)
			}
			if got.RequestedRange != tc.version {
				t.Fatalf("RequestedRange = %q, want manifest version %q", got.RequestedRange, tc.version)
			}
			if got.MatchReason != tc.wantMatchReason {
				t.Fatalf("MatchReason = %q, want %q", got.MatchReason, tc.wantMatchReason)
			}
			if got.DetectionProfile != DetectionProfilePrecise {
				t.Fatalf("DetectionProfile = %q, want precise", got.DetectionProfile)
			}
		})
	}
}

func TestBuildSupplyChainImpactFindingsKeepsUnresolvedJVMVersionsIncomplete(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC)
	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-unresolved-jvm", "CVE-2026-101105", 8.2),
		jvmAffectedPackageRangeFact(
			"affected-unresolved-jvm",
			"CVE-2026-101105",
			"pkg:maven/org.springframework/spring-core",
			"org.springframework:spring-core",
			"[5.3.0,5.3.30)",
			"5.3.30",
		),
		packageManifestDependencyFactWithMetadata(
			testImpactRepositoryID,
			"jvm-app",
			"pom.xml",
			"org.springframework:spring-core",
			"maven",
			"${spring.version}",
			observedAt,
			map[string]any{
				"section":                     "dependencies",
				"dependency_scope":            "compile",
				"dependency_resolution_state": "unresolved",
				"dependency_unresolved_keys":  []any{"spring.version"},
				"direct_dependency":           true,
				"dependency_path_kind":        "manifest",
			},
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-101105"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactPossiblyAffected)
	if got.ObservedVersion != "" {
		t.Fatalf("ObservedVersion = %q, want blank for unresolved JVM manifest version", got.ObservedVersion)
	}
	if got.RequestedRange != "${spring.version}" {
		t.Fatalf("RequestedRange = %q, want raw unresolved declaration", got.RequestedRange)
	}
	if got.MatchReason != supplyChainVersionReasonRangeOnlyManifest {
		t.Fatalf("MatchReason = %q, want %q", got.MatchReason, supplyChainVersionReasonRangeOnlyManifest)
	}
	assertContainsString(t, got.MissingEvidence, supplyChainMissingInstalledVersion)
	if got.DetectionProfile != DetectionProfileComprehensive {
		t.Fatalf("DetectionProfile = %q, want comprehensive for unresolved JVM evidence", got.DetectionProfile)
	}
}

// jvmAffectedPackageRangeFact builds a vulnerability.affected_package
// fixture. advisory_id is set equal to cveID: every real collector source
// always sets it, so a fixture without it was never realistic collector
// output.
func jvmAffectedPackageRangeFact(
	factID string,
	cveID string,
	packageID string,
	packageName string,
	affectedRange string,
	fixedVersion string,
) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.VulnerabilityAffectedPackageFactKind,
		Payload: map[string]any{
			"cve_id":         cveID,
			"advisory_id":    cveID,
			"package_id":     packageID,
			"ecosystem":      "maven",
			"package_name":   packageName,
			"affected_range": affectedRange,
			"fixed_versions": []any{fixedVersion},
		},
	}
}
