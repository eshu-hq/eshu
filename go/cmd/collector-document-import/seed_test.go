// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// seedTestManifest writes a minimal document-import ComponentPackage with a
// placeholder seed image and returns its path. Tests stamp it through the
// seed path instead of touching the repository manifest. Callers build the
// destination path inline from their own t.TempDir()-derived directory and
// pass the joined path here, so the path root stays visible at the call
// site.
func seedTestManifest(t *testing.T, path string) string {
	t.Helper()
	raw := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.collector.document_import
  name: Document import collector test
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - documentation
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/eshu-collector-document-import@sha256:0000000000000000000000000000000000000000000000000000000000000000
    - platform: linux/arm64
      image: ghcr.io/eshu-hq/eshu-collector-document-import@sha256:1111111111111111111111111111111111111111111111111111111111111111
  emittedFacts:
    - kind: documentation_document
      payloadSchemaRef: documentation_document
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - observed
    - kind: documentation_section
      payloadSchemaRef: documentation_section
      schemaVersions:
        - 1.1.0
      sourceConfidence:
        - observed
  consumerContracts:
    reducer:
      phases:
        - documentation_materialization
  telemetry:
    metricsPrefix: eshu_dp_collector_document_import_
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write test manifest: %v", err)
	}
	return path
}

func seedTestBinary(t *testing.T, path string, body []byte) string {
	t.Helper()
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}
	return path
}

const seedTestRef = "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:9b766668cec1b7fe5901716e2fd4df14b98dbb2a049c646674370ff7f56bb495"

// TestSeedWritesCollectorAndManifest proves the seed path copies the caller
// binary and stamps the seed image digest into the destination manifest.
func TestSeedWritesCollectorAndManifest(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	binary := seedTestBinary(t, filepath.Join(work, "collector"), []byte("fake-collector-bytes"))
	manifest := seedTestManifest(t, filepath.Join(work, "manifest.yaml"))
	engine := fixtureDocEnginePayload(t)
	engine.platform = "linux/amd64"
	dest := filepath.Join(work, "dest")

	if err := seedComponent(dest, seedTestRef, binary, manifest, "linux/amd64", engine); err != nil {
		t.Fatalf("seedComponent: %v", err)
	}
	gotBinary, err := os.ReadFile(filepath.Join(dest, "collector"))
	if err != nil {
		t.Fatalf("read dest collector: %v", err)
	}
	if string(gotBinary) != "fake-collector-bytes" {
		t.Fatalf("dest collector body = %q, want the source bytes", gotBinary)
	}
	info, err := os.Stat(filepath.Join(dest, "collector"))
	if err != nil {
		t.Fatalf("stat dest collector: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("dest collector mode = %v, want an executable bit", info.Mode())
	}
	raw, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read dest manifest: %v", err)
	}
	var stamped component.Manifest
	if err := yaml.Unmarshal(raw, &stamped); err != nil {
		t.Fatalf("decode dest manifest: %v", err)
	}
	if len(stamped.Spec.Artifacts) != 2 {
		t.Fatalf("dest artifacts = %d, want 2 (amd64+arm64)", len(stamped.Spec.Artifacts))
	}
	for _, entry := range stamped.Spec.Artifacts {
		if entry.Platform == "linux/amd64" {
			if entry.Image != seedTestRef {
				t.Fatalf("dest amd64 image = %q, want %q", entry.Image, seedTestRef)
			}
			continue
		}
		if entry.Image != "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:1111111111111111111111111111111111111111111111111111111111111111" {
			t.Fatalf("dest sibling platform %q image moved = %q, want it byte-identical", entry.Platform, entry.Image)
		}
	}
	for _, entry := range stamped.Spec.Artifacts {
		if err := entry.Validate(); err != nil {
			t.Fatalf("dest artifact fails the digest gate: %v", err)
		}
	}
	if stamped.Metadata.ID != componentID {
		t.Fatalf("dest id = %q, want %q", stamped.Metadata.ID, componentID)
	}
}

