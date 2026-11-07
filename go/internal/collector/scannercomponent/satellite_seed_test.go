// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	eshucomponent "github.com/eshu-hq/eshu/go/internal/component"
)

// satelliteIDs lists every satellite component identity the host-side seed
// pin registry covers: the two Event-B seed-capable documentation satellites.
func satelliteIDs() []string {
	return []string{SatelliteDocumentImportID, SatelliteMediaImportID}
}

// pinnedSatelliteManifest builds a seeded manifest for one satellite whose
// artifacts equal the registry pins: exactly what an uncompromised chart
// seed.image stamps.
func pinnedSatelliteManifest(t *testing.T, id string) eshucomponent.Manifest {
	t.Helper()

	pins := SatelliteSeedPins()[id]
	if len(pins) == 0 {
		t.Fatalf("SatelliteSeedPins()[%q] is empty, want one pin per platform", id)
	}
	manifest := eshucomponent.Manifest{}
	manifest.Metadata.ID = id
	for platform, image := range pins {
		manifest.Spec.Artifacts = append(manifest.Spec.Artifacts, eshucomponent.Artifact{
			Platform: platform,
			Image:    image,
		})
	}
	return manifest
}

// driftedSatelliteImage returns a well-formed seed image for one satellite
// that differs from every registry pin: the shape a drifted live-chart
// seed.image stamps, and what admission must deny.
func driftedSatelliteImage(t *testing.T, id string) string {
	t.Helper()

	pins := SatelliteSeedPins()[id]
	repo := "ghcr.io/eshu-hq/eshu-collector-document-import"
	if id == SatelliteMediaImportID {
		repo = "ghcr.io/eshu-hq/eshu-collector-media-import"
	}
	ref := repo + "@sha256:" + strings.Repeat("d", 64)
	for _, pin := range pins {
		if ref == pin {
			t.Fatalf("drifted seed image equals a registry pin for %q, want a differing ref", id)
		}
	}
	return ref
}

// TestVerifySatelliteSeededArtifactAdmitsPinnedSatellites proves admission
// lets a satellite manifest whose artifacts equal the registry pins through,
// for both satellite IDs (#10285).
func TestVerifySatelliteSeededArtifactAdmitsPinnedSatellites(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			if err := VerifySatelliteSeededArtifact(pinnedSatelliteManifest(t, id)); err != nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want nil for the pinned artifacts", id, err)
			}
		})
	}
}

// TestVerifySatelliteSeededArtifactDeniesDriftedArtifact proves a satellite
// manifest with a drifted artifact digest is denied fail-closed, without
// echoing either image value (#10285).
func TestVerifySatelliteSeededArtifactDeniesDriftedArtifact(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			manifest := pinnedSatelliteManifest(t, id)
			drifted := driftedSatelliteImage(t, id)
			manifest.Spec.Artifacts[0].Image = drifted
			err := VerifySatelliteSeededArtifact(manifest)
			if err == nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for a drifted artifact", id)
			}
			if !strings.Contains(err.Error(), "pinned") {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want it to name the pinned platform images", id, err)
			}
			for _, secret := range []string{drifted} {
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, must not echo an image value", id, err)
				}
			}
			for _, pin := range SatelliteSeedPins()[id] {
				if strings.Contains(err.Error(), pin) {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, must not echo a pinned image value", id, err)
				}
			}
		})
	}
}

// TestVerifySatelliteSeededArtifactDeniesPlatformMismatch proves a satellite
// manifest carrying an artifact for a platform the registry does not pin is
// denied fail-closed (#10285).
func TestVerifySatelliteSeededArtifactDeniesPlatformMismatch(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			manifest := pinnedSatelliteManifest(t, id)
			manifest.Spec.Artifacts[0].Platform = "linux/riscv64"
			if err := VerifySatelliteSeededArtifact(manifest); err == nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for an unpinned platform", id)
			}
		})
	}
}

// TestVerifySatelliteSeededArtifactDeniesMissingArtifacts proves a satellite
// manifest with no artifacts to compare is denied rather than admitted
// (#10285).
func TestVerifySatelliteSeededArtifactDeniesMissingArtifacts(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			manifest := eshucomponent.Manifest{}
			manifest.Metadata.ID = id
			if err := VerifySatelliteSeededArtifact(manifest); err == nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want fail-closed denial", id)
			}
		})
	}
}

// TestVerifySatelliteSeededArtifactIgnoresOtherIDs proves component families
// outside the satellite registry pass through untouched: the pin check owns
// only the two satellite IDs.
func TestVerifySatelliteSeededArtifactIgnoresOtherIDs(t *testing.T) {
	t.Parallel()

	for _, id := range []string{"dev.eshu.examples.scorecard", "eshu.scanner.image", "dev.eshu.collector.grafana"} {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			manifest := eshucomponent.Manifest{}
			manifest.Metadata.ID = id
			if err := VerifySatelliteSeededArtifact(manifest); err != nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want nil (pass-through)", id, err)
			}
		})
	}
}

// TestSatelliteSeedPinsMatchCallerManifests binds the host-side registry to
// the checked-in caller manifests: a re-pin PR that updates a caller
// manifest.yaml without bumping the registry in the same commit fails here,
// so the two can never drift apart silently (#10285).
func TestSatelliteSeedPinsMatchCallerManifests(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		SatelliteDocumentImportID: "../../../cmd/collector-document-import/manifest.yaml",
		SatelliteMediaImportID:    "../../../cmd/collector-media-import/manifest.yaml",
	}
	for id, path := range cases {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			// #nosec G304 -- path is a fixed test-fixture reference, not operator input.
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			var manifest eshucomponent.Manifest
			if err := yaml.Unmarshal(raw, &manifest); err != nil {
				t.Fatalf("decode %s: %v", path, err)
			}
			if manifest.Metadata.ID != id {
				t.Fatalf("manifest id = %q, want %q", manifest.Metadata.ID, id)
			}
			pins := SatelliteSeedPins()[id]
			if len(manifest.Spec.Artifacts) != len(pins) {
				t.Fatalf("manifest artifacts = %d, registry pins = %d, want equality", len(manifest.Spec.Artifacts), len(pins))
			}
			for _, artifact := range manifest.Spec.Artifacts {
				want, ok := pins[artifact.Platform]
				if !ok {
					t.Fatalf("artifact platform %q has no registry pin for %q", artifact.Platform, id)
				}
				if artifact.Image != want {
					t.Fatalf("artifact image for %q/%s differs from the registry pin", id, artifact.Platform)
				}
			}
		})
	}
}
