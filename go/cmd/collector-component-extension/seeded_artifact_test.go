// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// driftedSeedImage returns a well-formed seed image for one scanner
// component that differs from the platform ManifestFor pin: exactly what a
// stale chart seed.image stamps, and what admission must deny (#8878).
func driftedSeedImage(t *testing.T, kind scannercomponent.AnalyzerKind) string {
	t.Helper()

	manifest, err := scannercomponent.ManifestFor(kind)
	if err != nil {
		t.Fatalf("ManifestFor(%q) error = %v", kind, err)
	}
	id := manifest.Metadata.ID
	ref := "ghcr.io/eshu-hq/scanner-" + strings.TrimPrefix(id, "eshu.scanner.") + "@sha256:" + strings.Repeat("c", 64)
	if ref == manifest.Spec.Artifacts[0].Image {
		t.Fatalf("drifted seed image equals the pin for %q, want a differing ref", kind)
	}
	return ref
}

// installScannerComponent installs one scanner manifest into a registry
// home with its RequiredGrants recorded and one claims-enabled activation,
// exactly as the chart reconcile init container leaves it. imageOverride
// replaces the artifact image before install (empty keeps the pin).
func installScannerComponent(t *testing.T, home string, kind scannercomponent.AnalyzerKind, imageOverride string) string {
	t.Helper()

	manifest, err := scannercomponent.ManifestFor(kind)
	if err != nil {
		t.Fatalf("ManifestFor(%q) error = %v", kind, err)
	}
	if imageOverride != "" {
		manifest.Spec.Artifacts[0].Image = imageOverride
	}
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal scanner manifest error = %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatalf("write scanner manifest: %v", err)
	}
	registry := component.NewRegistry(home)
	grants := scannercomponent.RequiredGrants(manifest)
	for _, grant := range grants {
		if err := registry.RecordGrant(grant); err != nil {
			t.Fatalf("RecordGrant(%q) error = %v", grant.Kind, err)
		}
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{manifest.Metadata.ID},
		AllowedPublishers: []string{manifest.Metadata.Publisher},
		CoreVersion:       "dev",
	}
	// The bare loader rejects core-owned kinds without grants, so verify
	// the in-memory manifest against the recorded grant set directly.
	verification := policy.VerifyWithGrants(context.Background(), manifest, grants)
	if !verification.Allowed {
		t.Fatalf("VerifyWithGrants() Allowed = false, want true: %s", verification.Reason)
	}
	if _, err := registry.Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.Metadata.ID, component.Activation{
		InstanceID:    "scanner-local",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeComponentExtensionConfig(t),
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	return manifest.Metadata.ID
}

func scannerWorkerEnv(home, componentID string) func(string) string {
	return mapEnv(map[string]string{
		envComponentHome:            home,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        componentID,
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "scanner-local",
	})
}

// TestLoadRuntimeConfigAdmitsPinnedScannerArtifact proves the worker loader
// admits a seeded scanner manifest whose artifact equals the platform pin:
// the claim path stays live when seed and platform agree (#8878).
func TestLoadRuntimeConfigAdmitsPinnedScannerArtifact(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	id := installScannerComponent(t, home, scannercomponent.AnalyzerImageUnpacking, "")

	config, err := loadRuntimeConfig(scannerWorkerEnv(home, id))
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil for the pinned artifact", err)
	}
	if len(config.Manifest.Spec.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want exactly 1", len(config.Manifest.Spec.Artifacts))
	}
	pinned, err := scannercomponent.ManifestFor(scannercomponent.AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	if config.Manifest.Spec.Artifacts[0].Image != pinned.Spec.Artifacts[0].Image {
		t.Fatal("admitted artifact differs from the platform pin, want equality")
	}
}

