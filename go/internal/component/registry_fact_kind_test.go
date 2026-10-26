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

// Shared core-owned fact-kind tests (issues #9202 and #9172): two distinct
// granted components may declare the same core-owned fact kind when their
// schema-major sets agree and each side holds a live grant covering its
// declared schema versions scoped to its own collector kinds.
const sharedGrantedCoreKind = "observability.source_instance"

// componentManifestForGrantedFactKind renders a manifest for one producer
// declaring one fact kind under one collector kind.
func componentManifestForGrantedFactKind(componentID, version, collectorKind, factKind string, schemaVersions []string) string {
	var schemaBuilder strings.Builder
	for _, version := range schemaVersions {
		schemaBuilder.WriteString("        - ")
		schemaBuilder.WriteString(version)
		schemaBuilder.WriteString("\n")
	}
	out := validManifestYAML()
	out = strings.ReplaceAll(out, "dev.eshu.collector.aws", componentID)
	out = strings.ReplaceAll(out, "version: 0.1.0", "version: "+version)
	out = strings.ReplaceAll(out, "kind: dev.eshu.aws.cloud_resource", "kind: "+factKind)
	out = strings.ReplaceAll(out, "        - 1.0.0\n", schemaBuilder.String())
	return strings.ReplaceAll(out, "    - aws\n", "    - "+collectorKind+"\n")
}

func liveGrantFor(producerID, version, kind, scope string, schemaVersions []string) ProducerGrant {
	return ProducerGrant{
		ProducerID:     producerID,
		Version:        version,
		Kind:           kind,
		SchemaVersions: append([]string(nil), schemaVersions...),
		Scope:          scope,
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}
}

func installGranted(t *testing.T, registry Registry, body string, componentID, version string) {
	t.Helper()

	if _, err := registry.Install(writeManifest(t, body), verificationFor(componentID, version)); err != nil {
		t.Fatalf("Install(%s %s) error = %v, want nil", componentID, version, err)
	}
}

// TestRegistryInstallAndEnableAllowsSharedGrantedCoreKind proves two distinct
// granted components can install and enable while declaring the same
// core-owned fact kind with one shared schema major.
func TestRegistryInstallAndEnableAllowsSharedGrantedCoreKind(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	loki := componentManifestForGrantedFactKind(
		"dev.example.collector.loki", "0.1.0", "loki", sharedGrantedCoreKind, []string{"1.0.0"},
	)
	tempo := componentManifestForGrantedFactKind(
		"dev.example.collector.tempo", "0.1.0", "tempo", sharedGrantedCoreKind, []string{"1.0.0"},
	)
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.loki", "0.1.0", sharedGrantedCoreKind, "loki", []string{"1.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(loki) error = %v, want nil", err)
	}
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.tempo", "0.1.0", sharedGrantedCoreKind, "tempo", []string{"1.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(tempo) error = %v, want nil", err)
	}

	installGranted(t, registry, loki, "dev.example.collector.loki", "0.1.0")
	installGranted(t, registry, tempo, "dev.example.collector.tempo", "0.1.0")

	if _, err := registry.Enable("dev.example.collector.loki", Activation{InstanceID: "loki"}); err != nil {
		t.Fatalf("Enable(loki) error = %v, want nil", err)
	}
	if _, err := registry.Enable("dev.example.collector.tempo", Activation{InstanceID: "tempo"}); err != nil {
		t.Fatalf("Enable(tempo) error = %v, want nil", err)
	}
}

// TestRegistryInstallRejectsSharedCoreKindWhenEitherGrantLapses proves a
// revoked or expired grant on either side of a shared core-owned kind still
// rejects the second install.
func TestRegistryInstallRejectsSharedCoreKindWhenEitherGrantLapses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lapseSide string
		lapseKind string
		wantCode  ErrorCode
	}{
		// A lapsed grant on the installed side fails the fail-closed reload of
		// that manifest before collision comparison runs.
		{name: "installed side revoked", lapseSide: "loki", lapseKind: "revoked", wantCode: ErrorCodeCorruptedRegistryState},
		{name: "installed side expired", lapseSide: "loki", lapseKind: "expired", wantCode: ErrorCodeCorruptedRegistryState},
		{name: "candidate side revoked", lapseSide: "tempo", lapseKind: "revoked", wantCode: ErrorCodeInvalidManifest},
		{name: "candidate side expired", lapseSide: "tempo", lapseKind: "expired", wantCode: ErrorCodeInvalidManifest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registry := NewRegistry(t.TempDir())
			lokiGrant := liveGrantFor(
				"dev.example.collector.loki", "0.1.0", sharedGrantedCoreKind, "loki", []string{"1.0.0"},
			)
			tempoGrant := liveGrantFor(
				"dev.example.collector.tempo", "0.1.0", sharedGrantedCoreKind, "tempo", []string{"1.0.0"},
			)
			if err := registry.RecordGrant(lokiGrant); err != nil {
				t.Fatalf("RecordGrant(loki) error = %v, want nil", err)
			}
			if err := registry.RecordGrant(tempoGrant); err != nil {
				t.Fatalf("RecordGrant(tempo) error = %v, want nil", err)
			}
			installGranted(t, registry, componentManifestForGrantedFactKind(
				"dev.example.collector.loki", "0.1.0", "loki", sharedGrantedCoreKind, []string{"1.0.0"},
			), "dev.example.collector.loki", "0.1.0")

			lapsed := lokiGrant
			if tt.lapseSide == "tempo" {
				lapsed = tempoGrant
			}
			switch tt.lapseKind {
			case "revoked":
				lapsed.Revoked = true
			case "expired":
				lapsed.ExpiresAt = time.Now().Add(-time.Hour).UTC()
			}
			if err := registry.RecordGrant(lapsed); err != nil {
				t.Fatalf("RecordGrant(lapsed %s) error = %v, want nil", tt.lapseSide, err)
			}

			_, err := registry.Install(writeManifest(t, componentManifestForGrantedFactKind(
				"dev.example.collector.tempo", "0.1.0", "tempo", sharedGrantedCoreKind, []string{"1.0.0"},
			)), verificationFor("dev.example.collector.tempo", "0.1.0"))
			if err == nil {
				t.Fatalf("Install(tempo) error = nil, want %q after %s grant %s", tt.wantCode, tt.lapseSide, tt.lapseKind)
			}
			if got := ErrorCodeOf(err); got != tt.wantCode {
				t.Fatalf("Install(tempo) code = %q, want %q; err=%v", got, tt.wantCode, err)
			}
		})
	}
}

