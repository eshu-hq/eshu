// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// seedTestManifest writes a minimal document-import ComponentPackage with a
// placeholder seed image and returns its path. Tests stamp it through the
// seed path instead of touching the repository manifest.
func seedTestManifest(t *testing.T, dir string) string {
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
      image: ghcr.io/eshu-hq/eshu-document-import@sha256:0000000000000000000000000000000000000000000000000000000000000000
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
	path := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write test manifest: %v", err)
	}
	return path
}

func seedTestBinary(t *testing.T, dir string, body []byte) string {
	t.Helper()
	path := filepath.Join(dir, "collector")
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}
	return path
}

const seedTestRef = "ghcr.io/eshu-hq/eshu-document-import@sha256:9b766668cec1b7fe5901716e2fd4df14b98dbb2a049c646674370ff7f56bb495"

// TestSeedWritesCollectorAndManifest proves the seed path copies the caller
// binary and stamps the seed image digest into the destination manifest.
func TestSeedWritesCollectorAndManifest(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	binary := seedTestBinary(t, work, []byte("fake-collector-bytes"))
	manifest := seedTestManifest(t, work)
	dest := filepath.Join(work, "dest")

	if err := seedComponent(dest, seedTestRef, binary, manifest); err != nil {
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
	if len(stamped.Spec.Artifacts) != 1 {
		t.Fatalf("dest artifacts = %d, want 1", len(stamped.Spec.Artifacts))
	}
	if stamped.Spec.Artifacts[0].Image != seedTestRef {
		t.Fatalf("dest image = %q, want %q", stamped.Spec.Artifacts[0].Image, seedTestRef)
	}
	if err := stamped.Spec.Artifacts[0].Validate(); err != nil {
		t.Fatalf("dest artifact fails the digest gate: %v", err)
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
		"tag only":      "ghcr.io/eshu-hq/eshu-document-import:latest",
		"bare repo":     "ghcr.io/eshu-hq/eshu-document-import",
		"short digest":  "ghcr.io/eshu-hq/eshu-document-import@sha256:abc123",
		"empty":         "",
		"wrong algo":    "ghcr.io/eshu-hq/eshu-document-import@sha512:9b766668cec1b7fe5901716e2fd4df14b98dbb2a049c646674370ff7f56bb495",
		"trailing junk": seedTestRef + "-extra",
	}
	for name, ref := range refs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			work := t.TempDir()
			binary := seedTestBinary(t, work, []byte("fake-collector-bytes"))
			manifest := seedTestManifest(t, work)
			dest := filepath.Join(work, "dest")

			err := seedComponent(dest, ref, binary, manifest)
			if err == nil {
				t.Fatalf("seedComponent(%q): got nil error, want a digest refusal", ref)
			}
			if entries, _ := os.ReadDir(dest); len(entries) != 0 {
				t.Fatalf("seedComponent(%q) wrote %d dest entries, want a closed failure with none", ref, len(entries))
			}
		})
	}
}

// TestSeedRefusesMissingInputs proves a missing caller binary or manifest
// fails closed instead of writing a partial destination.
func TestSeedRefusesMissingInputs(t *testing.T) {
	t.Parallel()

	work := t.TempDir()
	binary := seedTestBinary(t, work, []byte("fake-collector-bytes"))
	manifest := seedTestManifest(t, work)

	if err := seedComponent(filepath.Join(work, "d1"), seedTestRef, filepath.Join(work, "missing"), manifest); err == nil {
		t.Fatal("seedComponent with a missing binary: got nil error, want a failure")
	}
	if err := seedComponent(filepath.Join(work, "d2"), seedTestRef, binary, filepath.Join(work, "missing.yaml")); err == nil {
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