// TestLoadRuntimeConfigDeniesDriftedScannerArtifact proves the worker loader
// denies a seeded scanner manifest whose artifact differs from the platform
// pin before any claim: the stale seed image never reaches execution, and
// the error names the defect without echoing the image (#8878).
func TestLoadRuntimeConfigDeniesDriftedScannerArtifact(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	drifted := driftedSeedImage(t, scannercomponent.AnalyzerImageUnpacking)
	id := installScannerComponent(t, home, scannercomponent.AnalyzerImageUnpacking, drifted)

	_, err := loadRuntimeConfig(scannerWorkerEnv(home, id))
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want denial for a drifted scanner artifact")
	}
	if !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("loadRuntimeConfig() error = %q, want it to name the pinned platform image", err)
	}
	if strings.Contains(err.Error(), drifted) {
		t.Fatalf("loadRuntimeConfig() error = %q, must not echo the image value", err)
	}
}

// satelliteSeedManifest builds a seeded satellite manifest for one satellite
// ID: the registry pins as artifacts with the identity, runtime, emitted
// families, and reducer contract the Event-B caller manifests carry.
// imageOverride replaces every artifact image before install (empty keeps
// the registry pins), so a drifted install denies on any host platform.
func satelliteSeedManifest(t *testing.T, id string, imageOverride string) component.Manifest {
	t.Helper()

	pins := scannercomponent.SatelliteSeedPins()[id]
	if len(pins) == 0 {
		t.Fatalf("SatelliteSeedPins()[%q] is empty, want one pin per platform", id)
	}
	manifest := component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
	}
	manifest.Metadata.ID = id
	manifest.Metadata.Name = "Seeded satellite under test"
	manifest.Metadata.Publisher = "eshu-hq"
	manifest.Metadata.Version = "0.1.0"
	manifest.Spec.CompatibleCore = ">=0.0.5 <0.2.0"
	manifest.Spec.ComponentType = component.ComponentTypeCollector
	manifest.Spec.CollectorKinds = []string{"documentation"}
	manifest.Spec.Runtime.SDKProtocol = "collector-sdk/v1alpha1"
	manifest.Spec.Runtime.Adapter = component.RuntimeAdapterProcess
	for platform, image := range pins {
		manifest.Spec.Artifacts = append(manifest.Spec.Artifacts, component.Artifact{
			Platform: platform,
			Image:    image,
		})
	}
	manifest.Spec.EmittedFacts = []component.FactFamily{
		{Kind: "documentation_document", SchemaVersions: []string{"1.0.0"}, SourceConfidence: []string{"observed"}},
		{Kind: "documentation_section", SchemaVersions: []string{"1.1.0"}, SourceConfidence: []string{"observed"}},
	}
	manifest.Spec.ConsumerContracts.Reducer.Phases = []string{"documentation_materialization"}
	manifest.Spec.Telemetry.MetricsPrefix = "eshu_dp_seeded_satellite_test_"
	if imageOverride != "" {
		for i := range manifest.Spec.Artifacts {
			manifest.Spec.Artifacts[i].Image = imageOverride
		}
	}
	return manifest
}

// satelliteSeedGrants authorizes the satellite manifest's documentation
// families for the install, mirroring the activation grant flow.
func satelliteSeedGrants(manifest component.Manifest) []component.ProducerGrant {
	grants := make([]component.ProducerGrant, 0, len(manifest.Spec.EmittedFacts))
	for _, family := range manifest.Spec.EmittedFacts {
		grants = append(grants, component.ProducerGrant{
			ProducerID:     manifest.Metadata.ID,
			Version:        manifest.Metadata.Version,
			Kind:           family.Kind,
			SchemaVersions: append([]string(nil), family.SchemaVersions...),
			Scope:          "documentation",
			ExpiresAt:      time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC),
		})
	}
	return grants
}

