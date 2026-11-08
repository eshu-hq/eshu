// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannercomponent

import (
	"os"
	"path/filepath"
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
func pinnedSatelliteManifest(t testing.TB, id string) eshucomponent.Manifest {
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

// setSatelliteArtifactImage replaces the image of one platform entry in a
// satellite manifest, failing the test when the platform is absent so a
// drift or stamp test always touches the slot the check consults.
func setSatelliteArtifactImage(t *testing.T, manifest *eshucomponent.Manifest, platform, image string) {
	t.Helper()

	for i, artifact := range manifest.Spec.Artifacts {
		if artifact.Platform == platform {
			manifest.Spec.Artifacts[i].Image = image
			return
		}
	}
	t.Fatalf("manifest for %q declares no %q slot", manifest.Metadata.ID, platform)
}

// TestVerifySatelliteSeededArtifactAdmitsPinnedSatellites proves admission
// lets a satellite manifest whose artifacts equal the registry pins through,
// for both satellite IDs on both pinned platforms (#10285).
func TestVerifySatelliteSeededArtifactAdmitsPinnedSatellites(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			t.Run(id+"/"+platform, func(t *testing.T) {
				t.Parallel()

				if err := VerifySatelliteSeededArtifact(pinnedSatelliteManifest(t, id), platform); err != nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want nil for the pinned %q artifact", id, err, platform)
				}
			})
		}
	}
}

// TestVerifySatelliteSeededArtifactDeniesDriftedArtifact proves a satellite
// manifest with a drifted host-platform artifact digest is denied
// fail-closed, without echoing either image value (#10285). The drift
// always poisons the checked platform's slot, so the denial does not depend
// on artifact order.
func TestVerifySatelliteSeededArtifactDeniesDriftedArtifact(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			t.Run(id+"/"+platform, func(t *testing.T) {
				t.Parallel()

				manifest := pinnedSatelliteManifest(t, id)
				drifted := driftedSatelliteImage(t, id)
				setSatelliteArtifactImage(t, &manifest, platform, drifted)
				err := VerifySatelliteSeededArtifact(manifest, platform)
				if err == nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for a drifted %q artifact", id, platform)
				}
				if !strings.Contains(err.Error(), "pinned") {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want it to name the pinned platform image", id, err)
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
}

// TestVerifySatelliteSeededArtifactDeniesMissingHostPlatform proves a
// satellite manifest with no artifact for the executing host's platform is
// denied fail-closed, even when every declared sibling slot is pinned
// (#10285).
func TestVerifySatelliteSeededArtifactDeniesMissingHostPlatform(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			manifest := pinnedSatelliteManifest(t, id)
			manifest.Spec.Artifacts = manifest.Spec.Artifacts[:1]
			checked := "linux/amd64"
			if manifest.Spec.Artifacts[0].Platform == checked {
				checked = "linux/arm64"
			}
			if err := VerifySatelliteSeededArtifact(manifest, checked); err == nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial with no %q artifact", id, checked)
			}
		})
	}
}

// TestVerifySatelliteSeededArtifactDeniesUnpinnedHostPlatform proves a check
// for a platform the registry does not pin is denied fail-closed (#10285).
func TestVerifySatelliteSeededArtifactDeniesUnpinnedHostPlatform(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			if err := VerifySatelliteSeededArtifact(pinnedSatelliteManifest(t, id), "linux/riscv64"); err == nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for an unpinned platform", id)
			}
		})
	}
}

// TestVerifySatelliteSeededArtifactAdmitsForeignSiblingEntries proves entries
// for platforms the check does not consult are ignored: a pinned host slot
// admits even next to a stale sibling or an unknown platform, which is the
// installed shape the seed flow leaves after a repin (#9877).
func TestVerifySatelliteSeededArtifactAdmitsForeignSiblingEntries(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			t.Run(id+"/"+platform, func(t *testing.T) {
				t.Parallel()

				manifest := pinnedSatelliteManifest(t, id)
				setSatelliteArtifactImage(t, &manifest, platform, SatelliteSeedPins()[id][platform])
				manifest.Spec.Artifacts = append(manifest.Spec.Artifacts, eshucomponent.Artifact{
					Platform: "linux/riscv64",
					Image:    staleSiblingImage(t, id),
				})
				if err := VerifySatelliteSeededArtifact(manifest, platform); err != nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want nil with a pinned %q slot", id, err, platform)
				}
			})
		}
	}
}

