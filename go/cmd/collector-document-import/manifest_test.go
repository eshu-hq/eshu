// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// loadRepoManifest reads the checked-in seed manifest under test.
func loadRepoManifest(t *testing.T) component.Manifest {
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

// repoManifestGrants authorizes the checked-in component identity for both
// documentation families, mirroring the activation grant flow.
func repoManifestGrants() []component.ProducerGrant {
	return []component.ProducerGrant{
		{
			ProducerID:     componentID,
			Version:        componentVersion,
			Kind:           "documentation_document",
			SchemaVersions: []string{"1.0.0"},
			Scope:          "documentation",
			ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
		},
		{
			ProducerID:     componentID,
			Version:        componentVersion,
			Kind:           "documentation_section",
			SchemaVersions: []string{"1.1.0"},
			Scope:          "documentation",
			ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
		},
	}
}

// provenSeedImages are the integration-proven seed images: the `docker
// build` of Dockerfile.seed records its content digests in
// image-provenance.md (both gates PASS per arch), and the manifest
// artifacts entries pin the same digests. A seed rebuild re-pins these
// constants in the same commit as the manifest fill.
const provenSeedImageAmd64 = "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:e8d2297bfeacea416d1bb0ec9ccb09d5a27060591042f5572225cf34502efead"

const provenSeedImageArm64 = "ghcr.io/eshu-hq/eshu-collector-document-import@sha256:c1bf5ce2fd46038d82db5823d76429137d258fe1d276dd98b94917b04d613db4"

// TestRepoManifestSeedContract proves the checked-in manifest carries the
// arbiter-ruled seed contract: pinned identity, the SDK process runtime,
// one digest-pinned artifact per platform, and exactly the two documentation
// families the caller drains.
func TestRepoManifestSeedContract(t *testing.T) {
	t.Parallel()

	manifest := loadRepoManifest(t)
	if manifest.APIVersion != "eshu.dev/v1alpha1" || manifest.Kind != "ComponentPackage" {
		t.Fatalf("manifest head = %q/%q, want eshu.dev/v1alpha1/ComponentPackage", manifest.APIVersion, manifest.Kind)
	}
	if manifest.Metadata.ID != componentID {
		t.Fatalf("manifest id = %q, want %q", manifest.Metadata.ID, componentID)
	}
	if manifest.Metadata.Version != componentVersion {
		t.Fatalf("manifest version = %q, want %q", manifest.Metadata.Version, componentVersion)
	}
	if manifest.Spec.Runtime.SDKProtocol != "collector-sdk/v1alpha1" || manifest.Spec.Runtime.Adapter != "process" {
		t.Fatalf("manifest runtime = %+v, want collector-sdk/v1alpha1/process", manifest.Spec.Runtime)
	}
	if len(manifest.Spec.CollectorKinds) != 1 || manifest.Spec.CollectorKinds[0] != "documentation" {
		t.Fatalf("manifest collectorKinds = %q, want [documentation]", manifest.Spec.CollectorKinds)
	}
	if len(manifest.Spec.Artifacts) != 2 {
		t.Fatalf("manifest artifacts = %d, want 2 (amd64+arm64)", len(manifest.Spec.Artifacts))
	}
	wantImages := map[string]string{
		"linux/amd64": provenSeedImageAmd64,
		"linux/arm64": provenSeedImageArm64,
	}
	for _, artifact := range manifest.Spec.Artifacts {
		if err := artifact.Validate(); err != nil {
			t.Fatalf("manifest artifact %q fails the digest gate: %v", artifact.Platform, err)
		}
		want, ok := wantImages[artifact.Platform]
		if !ok {
			t.Fatalf("manifest artifact platform %q is not a pinned seed platform", artifact.Platform)
		}
		if artifact.Image != want {
			t.Fatalf("manifest artifact %q image = %q, want the proven seed image %q", artifact.Platform, artifact.Image, want)
		}
	}
	wantFamilies := map[string]struct {
		version    string
		confidence string
	}{
		"documentation_document": {"1.0.0", "observed"},
		"documentation_section":  {"1.1.0", "observed"},
	}
	if len(manifest.Spec.EmittedFacts) != len(wantFamilies) {
		t.Fatalf("manifest emittedFacts = %d families, want %d", len(manifest.Spec.EmittedFacts), len(wantFamilies))
	}
	for _, family := range manifest.Spec.EmittedFacts {
		want, ok := wantFamilies[family.Kind]
		if !ok {
			t.Fatalf("manifest declares unexpected family %q", family.Kind)
		}
		if len(family.SchemaVersions) != 1 || family.SchemaVersions[0] != want.version {
			t.Fatalf("family %q versions = %q, want [%s]", family.Kind, family.SchemaVersions, want.version)
		}
		if len(family.SourceConfidence) != 1 || family.SourceConfidence[0] != want.confidence {
			t.Fatalf("family %q confidence = %q, want [%s]", family.Kind, family.SourceConfidence, want.confidence)
		}
		if family.PayloadSchemaRef != family.Kind {
			t.Fatalf("family %q payloadSchemaRef = %q, want the kind-keyed fixture-pack ref", family.Kind, family.PayloadSchemaRef)
		}
	}
	if phases := manifest.Spec.ConsumerContracts.Reducer.Phases; len(phases) != 1 || phases[0] != "documentation_materialization" {
		t.Fatalf("manifest reducer phases = %q, want [documentation_materialization]", phases)
	}
	if manifest.Spec.Telemetry.MetricsPrefix == "" {
		t.Fatal("manifest telemetry metricsPrefix is empty")
	}
}

// TestRepoManifestGrantGating proves the checked-in manifest admits the
// activation only with live producer grants for both core-owned families,
// and fails closed without them.
func TestRepoManifestGrantGating(t *testing.T) {
	t.Parallel()

	manifest := loadRepoManifest(t)
	if err := manifest.ValidateWithGrants(repoManifestGrants()); err != nil {
		t.Fatalf("manifest with live grants: %v", err)
	}
	if err := manifest.ValidateWithGrants(nil); err == nil {
		t.Fatal("manifest without grants: got nil error, want the core-owned denial")
	}
}