// installSatelliteComponent installs one satellite manifest into a registry
// home with its grants recorded and one claims-enabled activation, exactly
// as the chart reconcile init container leaves it.
func installSatelliteComponent(t *testing.T, home, id, imageOverride string) string {
	t.Helper()

	manifest := satelliteSeedManifest(t, id, imageOverride)
	raw, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal satellite manifest error = %v", err)
	}
	manifestPath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatalf("write satellite manifest: %v", err)
	}
	registry := component.NewRegistry(home)
	grants := satelliteSeedGrants(manifest)
	for _, grant := range grants {
		if err := registry.RecordGrant(grant); err != nil {
			t.Fatalf("RecordGrant(%q) error = %v", grant.Kind, err)
		}
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{manifest.Metadata.ID},
		AllowedPublishers: []string{manifest.Metadata.Publisher},
		CoreVersion:       "dev",
	}
	// The bare loader rejects core-owned kinds without grants, so verify
	// the in-memory manifest against the recorded grant set directly.
	verification := policy.VerifyWithGrants(context.Background(), manifest, grants)
	if !verification.Allowed {
		t.Fatalf("VerifyWithGrants() Allowed = false, want true: %s", verification.Reason)
	}
	if _, err := registry.Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.Metadata.ID, component.Activation{
		InstanceID:    "satellite-local",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeComponentExtensionConfig(t),
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	return manifest.Metadata.ID
}

// satelliteWorkerEnv mirrors scannerWorkerEnv for satellite activations,
// which claim under their own instance ID.
func satelliteWorkerEnv(home, componentID string) func(string) string {
	return mapEnv(map[string]string{
		envComponentHome:            home,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        componentID,
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "satellite-local",
	})
}

// driftedSatelliteSeedImage returns a well-formed seed image for one
// satellite that differs from every registry pin: exactly what a drifted
// live-chart seed.image stamps, and what admission must deny (#10285).
func driftedSatelliteSeedImage(t *testing.T, id string) string {
	t.Helper()

	repo := "ghcr.io/eshu-hq/eshu-collector-document-import"
	if id == scannercomponent.SatelliteMediaImportID {
		repo = "ghcr.io/eshu-hq/eshu-collector-media-import"
	}
	ref := repo + "@sha256:" + strings.Repeat("e", 64)
	for _, pin := range scannercomponent.SatelliteSeedPins()[id] {
		if ref == pin {
			t.Fatalf("drifted seed image equals a registry pin for %q, want a differing ref", id)
		}
	}
	return ref
}

// TestLoadRuntimeConfigAdmitsPinnedSatelliteArtifact proves the worker loader
// admits a seeded satellite manifest whose artifacts equal the registry
// pins: the claim path stays live when seed and platform agree (#10285).
func TestLoadRuntimeConfigAdmitsPinnedSatelliteArtifact(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	id := installSatelliteComponent(t, home, scannercomponent.SatelliteDocumentImportID, "")

	config, err := loadRuntimeConfig(satelliteWorkerEnv(home, id))
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil for the pinned artifacts", err)
	}
	pins := scannercomponent.SatelliteSeedPins()[id]
	if len(config.Manifest.Spec.Artifacts) != len(pins) {
		t.Fatalf("artifacts = %d, want exactly %d pinned seed images", len(config.Manifest.Spec.Artifacts), len(pins))
	}
	for _, artifact := range config.Manifest.Spec.Artifacts {
		if want := pins[artifact.Platform]; artifact.Image != want {
			t.Fatal("admitted artifact differs from the registry pin, want equality")
		}
	}
}

// TestLoadRuntimeConfigDeniesDriftedSatelliteArtifact proves the worker
// loader denies a seeded satellite manifest whose artifact differs from the
// registry pins before any claim: the stale seed image never reaches
// execution, and the error names the defect without echoing the image
// (#10285).
func TestLoadRuntimeConfigDeniesDriftedSatelliteArtifact(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	id := scannercomponent.SatelliteMediaImportID
	drifted := driftedSatelliteSeedImage(t, id)
	installSatelliteComponent(t, home, id, drifted)

	_, err := loadRuntimeConfig(satelliteWorkerEnv(home, id))
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want denial for a drifted satellite artifact")
	}
	if !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("loadRuntimeConfig() error = %q, want it to name the pinned platform images", err)
	}
	if strings.Contains(err.Error(), drifted) {
		t.Fatalf("loadRuntimeConfig() error = %q, must not echo the image value", err)
	}
}