// TestSeedRefusesNonDigestRef proves every non-digest image reference fails
// closed and leaves the destination untouched.
func TestSeedRefusesNonDigestRef(t *testing.T) {
	t.Parallel()

	refs := map[string]string{
		"tag only":      "ghcr.io/eshu-hq/eshu-collector-document-import:latest",
		"bare repo":     "ghcr.io/eshu-hq/eshu-collector-document-import",
		"short digest":  "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:abc123",
		"empty":         "",
		"wrong algo":    "ghcr.io/eshu-hq/eshu-collector-document-import@sha512:9b766668cec1b7fe5901716e2fd4df14b98dbb2a049c646674370ff7f56bb495",
		"trailing junk": seedTestRef + "-extra",
		"foreign repo":  "ghcr.io/eshu-hq/eshu-collector-media-import@sha256:9b766668cec1b7fe5901716e2fd4df14b98dbb2a049c646674370ff7f56bb495",
	}
	for name, ref := range refs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			work := t.TempDir()
			binary := seedTestBinary(t, filepath.Join(work, "collector"), []byte("fake-collector-bytes"))
			manifest := seedTestManifest(t, filepath.Join(work, "manifest.yaml"))
			engine := fixtureDocEnginePayload(t)
			engine.platform = "linux/amd64"
			dest := filepath.Join(work, "dest")

			err := seedComponent(dest, ref, binary, manifest, "linux/amd64", engine)
			if err == nil {
				t.Fatalf("seedComponent(%q): got nil error, want a digest refusal", ref)
			}
			if entries, _ := os.ReadDir(dest); len(entries) != 0 {
				t.Fatalf("seedComponent(%q) wrote %d dest entries, want a closed failure with none", ref, len(entries))
			}
		})
	}
}