// TestRegistryInstallRejectsSharedCoreKindOnSchemaMajorMismatch proves two
// live grants do not excuse divergent schema majors on a shared core-owned
// kind.
func TestRegistryInstallRejectsSharedCoreKindOnSchemaMajorMismatch(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.loki", "0.1.0", sharedGrantedCoreKind, "loki", []string{"1.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(loki) error = %v, want nil", err)
	}
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.tempo", "0.1.0", sharedGrantedCoreKind, "tempo", []string{"2.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(tempo) error = %v, want nil", err)
	}
	installGranted(t, registry, componentManifestForGrantedFactKind(
		"dev.example.collector.loki", "0.1.0", "loki", sharedGrantedCoreKind, []string{"1.0.0"},
	), "dev.example.collector.loki", "0.1.0")

	_, err := registry.Install(writeManifest(t, componentManifestForGrantedFactKind(
		"dev.example.collector.tempo", "0.1.0", "tempo", sharedGrantedCoreKind, []string{"2.0.0"},
	)), verificationFor("dev.example.collector.tempo", "0.1.0"))
	if err == nil {
		t.Fatal("Install(tempo) error = nil, want fact-kind collision on schema-major mismatch")
	}
	if got, want := ErrorCodeOf(err), ErrorCode("fact_kind_collision"); got != want {
		t.Fatalf("Install(tempo) code = %q, want %q; err=%v", got, want, err)
	}
}

// TestRegistryInstallRejectsSharedNonCoreKindDespiteGrants proves producer
// grants never loosen the strict single-owner collision for non-core
// namespaced kinds.
func TestRegistryInstallRejectsSharedNonCoreKindDespiteGrants(t *testing.T) {
	t.Parallel()

	const namespacedKind = "dev.example.shared.finding"

	registry := NewRegistry(t.TempDir())
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.alpha", "0.1.0", namespacedKind, "alpha", []string{"1.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(alpha) error = %v, want nil", err)
	}
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.beta", "0.1.0", namespacedKind, "beta", []string{"1.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(beta) error = %v, want nil", err)
	}
	installGranted(t, registry, componentManifestForGrantedFactKind(
		"dev.example.collector.alpha", "0.1.0", "alpha", namespacedKind, []string{"1.0.0"},
	), "dev.example.collector.alpha", "0.1.0")

	_, err := registry.Install(writeManifest(t, componentManifestForGrantedFactKind(
		"dev.example.collector.beta", "0.1.0", "beta", namespacedKind, []string{"1.0.0"},
	)), verificationFor("dev.example.collector.beta", "0.1.0"))
	if err == nil {
		t.Fatal("Install(beta) error = nil, want fact-kind collision for shared non-core kind")
	}
	if got, want := ErrorCodeOf(err), ErrorCode("fact_kind_collision"); got != want {
		t.Fatalf("Install(beta) code = %q, want %q; err=%v", got, want, err)
	}
}

// TestPlanEnableAllowsSharedGrantedCoreKind proves the enable-time collision
// check (which guards registries written before install-time checks) admits
// two distinct granted components declaring one shared core-owned kind.
func TestPlanEnableAllowsSharedGrantedCoreKind(t *testing.T) {
	t.Parallel()

	registry := NewRegistry(t.TempDir())
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.loki", "0.1.0", sharedGrantedCoreKind, "loki", []string{"1.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(loki) error = %v, want nil", err)
	}
	if err := registry.RecordGrant(liveGrantFor(
		"dev.example.collector.tempo", "0.1.0", sharedGrantedCoreKind, "tempo", []string{"1.0.0"},
	)); err != nil {
		t.Fatalf("RecordGrant(tempo) error = %v, want nil", err)
	}
	installGranted(t, registry, componentManifestForGrantedFactKind(
		"dev.example.collector.loki", "0.1.0", "loki", sharedGrantedCoreKind, []string{"1.0.0"},
	), "dev.example.collector.loki", "0.1.0")
	installGranted(t, registry, componentManifestForGrantedFactKind(
		"dev.example.collector.tempo", "0.1.0", "tempo", sharedGrantedCoreKind, []string{"1.0.0"},
	), "dev.example.collector.tempo", "0.1.0")

	if _, err := registry.PlanEnable("dev.example.collector.loki", Activation{InstanceID: "loki"}); err != nil {
		t.Fatalf("PlanEnable(loki) error = %v, want nil", err)
	}
	if _, err := registry.PlanEnable("dev.example.collector.tempo", Activation{InstanceID: "tempo"}); err != nil {
		t.Fatalf("PlanEnable(tempo) error = %v, want nil", err)
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
