// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"testing"
	"time"

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

// TestManifestArtifactPinsPublishTimeDigestContract pins the activation
// contract for component images: one linux/arm64 artifact per manifest,
// addressed as ghcr.io/eshu-hq/scanner-<name> pinned to an immutable
// sha256 digest. The digest is the documented placeholder until the images
// publish; the shape (registry, name, digest pinning) is what the publish
// step must preserve when it swaps in the real digest.
func TestManifestArtifactPinsPublishTimeDigestContract(t *testing.T) {
	t.Parallel()

	names := map[scannerworker.AnalyzerKind]string{
		scannerworker.AnalyzerImageUnpacking:      "image",
		scannerworker.AnalyzerSBOMGeneration:      "sbom",
		scannerworker.AnalyzerOSPackageExtraction: "osruntime",
	}
	for kind, name := range names {
		manifest, err := ManifestFor(kind)
		if err != nil {
			t.Fatalf("ManifestFor(%q) error = %v", kind, err)
		}
		if len(manifest.Spec.Artifacts) != 1 {
			t.Fatalf("ManifestFor(%q) artifacts = %d, want exactly one pinned image", kind, len(manifest.Spec.Artifacts))
		}
		artifact := manifest.Spec.Artifacts[0]
		if artifact.Platform != "linux/arm64" {
			t.Fatalf("ManifestFor(%q) platform = %q, want linux/arm64", kind, artifact.Platform)
		}
		want := "ghcr.io/eshu-hq/scanner-" + name + "@sha256:" + placeholderArtifactDigest
		if artifact.Image != want {
			t.Fatalf("ManifestFor(%q) image = %q, want %q", kind, artifact.Image, want)
		}
		if placeholderArtifactDigest != strings.Repeat("a", 64) {
			t.Fatalf("placeholderArtifactDigest = %q, want the 64-hex never-pulled convention", placeholderArtifactDigest)
		}
	}
}

// TestRequiredGrantsAreLive pins that the required grant set stays usable:
// every grant expires in the future with renewal margin, so fixtures do
// not rot into fail-closed expiry denials. Production lifetimes come from
// `eshu component grant` activation policy, not this constructor.
func TestRequiredGrantsAreLive(t *testing.T) {
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
		for i, grant := range RequiredGrants(manifest) {
			if !grant.ExpiresAt.After(time.Now().Add(24 * time.Hour)) {
				t.Fatalf("ManifestFor(%q) grant[%d] expires at %s, want live with renewal margin", kind, i, grant.ExpiresAt.Format(time.RFC3339))
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