// TestVerifySatelliteSeededArtifactDeniesDuplicateHostPlatform proves a
// satellite manifest that repeats the host platform's entry is denied: with
// two competing images for the executing slot the check cannot tell which
// one the seed stamped, so ambiguity fails closed (#10285).
func TestVerifySatelliteSeededArtifactDeniesDuplicateHostPlatform(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			t.Run(id+"/"+platform, func(t *testing.T) {
				t.Parallel()

				manifest := pinnedSatelliteManifest(t, id)
				for _, artifact := range manifest.Spec.Artifacts {
					if artifact.Platform == platform {
						manifest.Spec.Artifacts = append(manifest.Spec.Artifacts, artifact)
					}
				}
				if err := VerifySatelliteSeededArtifact(manifest, platform); err == nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for a duplicated %q entry", id, platform)
				}
			})
		}
	}
}

// TestVerifySatelliteSeededArtifactDeniesMissingArtifacts proves a satellite
// manifest with no artifacts to compare is denied rather than admitted
// (#10285).
func TestVerifySatelliteSeededArtifactDeniesMissingArtifacts(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			t.Run(id+"/"+platform, func(t *testing.T) {
				t.Parallel()

				manifest := eshucomponent.Manifest{}
				manifest.Metadata.ID = id
				if err := VerifySatelliteSeededArtifact(manifest, platform); err == nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want fail-closed denial", id)
				}
			})
		}
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
			if err := VerifySatelliteSeededArtifact(manifest, "linux/amd64"); err != nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want nil (pass-through)", id, err)
			}
		})
	}
}

// seedCapableManifestIDs lists the component IDs of every collector caller
// manifest whose binary stamps --image-ref at seed time: a sibling seed.go
// next to manifest.yaml marks a seed-capable caller. A future third
// seed-capable caller appears here first, before any registry entry.
func seedCapableManifestIDs(t *testing.T, cmdRoot string) map[string]string {
	t.Helper()

	ids := map[string]string{}
	seeds, err := filepath.Glob(filepath.Join(cmdRoot, "collector-*", "seed.go"))
	if err != nil {
		t.Fatalf("glob seed owners: %v", err)
	}
	for _, seed := range seeds {
		manifestPath := filepath.Join(filepath.Dir(seed), "manifest.yaml")
		// #nosec G304 -- path derives from the repo glob, not operator input.
		raw, err := os.ReadFile(manifestPath)
		if err != nil {
			t.Fatalf("read %s: %v", manifestPath, err)
		}
		var manifest eshucomponent.Manifest
		if err := yaml.Unmarshal(raw, &manifest); err != nil {
			t.Fatalf("decode %s: %v", manifestPath, err)
		}
		if manifest.Metadata.ID == "" {
			t.Fatalf("manifest %s has no metadata id", manifestPath)
		}
		ids[manifest.Metadata.ID] = manifestPath
	}
	return ids
}

// BenchmarkVerifySatelliteSeededArtifact bounds the per-artifact admission
// cost the worker and planner paths pay: one map lookup plus a small platform
// set compare, measured for a pinned satellite and a pass-through ID (#10285).
func BenchmarkVerifySatelliteSeededArtifact(b *testing.B) {
	pinned := pinnedSatelliteManifest(b, SatelliteDocumentImportID)
	other := pinned
	other.Metadata.ID = "dev.eshu.collector.third-party"

	b.Run("pinned", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := VerifySatelliteSeededArtifact(pinned, "linux/amd64"); err != nil {
				b.Fatalf("admit pinned: %v", err)
			}
		}
	})
	b.Run("passthrough", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if err := VerifySatelliteSeededArtifact(other, "linux/amd64"); err != nil {
				b.Fatalf("admit third-party: %v", err)
			}
		}
	})
}

