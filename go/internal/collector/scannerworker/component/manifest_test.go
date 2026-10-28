// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
)

// TestManifestForDeclaresPerAnalyzerIdentities pins the three component
// identities: distinct IDs, the v1alpha3 process-adapter contract, and the
// exact fact families each analyzer can emit (image and osruntime truthfully
// share the vulnerability kinds both analyzers produce; grants stay
// per-analyzer regardless).
func TestManifestForDeclaresPerAnalyzerIdentities(t *testing.T) {
	t.Parallel()

	kinds := map[scannerworker.AnalyzerKind]struct {
		id     string
		family []string
	}{
		scannerworker.AnalyzerImageUnpacking: {
			id:     IDImage,
			family: []string{"scanner_worker.analysis", "scanner_worker.warning", "vulnerability.os_package", "vulnerability.warning"},
		},
		scannerworker.AnalyzerSBOMGeneration: {
			id:     IDSBOM,
			family: []string{"sbom.document", "sbom.component", "sbom.warning"},
		},
		scannerworker.AnalyzerOSPackageExtraction: {
			id:     IDOSRuntime,
			family: []string{"vulnerability.os_package", "vulnerability.warning"},
		},
	}
	for kind, want := range kinds {
		manifest, err := ManifestFor(kind)
		if err != nil {
			t.Fatalf("ManifestFor(%q) error = %v", kind, err)
		}
		if manifest.Metadata.ID != want.id {
			t.Fatalf("ManifestFor(%q) id = %q, want %q", kind, manifest.Metadata.ID, want.id)
		}
		if manifest.Spec.Runtime.SDKProtocol != CollectorSDKProtocolV1Alpha3 {
			t.Fatalf("ManifestFor(%q) protocol = %q, want v1alpha3", kind, manifest.Spec.Runtime.SDKProtocol)
		}
		if manifest.Spec.Runtime.Adapter != "process" {
			t.Fatalf("ManifestFor(%q) adapter = %q, want process", kind, manifest.Spec.Runtime.Adapter)
		}
		if len(manifest.Spec.EmittedFacts) != len(want.family) {
			t.Fatalf("ManifestFor(%q) families = %d, want %d", kind, len(manifest.Spec.EmittedFacts), len(want.family))
		}
		for i, family := range manifest.Spec.EmittedFacts {
			if family.Kind != want.family[i] {
				t.Fatalf("ManifestFor(%q) family[%d] = %q, want %q", kind, i, family.Kind, want.family[i])
			}
			if len(family.SchemaVersions) != 1 || len(family.SourceConfidence) != 1 {
				t.Fatalf("ManifestFor(%q) family[%d] wants one schema version and confidence", kind, i)
			}
		}
	}
}

// TestManifestForRejectsReducerLane proves reducer-owned analyzers never get
// a component manifest: the binaries only execute the scanner lane.
func TestManifestForRejectsReducerLane(t *testing.T) {
	t.Parallel()

	for _, kind := range []scannerworker.AnalyzerKind{
		scannerworker.AnalyzerVulnerabilityMatching,
		scannerworker.AnalyzerCoverageReadiness,
		scannerworker.AnalyzerSecurityPriority,
	} {
		if _, err := ManifestFor(kind); err == nil {
			t.Fatalf("ManifestFor(%q) error = nil, want reducer-lane rejection", kind)
		}
	}
}

// TestRequiredGrantsCoverEmittedKinds proves every declared fact family has
// a matching producer grant pinned to the manifest id and version.
func TestRequiredGrantsCoverEmittedKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range []scannerworker.AnalyzerKind{
		scannerworker.AnalyzerImageUnpacking,
		scannerworker.AnalyzerSBOMGeneration,
		scannerworker.AnalyzerOSPackageExtraction,
	} {
		manifest, err := ManifestFor(kind)
		if err != nil {
			t.Fatalf("ManifestFor(%q) error = %v", kind, err)
		}
		grants := RequiredGrants(manifest)
		if len(grants) != len(manifest.Spec.EmittedFacts) {
			t.Fatalf("ManifestFor(%q): grants = %d, want %d", kind, len(grants), len(manifest.Spec.EmittedFacts))
		}
		for i, grant := range grants {
			family := manifest.Spec.EmittedFacts[i]
			if grant.ProducerID != manifest.Metadata.ID {
				t.Fatalf("grant[%d].ProducerID = %q, want %q", i, grant.ProducerID, manifest.Metadata.ID)
			}
			if grant.Kind != family.Kind {
				t.Fatalf("grant[%d].Kind = %q, want %q", i, grant.Kind, family.Kind)
			}
			if grant.Revoked {
				t.Fatalf("grant[%d] revoked, want live", i)
			}
			if grant.Scope == "" {
				t.Fatalf("grant[%d] has empty scope", i)
			}
		}
	}
}

// TestAnalyzerForRoundTrips proves component IDs resolve back to their
// analyzer and unknown IDs fail closed.
func TestAnalyzerForRoundTrips(t *testing.T) {
	t.Parallel()

	for _, kind := range []scannerworker.AnalyzerKind{
		scannerworker.AnalyzerImageUnpacking,
		scannerworker.AnalyzerSBOMGeneration,
		scannerworker.AnalyzerOSPackageExtraction,
	} {
		manifest, err := ManifestFor(kind)
		if err != nil {
			t.Fatalf("ManifestFor(%q) error = %v", kind, err)
		}
		got, err := AnalyzerFor(manifest.Metadata.ID)
		if err != nil {
			t.Fatalf("AnalyzerFor(%q) error = %v", manifest.Metadata.ID, err)
		}
		if got != kind {
			t.Fatalf("AnalyzerFor(%q) = %q, want %q", manifest.Metadata.ID, got, kind)
		}
	}
	if _, err := AnalyzerFor("eshu.scanner.unknown"); err == nil {
		t.Fatal("AnalyzerFor(unknown) error = nil, want fail-closed")
	}
}
