// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestRegistryReadbackClassifiesLifecycleAndPolicyStatus(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{
		InstanceID:    "prod-aws",
		ClaimsEnabled: true,
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}

	readback, err := registry.Readback(Policy{})
	if err != nil {
		t.Fatalf("Readback() error = %v, want nil", err)
	}
	assertReadbackStates(t, readback, "installed", "enabled", "claim_capable")

	revoked, err := registry.Readback(Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		RevokedIDs:        []string{"dev.eshu.collector.aws"},
		CoreVersion:       "v0.0.5",
	})
	if err != nil {
		t.Fatalf("revoked Readback() error = %v, want nil", err)
	}
	assertReadbackStates(t, revoked, "revoked", "failed")

	incompatible, err := registry.Readback(Policy{
		Mode:              TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.collector.aws"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v9.0.0",
	})
	if err != nil {
		t.Fatalf("incompatible Readback() error = %v, want nil", err)
	}
	assertReadbackStates(t, incompatible, "incompatible", "failed")

	if err := os.Remove(readback[0].ManifestPath); err != nil {
		t.Fatalf("os.Remove() error = %v, want nil", err)
	}
	failed, err := registry.Readback(Policy{})
	if err != nil {
		t.Fatalf("failed Readback() error = %v, want nil", err)
	}
	assertReadbackStates(t, failed, "failed")
	if got, want := failed[0].Error.Code, ErrorCodeInvalidManifest; got != want {
		t.Fatalf("failed Error.Code = %q, want %q", got, want)
	}
}

func TestRegistryReadbackRequiresExplicitClaimActivation(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	manifestPath := writeManifest(t, validManifestYAML())
	if _, err := registry.Install(manifestPath, allowedVerification()); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}

	installedReadback, err := registry.Readback(Policy{})
	if err != nil {
		t.Fatalf("installed Readback() error = %v, want nil", err)
	}
	assertReadbackStates(t, installedReadback, "installed")
	assertReadbackMissingStates(t, installedReadback, "enabled", "claim_capable")

	if _, err := registry.Enable("dev.eshu.collector.aws", Activation{
		InstanceID:    "prod-aws",
		ClaimsEnabled: false,
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}

	enabledReadback, err := registry.Readback(Policy{})
	if err != nil {
		t.Fatalf("enabled Readback() error = %v, want nil", err)
	}
	assertReadbackStates(t, enabledReadback, "installed", "enabled")
	assertReadbackMissingStates(t, enabledReadback, "claim_capable")
}

func TestRegistryReadbackIgnoresManifestPathFromRegistryState(t *testing.T) {
	t.Parallel()

	registryHome := t.TempDir()
	registry := NewRegistry(registryHome)
	canonicalManifestPath := filepath.Join(registryHome, "packages", "dev.eshu.collector.aws", "0.1.0", "manifest.yaml")
	if err := os.MkdirAll(filepath.Dir(canonicalManifestPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(canonicalManifestPath, []byte(validManifestYAML()), 0o600); err != nil {
		t.Fatalf("os.WriteFile(canonical manifest) error = %v, want nil", err)
	}
	state := registryState{
		Components: []InstalledComponent{
			{
				ID:           "dev.eshu.collector.aws",
				Name:         "AWS cloud scanner",
				Publisher:    "eshu-hq",
				Version:      "0.1.0",
				ManifestPath: filepath.Join(t.TempDir(), "outside.yaml"),
				Verified:     true,
				TrustMode:    TrustModeAllowlist,
			},
		},
	}
	if err := registry.save(state); err != nil {
		t.Fatalf("save() error = %v, want nil", err)
	}

	readback, err := registry.Readback(Policy{})
	if err != nil {
		t.Fatalf("Readback() error = %v, want nil", err)
	}
	assertReadbackStates(t, readback, "installed")
	if readback[0].Error != nil {
		t.Fatalf("Readback().Error = %#v, want nil", readback[0].Error)
	}
	if got, want := readback[0].ManifestPath, canonicalManifestPath; got != want {
		t.Fatalf("Readback().ManifestPath = %q, want canonical %q", got, want)
	}
}

func assertReadbackStates(t *testing.T, readback []RegistryReadbackComponent, wantStates ...string) {
	t.Helper()

	if len(readback) != 1 {
		t.Fatalf("len(readback) = %d, want 1", len(readback))
	}
	for _, want := range wantStates {
		if !slices.Contains(readback[0].States, want) {
			t.Fatalf("states = %v, want %q", readback[0].States, want)
		}
	}
}

func assertReadbackMissingStates(t *testing.T, readback []RegistryReadbackComponent, missingStates ...string) {
	t.Helper()

	if len(readback) != 1 {
		t.Fatalf("len(readback) = %d, want 1", len(readback))
	}
	for _, missing := range missingStates {
		if slices.Contains(readback[0].States, missing) {
			t.Fatalf("states = %v, want missing %q", readback[0].States, missing)
		}
	}
}
