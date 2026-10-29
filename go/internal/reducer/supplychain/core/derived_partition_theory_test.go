// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"fmt"
	"slices"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// BenchmarkDerivedScanPartitionTheory measures the existing mixed classifier
// against target-local calls to the same production classifier.
func BenchmarkDerivedScanPartitionTheory(b *testing.B) {
	const digest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	public := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	for _, targets := range []int{1, 2, 16} {
		partitioned := make([][]facts.Envelope, 0, targets)
		authorized := make([]AuthorizedScanImpactEvidence, 0, targets)
		mixed := make([]facts.Envelope, 0, len(public)+2*targets)
		mixed = append(mixed, public...)
		for i := range targets {
			target := fmt.Sprintf("image://derived/%064x", i+1)
			input := append(slices.Clone(public), derivedPartitionScan(
				fmt.Sprintf("bench-%d", i), target, "generation-bench", digest, "3.0.11-1~deb12u2",
			)...)
			partitioned = append(partitioned, input)
			authorized = append(authorized, AuthorizedScanImpactEvidence{
				ScanTargetID: target, ScanGenerationID: "generation-bench",
				OSPackages: []facts.Envelope{input[len(public)]}, Analyses: []facts.Envelope{input[len(public)+1]},
				Advisories: public, AuthorityDependencies: []AdvisoryAuthorityDependency{{
					Kind: "public_registration", ID: "registration-1", RevisionID: "revision-1",
					AdvisoryScopeID: public[0].ScopeID, AdvisoryGenerationID: public[0].GenerationID,
					ConfigFingerprint: "config-1", ProviderID: "debian", TrustedSourceClass: "debian",
				}}, Complete: true,
			})
			mixed = append(mixed, input[len(public):]...)
		}
		b.Run(fmt.Sprintf("mixed_%d", targets), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = BuildSupplyChainImpactFindings(mixed)
			}
		})
		b.Run(fmt.Sprintf("authorized_shared_%d", targets), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if _, err := BuildAuthorizedScanImpactFindings(nil, authorized); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(fmt.Sprintf("target_local_%d", targets), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				for _, input := range partitioned {
					_ = BuildSupplyChainImpactFindings(input)
				}
			}
		})
	}
}

func derivedPartitionAdvisory(id, scope, fixedVersion string) []facts.Envelope {
	cve := vulnerabilityCVEFactWithProvenance(
		"cve-"+id, "CVE-2026-9299", "debian", "DSA-2026-9299", 7.5, "", "HIGH", "2026-10-28T00:00:00Z",
	)
	affected := vulnerabilityAffectedPackageFactWithSource(
		"affected-"+id, "CVE-2026-9299", "debian", "DSA-2026-9299",
		"pkg:deb/debian/openssl", "deb", "openssl", "3.0.11-1~deb12u2", fixedVersion,
	)
	for _, envelope := range []*facts.Envelope{&cve, &affected} {
		envelope.ScopeID = scope
		envelope.GenerationID = "generation-" + id
	}
	return []facts.Envelope{cve, affected}
}

func derivedPartitionScan(id, target, generation, digest, version string) []facts.Envelope {
	osPackage := osPackageFact("os-"+id, target, map[string]any{
		"distro":                 "debian",
		"distro_version":         "12",
		"package_manager":        "dpkg",
		"name":                   "openssl",
		"arch":                   "amd64",
		"repository_class":       "vendor",
		"vendor_advisory_source": "debian",
		"installed_version_raw":  version,
		"purl":                   "pkg:deb/debian/openssl@" + version + "?arch=amd64&distro=debian-12",
	})
	osPackage.GenerationID = generation
	osPackage.SourceRef.SourceRecordID = target
	analysis := scannerWorkerAnalysisFact(target, generation, digest, "registry.example/app@"+digest)
	analysis.SourceRef.SourceRecordID = target
	return []facts.Envelope{osPackage, analysis}
}

// BenchmarkDerivedScanSliceIdentityTheory measures a cache hit when the
// authorized loader reuses one immutable advisory slice for public evidence.
// A distinct slice must take the full-content fallback in production.
func BenchmarkDerivedScanSliceIdentityTheory(b *testing.B) {
	advisories := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	type sliceIdentity struct {
		first  *facts.Envelope
		length int
	}
	cache := map[sliceIdentity]struct{}{{first: &advisories[0], length: len(advisories)}: {}}
	b.Run("full_json_key", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, err := advisoryIndexKey(advisories); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("same_slice_identity_hit", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			if _, ok := cache[sliceIdentity{first: &advisories[0], length: len(advisories)}]; !ok {
				b.Fatal("same immutable advisory slice missed cache")
			}
		}
	})
}

// BenchmarkDerivedScanAdvisoryPreparationTheory measures the actual
// deduplication, ordering, and content key work for one shared public slice.
func BenchmarkDerivedScanAdvisoryPreparationTheory(b *testing.B) {
	advisories := derivedPartitionAdvisory("public", "vuln-intel://nvd/cve", "3.0.11-1~deb12u3")
	type sliceIdentity struct {
		first  *facts.Envelope
		length int
	}
	prepare := func() {
		ordered, err := deduplicateScanImpactEnvelopes(advisories)
		if err != nil {
			b.Fatal(err)
		}
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].ScopeID != ordered[j].ScopeID {
				return ordered[i].ScopeID < ordered[j].ScopeID
			}
			return ordered[i].FactID < ordered[j].FactID
		})
		if _, err := advisoryIndexKey(ordered); err != nil {
			b.Fatal(err)
		}
	}
	b.Run("full_preparation_16", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			for range 16 {
				prepare()
			}
		}
	})
	b.Run("same_immutable_slice_16", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			cache := make(map[sliceIdentity]struct{}, 1)
			for range 16 {
				key := sliceIdentity{first: &advisories[0], length: len(advisories)}
				if _, exists := cache[key]; exists {
					continue
				}
				prepare()
				cache[key] = struct{}{}
			}
		}
	})
}
