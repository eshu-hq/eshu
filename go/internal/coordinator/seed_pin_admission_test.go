// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

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
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// installScannerActivation installs one scanner manifest with its
// RequiredGrants recorded and one claims-enabled activation, mirroring the
// chart reconcile state. imageOverride replaces the artifact image before
// install (empty keeps the platform pin).
func installScannerActivation(t *testing.T, kind scannerworker.AnalyzerKind, imageOverride string) string {
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
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, raw, 0o600); err != nil {
		t.Fatalf("write scanner manifest: %v", err)
	}
	home := filepath.Join(dir, "components")
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
		CoreVersion:       "v0.1.0",
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
	configPath := filepath.Join(dir, "scanner.yaml")
	if err := os.WriteFile(configPath, []byte("process:\n  command: /usr/local/bin/scanner-collector\n"), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}
	if _, err := registry.Enable(manifest.Metadata.ID, component.Activation{
		InstanceID:    "scanner-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	return home
}

func scannerCoordinatorEnv(home, componentID string) func(string) string {
	return componentCoordinatorEnv(home, map[string]string{
		"ESHU_COMPONENT_ALLOW_IDS": componentID,
	})
}

// TestLoadConfigPlansPinnedScannerActivation proves the coordinator planner
// admits a seeded scanner manifest whose artifact equals the platform pin
// (#8878).
func TestLoadConfigPlansPinnedScannerActivation(t *testing.T) {
	t.Parallel()

	home := installScannerActivation(t, scannerworker.AnalyzerImageUnpacking, "")

	cfg, err := LoadConfig(scannerCoordinatorEnv(home, scannercomponent.IDImage))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil for the pinned artifact", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want %d", got, want)
	}
	instance := cfg.CollectorInstances[0]
	if got, want := instance.InstanceID, "scanner-primary"; got != want {
		t.Fatalf("instance id = %q, want %q", got, want)
	}
	if got, want := instance.CollectorKind, scope.CollectorKind("scanner_worker"); got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
}

// TestLoadConfigDeniesDriftedScannerActivation proves the coordinator
// planner denies a seeded scanner manifest whose artifact differs from the
// platform pin before any instance is planned, without echoing the image
// (#8878).
func TestLoadConfigDeniesDriftedScannerActivation(t *testing.T) {
	t.Parallel()

	pinned, err := scannercomponent.ManifestFor(scannerworker.AnalyzerImageUnpacking)
	if err != nil {
		t.Fatalf("ManifestFor() error = %v", err)
	}
	drifted := "ghcr.io/eshu-hq/scanner-image@sha256:" + strings.Repeat("c", 64)
	if drifted == pinned.Spec.Artifacts[0].Image {
		t.Fatal("drifted seed image equals the pin, want a differing ref")
	}
	home := installScannerActivation(t, scannerworker.AnalyzerImageUnpacking, drifted)

	_, err = LoadConfig(scannerCoordinatorEnv(home, scannercomponent.IDImage))
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want denial for a drifted scanner artifact")
	}
	if !strings.Contains(err.Error(), "pinned") {
		t.Fatalf("LoadConfig() error = %q, want it to name the pinned platform image", err)
	}
	if strings.Contains(err.Error(), drifted) {
		t.Fatalf("LoadConfig() error = %q, must not echo the image value", err)
	}
}
