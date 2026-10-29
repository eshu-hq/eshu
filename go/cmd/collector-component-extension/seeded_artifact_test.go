// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannerworker/component"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// driftedSeedImage returns a well-formed seed image for one scanner
// component that differs from the platform ManifestFor pin: exactly what a
// stale chart seed.image stamps, and what admission must deny (#8878).
func driftedSeedImage(t *testing.T, kind scannerworker.AnalyzerKind) string {
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
func installScannerComponent(t *testing.T, home string, kind scannerworker.AnalyzerKind, imageOverride string) string {
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
	id := installScannerComponent(t, home, scannerworker.AnalyzerImageUnpacking, "")

	config, err := loadRuntimeConfig(scannerWorkerEnv(home, id))
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil for the pinned artifact", err)
	}
	if len(config.Manifest.Spec.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want exactly 1", len(config.Manifest.Spec.Artifacts))
	}
	pinned, err := scannercomponent.ManifestFor(scannerworker.AnalyzerImageUnpacking)
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
	drifted := driftedSeedImage(t, scannerworker.AnalyzerImageUnpacking)
	id := installScannerComponent(t, home, scannerworker.AnalyzerImageUnpacking, drifted)

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