// TestSatelliteSeedRegistryCoversSeedCapableCallers is the reverse binding
// for TestSatelliteSeedPinsMatchCallerManifests: a future seed-capable
// caller manifest with no registry entry fails here instead of passing
// through admission silently (#10285).
func TestSatelliteSeedRegistryCoversSeedCapableCallers(t *testing.T) {
	t.Parallel()

	t.Run("every seed-capable caller has a registry entry", func(t *testing.T) {
		t.Parallel()

		for id, path := range seedCapableManifestIDs(t, "../../../cmd") {
			if _, ok := SatelliteSeedPins()[id]; !ok {
				t.Errorf("seed-capable caller %q (%s) has no satellite registry entry", id, path)
			}
		}
	})

	t.Run("an uncovered seed-capable caller is reported", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		dir := filepath.Join(root, "collector-fake")
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatalf("mkdir fake caller: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "seed.go"), []byte("package main\n"), 0o600); err != nil {
			t.Fatalf("write fake seed.go: %v", err)
		}
		const manifest = "metadata:\n  id: dev.eshu.collector.unregistered\n"
		if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
			t.Fatalf("write fake manifest.yaml: %v", err)
		}
		ids := seedCapableManifestIDs(t, root)
		if len(ids) != 1 {
			t.Fatalf("seed-capable ids = %d, want 1", len(ids))
		}
		for id := range ids {
			if _, ok := SatelliteSeedPins()[id]; ok {
				t.Fatalf("fake caller %q unexpectedly has a registry entry", id)
			}
		}
	})
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

// staleSiblingImage returns a well-formed seed image ref for one satellite
// that differs from every registry pin: the shape of a build-frozen sibling
// slot after a repin moved the pins past the image build commit.
func staleSiblingImage(t *testing.T, id string) string {
	t.Helper()

	repo := "ghcr.io/eshu-hq/eshu-collector-document-import"
	if id == SatelliteMediaImportID {
		repo = "ghcr.io/eshu-hq/eshu-collector-media-import"
	}
	ref := repo + "@sha256:" + strings.Repeat("e", 64)
	for _, pin := range SatelliteSeedPins()[id] {
		if ref == pin {
			t.Fatalf("stale sibling image equals a registry pin for %q, want a pre-repin ref", id)
		}
	}
	return ref
}

// seedStampedSatelliteManifest replays the installed manifest the chart seed
// path leaves after a repin: the stamped platform slot carries the fresh pin
// (seed writes --image-ref into the runner's own slot) while the sibling slot
// keeps the build-frozen pre-repin image (seed leaves sibling slots
// byte-identical by construction, see TestSeedWritesCollectorAndManifest).
func seedStampedSatelliteManifest(t *testing.T, id, platform string) eshucomponent.Manifest {
	t.Helper()

	manifest := pinnedSatelliteManifest(t, id)
	pins := SatelliteSeedPins()[id]
	if _, ok := pins[platform]; !ok {
		t.Fatalf("registry pins for %q declare no %q slot to stamp", id, platform)
	}
	stale := staleSiblingImage(t, id)
	matched := false
	for i, artifact := range manifest.Spec.Artifacts {
		if artifact.Platform == platform {
			manifest.Spec.Artifacts[i].Image = pins[platform]
			matched = true
			continue
		}
		manifest.Spec.Artifacts[i].Image = stale
	}
	if !matched {
		t.Fatalf("pinned manifest for %q declares no %q slot to stamp", id, platform)
	}
	return manifest
}

