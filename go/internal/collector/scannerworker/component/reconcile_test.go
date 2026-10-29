// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	eshucomponent "github.com/eshu-hq/eshu/go/internal/component"
)

// TestVerifySeededArtifactAdmitsPinnedManifest proves the reconcile check
// admits a seeded manifest whose artifact equals the platform ManifestFor
// pin, for all three scanner kinds: equality with the pin is the admission
// contract (#8878).
func TestVerifySeededArtifactAdmitsPinnedManifest(t *testing.T) {
	t.Parallel()

	for _, kind := range []scannerworker.AnalyzerKind{
		scannerworker.AnalyzerImageUnpacking,
		scannerworker.AnalyzerSBOMGeneration,
		scannerworker.AnalyzerOSPackageExtraction,
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

	for _, kind := range []scannerworker.AnalyzerKind{
		scannerworker.AnalyzerImageUnpacking,
		scannerworker.AnalyzerSBOMGeneration,
		scannerworker.AnalyzerOSPackageExtraction,
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

	manifest, err := ManifestFor(scannerworker.AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	manifest.Spec.Artifacts = nil
	if err := VerifySeededArtifact(manifest); err == nil {
		t.Fatal("VerifySeededArtifact(no artifact) error = nil, want fail-closed denial")
	}
}
