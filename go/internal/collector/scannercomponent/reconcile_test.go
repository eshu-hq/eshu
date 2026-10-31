// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"strings"
	"testing"

	eshucomponent "github.com/eshu-hq/eshu/go/internal/component"
)

// pinnedSeedImage returns the digest-pinned artifact image ManifestFor
// declares for one analyzer.
func pinnedSeedImage(t *testing.T, kind AnalyzerKind) string {
	t.Helper()

	manifest, err := ManifestFor(kind)
	if err != nil {
		t.Fatalf("ManifestFor(%q) error = %v, want nil", kind, err)
	}
	if len(manifest.Spec.Artifacts) != 1 {
		t.Fatalf("ManifestFor(%q) declares %d artifacts, want exactly 1", kind, len(manifest.Spec.Artifacts))
	}
	return manifest.Spec.Artifacts[0].Image
}

// nonPinSeedImage returns a well-formed seed image for one analyzer that
// differs from the compiled ManifestFor pin: the shape a stale chart
// seed.image stamps, and what admission must deny. The digest is fixed so
// the test stays deterministic; the guard proves it is not the pin.
func nonPinSeedImage(t *testing.T, kind AnalyzerKind) string {
	t.Helper()

	id, err := componentID(kind)
	if err != nil {
		t.Fatalf("componentID(%q) error = %v", kind, err)
	}
	ref := "ghcr.io/eshu-hq/scanner-" + strings.TrimPrefix(id, "eshu.scanner.") + "@sha256:" + strings.Repeat("b", 64)
	if ref == pinnedSeedImage(t, kind) {
		t.Fatalf("non-pin seed image equals the ManifestFor pin for %q, want a differing ref", kind)
	}
	return ref
}

// TestVerifySeededArtifactAdmitsPinnedManifest proves the reconcile check
// admits a seeded manifest whose artifact equals the platform ManifestFor
// pin, for all three scanner kinds: equality with the pin is the admission
// contract (#8878).
func TestVerifySeededArtifactAdmitsPinnedManifest(t *testing.T) {
	t.Parallel()

	for _, kind := range []AnalyzerKind{
		AnalyzerImageUnpacking,
		AnalyzerSBOMGeneration,
		AnalyzerOSPackageExtraction,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			manifest, err := ManifestFor(kind)
			if err != nil {
				t.Fatalf("ManifestFor(%q) error = %v", kind, err)
			}
			if err := VerifySeededArtifact(manifest); err != nil {
				t.Fatalf("VerifySeededArtifact(%q) error = %v, want nil for the pinned artifact", kind, err)
			}
		})
	}
}

// TestVerifySeededArtifactDeniesTamperedArtifact proves a seeded manifest
// whose artifact differs from the platform pin is denied fail-closed for
// all three kinds, without echoing either image value: the seeded ref is
// exactly what Seed stamps, so a well-formed but drifted seed image must
// stop at admission, before any claim or execution.
func TestVerifySeededArtifactDeniesTamperedArtifact(t *testing.T) {
	t.Parallel()

	for _, kind := range []AnalyzerKind{
		AnalyzerImageUnpacking,
		AnalyzerSBOMGeneration,
		AnalyzerOSPackageExtraction,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			manifest, err := ManifestFor(kind)
			if err != nil {
				t.Fatalf("ManifestFor(%q) error = %v", kind, err)
			}
			pin := manifest.Spec.Artifacts[0].Image
			drifted := nonPinSeedImage(t, kind)
			manifest.Spec.Artifacts[0].Image = drifted
			err = VerifySeededArtifact(manifest)
			if err == nil {
				t.Fatalf("VerifySeededArtifact(%q) error = nil, want denial for a drifted artifact", kind)
			}
			if !strings.Contains(err.Error(), "pinned") {
				t.Fatalf("VerifySeededArtifact(%q) error = %v, want it to name the pinned platform image", kind, err)
			}
			for _, secret := range []string{pin, drifted} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("VerifySeededArtifact(%q) error = %v, must not echo an image value", kind, err)
				}
			}
		})
	}
}

// TestVerifySeededArtifactDeniesPlatformMismatch proves a seeded manifest
// whose artifact keeps the pinned image but carries a non-pin platform is
// denied fail-closed for all three kinds: the platform-equality disjunct of
// the admission check is pinned, so dropping it breaks this test (#9539).
func TestVerifySeededArtifactDeniesPlatformMismatch(t *testing.T) {
	t.Parallel()

	for _, kind := range []AnalyzerKind{
		AnalyzerImageUnpacking,
		AnalyzerSBOMGeneration,
		AnalyzerOSPackageExtraction,
	} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			manifest, err := ManifestFor(kind)
			if err != nil {
				t.Fatalf("ManifestFor(%q) error = %v", kind, err)
			}
			pin := manifest.Spec.Artifacts[0].Image
			if manifest.Spec.Artifacts[0].Platform == "linux/amd64" {
				t.Fatalf("ManifestFor(%q) platform is already linux/amd64, want a distinct non-pin platform for the mismatch case", kind)
			}
			manifest.Spec.Artifacts[0].Platform = "linux/amd64"
			err = VerifySeededArtifact(manifest)
			if err == nil {
				t.Fatalf("VerifySeededArtifact(%q) error = nil, want denial for a platform-mismatched artifact", kind)
			}
			if strings.Contains(err.Error(), pin) {
				t.Fatalf("VerifySeededArtifact(%q) error = %v, must not echo an image value", kind, err)
			}
		})
	}
}

// TestVerifySeededArtifactIgnoresNonScannerManifest proves component
// families outside eshu.scanner.* pass through untouched: the pin check
// owns only the scanner family.
func TestVerifySeededArtifactIgnoresNonScannerManifest(t *testing.T) {
	t.Parallel()

	manifest := eshucomponent.Manifest{}
	manifest.Metadata.ID = "dev.eshu.examples.scorecard"
	if err := VerifySeededArtifact(manifest); err != nil {
		t.Fatalf("VerifySeededArtifact(scorecard) error = %v, want nil (pass-through)", err)
	}
}

// TestVerifySeededArtifactDeniesUnknownScannerID proves an eshu.scanner.*
// identity with no analyzer mapping is denied, never admitted.
func TestVerifySeededArtifactDeniesUnknownScannerID(t *testing.T) {
	t.Parallel()

	manifest := eshucomponent.Manifest{}
	manifest.Metadata.ID = "eshu.scanner.unknown"
	if err := VerifySeededArtifact(manifest); err == nil {
		t.Fatal("VerifySeededArtifact(unknown scanner) error = nil, want fail-closed denial")
	}
}

// TestVerifySeededArtifactDeniesMissingArtifact proves a scanner manifest
// with no image artifact to compare is denied rather than admitted.
func TestVerifySeededArtifactDeniesMissingArtifact(t *testing.T) {
	t.Parallel()

	manifest, err := ManifestFor(AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	manifest.Spec.Artifacts = nil
	if err := VerifySeededArtifact(manifest); err == nil {
		t.Fatal("VerifySeededArtifact(no artifact) error = nil, want fail-closed denial")
	}
}
