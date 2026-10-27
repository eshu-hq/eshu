// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/supplychainmodel"
)

// BenchmarkSBOMSubjectOSLookup isolates classification after index construction
// with one relevant OS row and many unrelated package keys.
func BenchmarkSBOMSubjectOSLookup(b *testing.B) {
	const (
		packageID = "pkg:rpm/redhat/openssl"
		digest    = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	os := osPackageFact("os-bench", "scan-os-bench", map[string]any{
		"distro": "redhat", "distro_version": "9.2", "package_manager": "rpm",
		"name": "openssl", "epoch": "1", "upstream_version": "3.0.7",
		"distro_release": "18.el9_2", "arch": "x86_64", "source_package": "openssl",
		"repository_class": "vendor", "vendor_advisory_source": "redhat",
		"installed_version_raw": "1:3.0.7-18.el9_2",
		"purl":                  "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2?arch=x86_64&distro=redhat-9.2",
	})
	os.GenerationID = "generation-os-bench"
	input := []facts.Envelope{
		vulnerabilityCVEFactWithProvenance("cve-os-bench", "CVE-2026-91914", "redhat", "RHSA-2026:91914", 7.8, "", "HIGH", "2026-05-31T12:00:00Z"),
		vulnerabilityAffectedPackageFactWithSource("affected-os-bench", "CVE-2026-91914", "redhat", "RHSA-2026:91914", packageID, "redhat", "openssl", "1:3.0.7-18.el9_2", "1:3.0.7-20.el9_2"),
		sbomComponentImpactFactWithPackageIDVersion("component-os-bench", "doc-os-bench", "pkg:rpm/redhat/openssl@1:3.0.7-18.el9_2", packageID, "1:3.0.7-18.el9_2"),
		sbomAttachmentImpactFact("attachment-os-bench", "doc-os-bench", digest),
		containerImageIdentityImpactFact("image-os-bench", digest, testImpactRepositoryID),
		os,
		scannerWorkerAnalysisFact(os.ScopeID, os.GenerationID, digest, "registry.example/os@"+digest),
	}
	base, quarantined, err := buildSupplyChainImpactIndexWithQuarantine(input)
	if err != nil || len(quarantined) != 0 {
		b.Fatalf("index: %v, quarantined=%d", err, len(quarantined))
	}
	cves := groupSupplyChainCVEsByID(base.cves)["CVE-2026-91914"]
	pkgs := base.affectedPackages["CVE-2026-91914"]
	for _, unrelated := range []int{0, 100, 10000} {
		b.Run(fmt.Sprintf("unrelated_%d", unrelated), func(b *testing.B) {
			index := base
			index.osPackages = make(map[string][]supplychainmodel.OSPackage, unrelated+1)
			for key, rows := range base.osPackages {
				index.osPackages[key] = rows
			}
			for i := range unrelated {
				key := fmt.Sprintf("pkg:rpm/redhat/other-%05d", i)
				index.osPackages[key] = []supplychainmodel.OSPackage{{
					FactID: key, PackageID: key, Distro: "redhat", DistroVersion: "9.2",
					PackageManager: "rpm", Arch: "x86_64", InstalledVersion: "1:3.0.7-18.el9_2",
					RepositoryClass: "vendor", VendorAdvisorySource: "redhat",
				}}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				findings := appendSupplyChainImpactPackageFindings(nil, cves, pkgs, index)
				if len(findings) != 1 || findings[0].SubjectDigest != digest {
					b.Fatalf("findings=%#v", findings)
				}
			}
		})
	}
}
