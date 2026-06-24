// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifestValidateRejectsCoreFactKindClaims(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.EmittedFacts[0].Kind = "aws_resource"

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want core fact-kind collision")
	}
	if !strings.Contains(err.Error(), "aws_resource") || !strings.Contains(err.Error(), "core-owned") {
		t.Fatalf("Validate() error = %v, want actionable core-owned fact-kind error", err)
	}
}

func TestManifestValidateRejectsUnnamespacedFactKindClaims(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.EmittedFacts[0].Kind = "demo_observation"

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want namespace validation error")
	}
	if !strings.Contains(err.Error(), "demo_observation") || !strings.Contains(err.Error(), "namespaced") {
		t.Fatalf("Validate() error = %v, want actionable namespace error", err)
	}
}

func TestManifestValidateRejectsNonCanonicalFactKindClaims(t *testing.T) {
	t.Parallel()

	manifest := validManifest()
	manifest.Spec.EmittedFacts[0].Kind = " dev.eshu.example.finding "

	err := manifest.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want canonical fact-kind validation error")
	}
	if !strings.Contains(err.Error(), "canonical") {
		t.Fatalf("Validate() error = %v, want canonical fact-kind error", err)
	}
}

func TestRegistryInstallRejectsComponentFactKindCollision(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	first := componentManifestForFactKind("dev.example.collector.alpha", "0.1.0", "dev.example.shared.finding", []string{"1.0.0"})
	second := componentManifestForFactKind("dev.example.collector.beta", "0.1.0", "dev.example.shared.finding", []string{"1.0.0"})
	if _, err := registry.Install(writeManifest(t, first), verificationFor("dev.example.collector.alpha", "0.1.0")); err != nil {
		t.Fatalf("Install(first) error = %v, want nil", err)
	}

	_, err := registry.Install(writeManifest(t, second), verificationFor("dev.example.collector.beta", "0.1.0"))
	if err == nil {
		t.Fatal("Install(second) error = nil, want fact-kind collision")
	}
	if got, want := ErrorCodeOf(err), ErrorCode("fact_kind_collision"); got != want {
		t.Fatalf("Install(second) code = %q, want %q; err=%v", got, want, err)
	}
	for _, want := range []string{"dev.example.shared.finding", "dev.example.collector.beta", "dev.example.collector.alpha"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Install(second) error = %v, want message to contain %q", err, want)
		}
	}
}

func TestRegistryInstallAllowsCompatibleVersionForSameComponentFactKind(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	v010 := componentManifestForFactKind("dev.example.collector.alpha", "0.1.0", "dev.example.shared.finding", []string{"1.0.0"})
	v020 := componentManifestForFactKind("dev.example.collector.alpha", "0.2.0", "dev.example.shared.finding", []string{"1.1.0"})
	if _, err := registry.Install(writeManifest(t, v010), verificationFor("dev.example.collector.alpha", "0.1.0")); err != nil {
		t.Fatalf("Install(0.1.0) error = %v, want nil", err)
	}
	if _, err := registry.Install(writeManifest(t, v020), verificationFor("dev.example.collector.alpha", "0.2.0")); err != nil {
		t.Fatalf("Install(0.2.0) error = %v, want nil", err)
	}
}

func TestRegistryInstallAllowsFactKindAfterUninstall(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	first := componentManifestForFactKind("dev.example.collector.alpha", "0.1.0", "dev.example.shared.finding", []string{"1.0.0"})
	second := componentManifestForFactKind("dev.example.collector.beta", "0.1.0", "dev.example.shared.finding", []string{"1.0.0"})
	if _, err := registry.Install(writeManifest(t, first), verificationFor("dev.example.collector.alpha", "0.1.0")); err != nil {
		t.Fatalf("Install(first) error = %v, want nil", err)
	}
	if err := registry.Uninstall("dev.example.collector.alpha", "0.1.0"); err != nil {
		t.Fatalf("Uninstall(first) error = %v, want nil", err)
	}
	if _, err := registry.Install(writeManifest(t, second), verificationFor("dev.example.collector.beta", "0.1.0")); err != nil {
		t.Fatalf("Install(second) error = %v, want nil after uninstall; err=%v", err, err)
	}
}

func TestRegistryPlanEnableRejectsStoredComponentFactKindCollision(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	registry := NewRegistry(home)
	first := installedComponentForFactKind(t, registry, "dev.example.collector.alpha", "0.1.0", "dev.example.shared.finding", []string{"1.0.0"})
	second := installedComponentForFactKind(t, registry, "dev.example.collector.beta", "0.1.0", "dev.example.shared.finding", []string{"1.0.0"})
	if err := registry.save(registryState{Components: []InstalledComponent{first, second}}); err != nil {
		t.Fatalf("save() error = %v, want nil", err)
	}

	_, err := registry.PlanEnable("dev.example.collector.beta", Activation{InstanceID: "beta"})
	if err == nil {
		t.Fatal("PlanEnable() error = nil, want fact-kind collision")
	}
	if got, want := ErrorCodeOf(err), ErrorCode("fact_kind_collision"); got != want {
		t.Fatalf("PlanEnable() code = %q, want %q; err=%v", got, want, err)
	}
}

func componentManifestForFactKind(componentID, version, factKind string, schemaVersions []string) string {
	var schemaBuilder strings.Builder
	for _, version := range schemaVersions {
		schemaBuilder.WriteString("        - ")
		schemaBuilder.WriteString(version)
		schemaBuilder.WriteString("\n")
	}
	return strings.ReplaceAll(
		strings.ReplaceAll(
			strings.ReplaceAll(
				strings.ReplaceAll(
					validManifestYAML(),
					"dev.eshu.collector.aws",
					componentID,
				),
				"version: 0.1.0",
				"version: "+version,
			),
			"kind: dev.eshu.aws.cloud_resource",
			"kind: "+factKind,
		),
		"        - 1.0.0\n",
		schemaBuilder.String(),
	)
}

func verificationFor(componentID, version string) VerificationResult {
	return VerificationResult{
		Allowed:   true,
		Mode:      TrustModeAllowlist,
		Component: componentID,
		Publisher: "eshu-hq",
		Version:   version,
	}
}

func installedComponentForFactKind(
	t *testing.T,
	registry Registry,
	componentID string,
	version string,
	factKind string,
	schemaVersions []string,
) InstalledComponent {
	t.Helper()

	manifestPath := registry.manifestPath(componentID, version)
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v, want nil", err)
	}
	if err := os.WriteFile(manifestPath, []byte(componentManifestForFactKind(componentID, version, factKind, schemaVersions)), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return InstalledComponent{
		ID:             componentID,
		Name:           componentID,
		Publisher:      "eshu-hq",
		Version:        version,
		ManifestPath:   manifestPath,
		ManifestDigest: "sha256:test",
		Verified:       true,
		TrustMode:      TrustModeAllowlist,
		InstalledAt:    time.Now().UTC(),
	}
}