// TestSeedRefusesMissingPlatform proves a manifest with no entry for the
// seed runner's platform fails closed with an empty destination, instead of
// stamping the digest into another arch's slot.
func TestSeedRefusesMissingPlatform(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	binary := seedTestBinary(t, filepath.Join(work, "collector"), []byte("fake-collector-bytes"))
	raw := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.collector.document_import
  name: Document import collector test
  publisher: eshu-hq
  version: 0.1.0
spec:
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/eshu-collector-document-import@sha256:0000000000000000000000000000000000000000000000000000000000000000
`
	manifest := filepath.Join(work, "manifest.yaml")
	if err := os.WriteFile(manifest, []byte(raw), 0o600); err != nil {
		t.Fatalf("write test manifest: %v", err)
	}
	engine := fixtureDocEnginePayload(t)
	engine.platform = "linux/arm64"
	dest := filepath.Join(work, "dest")

	err := seedComponent(dest, seedTestRef, binary, manifest, "linux/arm64", engine)
	if err == nil {
		t.Fatal("seedComponent for a missing platform: got nil error, want a failure")
	} else if !strings.Contains(err.Error(), "linux/arm64") {
		t.Fatalf("seedComponent error = %q, want it to name the missing platform", err)
	}
	if entries, _ := os.ReadDir(dest); len(entries) != 0 {
		t.Fatalf("seedComponent wrote %d dest entries, want a closed failure with none", len(entries))
	}
}

// TestSeedRefusesMissingInputs proves a missing caller binary or manifest
// fails closed instead of writing a partial destination.
func TestSeedRefusesMissingInputs(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	binary := seedTestBinary(t, filepath.Join(work, "collector"), []byte("fake-collector-bytes"))
	manifest := seedTestManifest(t, filepath.Join(work, "manifest.yaml"))
	engine := fixtureDocEnginePayload(t)
	engine.platform = "linux/amd64"

	if err := seedComponent(filepath.Join(work, "d1"), seedTestRef, filepath.Join(work, "missing"), manifest, "linux/amd64", engine); err == nil {
		t.Fatal("seedComponent with a missing binary: got nil error, want a failure")
	}
	if entries, _ := os.ReadDir(filepath.Join(work, "d1")); len(entries) != 0 {
		t.Fatalf("seedComponent with a missing binary wrote %d dest entries, want a closed failure with none", len(entries))
	}
	if err := seedComponent(filepath.Join(work, "d2"), seedTestRef, binary, filepath.Join(work, "missing.yaml"), "linux/amd64", engine); err == nil {
		t.Fatal("seedComponent with a missing manifest: got nil error, want a failure")
	}
}

// TestParseSeedArgs proves the seed flag contract: both flags required, no
// positionals, and a clear error naming the missing flag.
func TestParseSeedArgs(t *testing.T) {
	t.Parallel()

	dest, ref, err := parseSeedArgs([]string{"--dest", "/tmp/d", "--image-ref", seedTestRef})
	if err != nil {
		t.Fatalf("parseSeedArgs: %v", err)
	}
	if dest != "/tmp/d" || ref != seedTestRef {
		t.Fatalf("parseSeedArgs = (%q, %q), want (/tmp/d, ref)", dest, ref)
	}
	for name, args := range map[string][]string{
		"missing dest": {"--image-ref", seedTestRef},
		"missing ref":  {"--dest", "/tmp/d"},
		"no flags":     {},
		"positional":   {"--dest", "/tmp/d", "--image-ref", seedTestRef, "extra"},
		"unknown flag": {"--dest", "/tmp/d", "--image-ref", seedTestRef, "--bogus"},
		"blank dest":   {"--dest", "  ", "--image-ref", seedTestRef},
		"blank ref":    {"--dest", "/tmp/d", "--image-ref", "  "},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := parseSeedArgs(args); err == nil {
				t.Fatalf("parseSeedArgs(%q): got nil error, want a usage failure", args)
			} else if !strings.Contains(err.Error(), "seed") {
				t.Fatalf("parseSeedArgs(%q) error = %q, want it to name the seed usage", args, err)
			}
		})
	}
}

// seedBuildFrozenManifest writes the /manifest.yaml content a seed image
// built before a repin carries: both slots name the pre-repin images. The
// digests below are the ba038ecaf2 build-frozen values the :seed-ba038ecaf2
// images embedded before the #10329 repin moved the registry pins.
func seedBuildFrozenManifest(t *testing.T, path string) string {
	t.Helper()
	raw := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.collector.document_import
  name: Document import collector (OCR satellite caller)
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - documentation
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/eshu-collector-document-import@sha256:e8d2297bfeacea416d1bb0ec9ccb09d5a27060591042f5572225cf34502efead
    - platform: linux/arm64
      image: ghcr.io/eshu-hq/eshu-collector-document-import@sha256:c1bf5ce2fd46038d82db5823d76429137d258fe1d276dd98b94917b04d613db4
  emittedFacts:
    - kind: documentation_document
      payloadSchemaRef: documentation_document
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - observed
    - kind: documentation_section
      payloadSchemaRef: documentation_section
      schemaVersions:
        - 1.1.0
      sourceConfidence:
        - observed
  consumerContracts:
    reducer:
      phases:
        - documentation_materialization
  telemetry:
    metricsPrefix: eshu_dp_collector_document_import_
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write build-frozen manifest: %v", err)
	}
	return path
}

// TestSeedStampedManifestPassesHostAdmission replays the QA #9877 rollout
// at the seed/admission seam: the seed stamps the deploy-time --image-ref
// (the current registry pin, as the chart passes it) into the runner's own
// slot of a build-frozen pre-repin manifest, and host admission for that
// platform must accept the installed copy even though the sibling slot stays
// stale. The stale sibling is asserted stale, so the test proves the
// admission tolerates exactly the installed shape the seed protocol leaves.
func TestSeedStampedManifestPassesHostAdmission(t *testing.T) {
	t.Parallel()

	const platform = "linux/amd64"
	pins := scannercomponent.SatelliteSeedPins()[scannercomponent.SatelliteDocumentImportID]
	imageRef, ok := pins[platform]
	if !ok {
		t.Fatalf("registry pins declare no %q slot to stamp", platform)
	}
	const staleSibling = "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:c1bf5ce2fd46038d82db5823d76429137d258fe1d276dd98b94917b04d613db4"
	if staleSibling == imageRef {
		t.Fatal("stale sibling equals the current pin, want a pre-repin ref")
	}

	work := t.TempDir()
	binary := seedTestBinary(t, filepath.Join(work, "collector"), []byte("fake-collector-bytes"))
	manifest := seedBuildFrozenManifest(t, filepath.Join(work, "manifest.yaml"))
	engine := fixtureDocEnginePayload(t)
	engine.platform = platform
	dest := filepath.Join(work, "dest")

	if err := seedComponent(dest, imageRef, binary, manifest, platform, engine); err != nil {
		t.Fatalf("seedComponent: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dest, "manifest.yaml"))
	if err != nil {
		t.Fatalf("read stamped manifest: %v", err)
	}
	var stamped component.Manifest
	if err := yaml.Unmarshal(raw, &stamped); err != nil {
		t.Fatalf("decode stamped manifest: %v", err)
	}
	for _, entry := range stamped.Spec.Artifacts {
		if entry.Platform == platform {
			if entry.Image != imageRef {
				t.Fatalf("stamped %q image = %q, want the seed --image-ref", platform, entry.Image)
			}
			continue
		}
		if entry.Image != staleSibling {
			t.Fatalf("stamped sibling %q image moved = %q, want it build-frozen", entry.Platform, entry.Image)
		}
	}
	if err := scannercomponent.VerifySatelliteSeededArtifact(stamped, platform); err != nil {
		t.Fatalf("host admission error = %v, want nil for the honestly stamped seed", err)
	}
}