// TestVerifySatelliteSeededArtifactDeniesTrimVariantHostPlatform proves a
// non-canonical host-platform spelling never admits: a trim-variant entry
// carrying a drifted image is denied both alongside a clean pinned entry
// (duplicate/ambiguous host slot) and alone (non-canonical host slot), and a
// lone padded entry carrying the pinned image is denied by the canonical
// Platform == platform line, for both satellite IDs on both platforms (#9877
// arbiter ruling 2.4). The image picker matches on TrimSpace while
// Artifact.Validate accepts padded platforms, so exact-match candidate
// selection would silently skip the padded entry and admit — the
// padded-pinned subcase pins the fail-closed behavior of the canonical line,
// which neither drifted-image subcase exercises.
func TestVerifySatelliteSeededArtifactDeniesTrimVariantHostPlatform(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			t.Run(id+"/"+platform+"/alongside-clean", func(t *testing.T) {
				t.Parallel()

				manifest := pinnedSatelliteManifest(t, id)
				manifest.Spec.Artifacts = append(manifest.Spec.Artifacts, eshucomponent.Artifact{
					Platform: " " + platform,
					Image:    driftedSatelliteImage(t, id),
				})
				if err := VerifySatelliteSeededArtifact(manifest, platform); err == nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for a trim-variant %q entry alongside the pinned slot", id, platform)
				}
			})
			t.Run(id+"/"+platform+"/alone", func(t *testing.T) {
				t.Parallel()

				manifest := pinnedSatelliteManifest(t, id)
				var kept []eshucomponent.Artifact
				for _, artifact := range manifest.Spec.Artifacts {
					if artifact.Platform != platform {
						kept = append(kept, artifact)
					}
				}
				stripped := kept
				manifest.Spec.Artifacts = append(stripped, eshucomponent.Artifact{
					Platform: platform + " ",
					Image:    driftedSatelliteImage(t, id),
				})
				if err := VerifySatelliteSeededArtifact(manifest, platform); err == nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for a lone trim-variant %q entry", id, platform)
				}
			})
			t.Run(id+"/"+platform+"/padded-pinned", func(t *testing.T) {
				t.Parallel()

				manifest := pinnedSatelliteManifest(t, id)
				var kept []eshucomponent.Artifact
				for _, artifact := range manifest.Spec.Artifacts {
					if artifact.Platform != platform {
						kept = append(kept, artifact)
					}
				}
				manifest.Spec.Artifacts = append(kept, eshucomponent.Artifact{
					Platform: " " + platform + " ",
					Image:    SatelliteSeedPins()[id][platform],
				})
				if err := VerifySatelliteSeededArtifact(manifest, platform); err == nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for a lone padded %q entry carrying the pinned image", id, platform)
				}
			})
		}
	}
}

// TestVerifySatelliteSeededArtifactDeniesEmptyPlatform proves an empty
// platform argument denies fail-closed through the same pin-miss path as an
// unpinned platform (#9877 arbiter ruling 3.1).
func TestVerifySatelliteSeededArtifactDeniesEmptyPlatform(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		t.Run(id, func(t *testing.T) {
			t.Parallel()

			if err := VerifySatelliteSeededArtifact(pinnedSatelliteManifest(t, id), ""); err == nil {
				t.Fatalf("VerifySatelliteSeededArtifact(%q) error = nil, want denial for an empty platform argument", id)
			}
		})
	}
}

// TestVerifySatelliteSeededArtifactAdmitsSeedStampedSibling proves admission
// accepts the manifest the real seed flow installs after a repin: the local
// slot carries the fresh stamp while the sibling slot keeps its build-frozen
// pre-repin image. QA 2026-11-08 refused exactly this shape on amd64 nodes
// for document_import (stale arm64 slot) and media_import (stale arm64 slot),
// CrashLooping the Path-A core and both workers (#9877).
func TestVerifySatelliteSeededArtifactAdmitsSeedStampedSibling(t *testing.T) {
	t.Parallel()

	for _, id := range satelliteIDs() {
		for _, platform := range []string{"linux/amd64", "linux/arm64"} {
			t.Run(id+"/"+platform, func(t *testing.T) {
				t.Parallel()

				if err := VerifySatelliteSeededArtifact(seedStampedSatelliteManifest(t, id, platform), platform); err != nil {
					t.Fatalf("VerifySatelliteSeededArtifact(%q) error = %v, want nil for the seed-stamped %q slot", id, err, platform)
				}
			})
		}
	}
}
