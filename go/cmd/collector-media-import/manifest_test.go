// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"regexp"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// provenSeedImages are the dispatch-proven seed images: the integration
// `docker build` of Dockerfile.seed records its content digests here (both
// gates PASS per arch), and the manifest artifacts entries pin the same
// digests. A seed rebuild re-pins these constants in the same commit as the
// manifest fill.
const provenSeedImageAmd64 = "ghcr.io/eshu-hq/eshu-collector-media-import@sha256:68636a8e8bf2c4f3ca2b037505e8d2ee5d28cc0050eae0f29d1554dbd128111a"

const provenSeedImageArm64 = "ghcr.io/eshu-hq/eshu-collector-media-import@sha256:825c7b3acef7617f14787c6fd007296902f5716f299012e9b1b33530c2c5b688"

var seedDigestPattern = regexp.MustCompile(`^.+@sha256:[A-Fa-f0-9]{64}$`)

// loadSeedManifest decodes the authored component manifest without
// producer-grant context: documentation_document and documentation_section
// are core-owned, so grant admission happens in the host registry flow, not
// in this structural contract test.
func loadSeedManifest(t *testing.T) component.Manifest {
	t.Helper()
	raw, err := os.ReadFile("manifest.yaml")
	if err != nil {
		t.Fatalf("read manifest.yaml: %v", err)
	}
	var manifest component.Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("decode manifest.yaml: %v", err)
	}
	return manifest
}

// TestSeedManifestIdentity proves the authored manifest carries the
// arbiter-ruled seed identity: the media-import component id at version
// 0.1.0 serving the documentation collector kind.
func TestSeedManifestIdentity(t *testing.T) {
	t.Parallel()

	manifest := loadSeedManifest(t)
	if manifest.APIVersion != "eshu.dev/v1alpha1" {
		t.Fatalf("apiVersion = %q, want eshu.dev/v1alpha1", manifest.APIVersion)
	}
	if manifest.Kind != "ComponentPackage" {
		t.Fatalf("kind = %q, want ComponentPackage", manifest.Kind)
	}
	if manifest.Metadata.ID != "dev.eshu.collector.media_import" {
		t.Fatalf("metadata.id = %q, want dev.eshu.collector.media_import", manifest.Metadata.ID)
	}
	if manifest.Metadata.Version != "0.1.0" {
		t.Fatalf("metadata.version = %q, want 0.1.0", manifest.Metadata.Version)
	}
	if len(manifest.Spec.CollectorKinds) != 1 || manifest.Spec.CollectorKinds[0] != "documentation" {
		t.Fatalf("collectorKinds = %q, want exactly [documentation]", manifest.Spec.CollectorKinds)
	}
}

// TestSeedManifestRuntime proves the manifest declares the satellite wire
// contract the stdio adapter speaks: collector-sdk/v1alpha1 over a process
// adapter.
func TestSeedManifestRuntime(t *testing.T) {
	t.Parallel()

	manifest := loadSeedManifest(t)
	if manifest.Spec.Runtime.SDKProtocol != "collector-sdk/v1alpha1" {
		t.Fatalf("runtime.sdkProtocol = %q, want collector-sdk/v1alpha1", manifest.Spec.Runtime.SDKProtocol)
	}
	if manifest.Spec.Runtime.Adapter != "process" {
		t.Fatalf("runtime.adapter = %q, want process", manifest.Spec.Runtime.Adapter)
	}
}

// TestSeedManifestArtifactPinsDispatchProvenImage proves each artifact
// entry names exactly the dispatch-proven digest-pinned seed image for its
// platform: the host launches these bytes and nothing else.
func TestSeedManifestArtifactPinsDispatchProvenImage(t *testing.T) {
	t.Parallel()

	manifest := loadSeedManifest(t)
	want := map[string]string{
		"linux/amd64": provenSeedImageAmd64,
		"linux/arm64": provenSeedImageArm64,
	}
	if len(manifest.Spec.Artifacts) != len(want) {
		t.Fatalf("artifacts = %d, want exactly %d pinned seed images", len(manifest.Spec.Artifacts), len(want))
	}
	for _, artifact := range manifest.Spec.Artifacts {
		expected, ok := want[artifact.Platform]
		if !ok {
			t.Fatalf("artifact platform %q is not a pinned seed platform", artifact.Platform)
		}
		if !seedDigestPattern.MatchString(artifact.Image) {
			t.Fatalf("artifact image %q is not digest-pinned (want repo@sha256:<64hex>)", artifact.Image)
		}
		if artifact.Image != expected {
			t.Fatalf("artifact image %q != dispatch-proven seed image %q", artifact.Image, expected)
		}
	}
}

// TestSeedManifestEmittedFacts proves the manifest declares exactly the
// transcript families the drain emits, at the schema versions the registry
// governs: documentation_document 1.0.0 plus documentation_section 1.1.0.
func TestSeedManifestEmittedFacts(t *testing.T) {
	t.Parallel()

	manifest := loadSeedManifest(t)
	type family struct {
		kind     string
		versions []string
	}
	want := []family{
		{kind: "documentation_document", versions: []string{"1.0.0"}},
		{kind: "documentation_section", versions: []string{"1.1.0"}},
	}
	if len(manifest.Spec.EmittedFacts) != len(want) {
		t.Fatalf("emittedFacts = %+v, want exactly %d families", manifest.Spec.EmittedFacts, len(want))
	}
	for i, w := range want {
		got := manifest.Spec.EmittedFacts[i]
		if got.Kind != w.kind {
			t.Fatalf("emittedFacts[%d].kind = %q, want %q", i, got.Kind, w.kind)
		}
		if len(got.SchemaVersions) != len(w.versions) {
			t.Fatalf("emittedFacts[%d].schemaVersions = %q, want %q", i, got.SchemaVersions, w.versions)
		}
		for j, version := range w.versions {
			if got.SchemaVersions[j] != version {
				t.Fatalf("emittedFacts[%d].schemaVersions = %q, want %q", i, got.SchemaVersions, w.versions)
			}
		}
		if len(got.SourceConfidence) == 0 {
			t.Fatalf("emittedFacts[%d] declares no sourceConfidence", i)
		}
		for _, confidence := range got.SourceConfidence {
			if confidence == facts.SourceConfidenceUnknown {
				t.Fatalf("emittedFacts[%d] declares unknown confidence for new component output", i)
			}
		}
	}
	// The reducer contract is registry-derived and kind-global: both
	// documentation kinds carry ReducerDomain documentation_materialization,
	// so the phases name it. source_evidence_only would be a lie here.
	if phases := manifest.Spec.ConsumerContracts.Reducer.Phases; len(phases) != 1 || phases[0] != "documentation_materialization" {
		t.Fatalf("manifest reducer phases = %q, want [documentation_materialization]", phases)
	}
}
