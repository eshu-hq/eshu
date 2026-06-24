// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLoadRuntimeConfigSelectsTrustedProcessActivation(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	configPath := writeComponentExtensionConfig(t)
	installEnabledComponent(t, componentHome, "process", configPath)

	config, err := loadRuntimeConfig(mapEnv(map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.scorecard",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "scorecard-local",
		envCollectorOwnerID:         "component-owner-a",
	}))
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil", err)
	}
	if got, want := config.Instance.InstanceID, "scorecard-local"; got != want {
		t.Fatalf("InstanceID = %q, want %q", got, want)
	}
	if got, want := config.CollectorKind, scope.CollectorKind("scorecard"); got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := config.ScopeKind, scope.KindRepository; got != want {
		t.Fatalf("ScopeKind = %q, want %q", got, want)
	}
	if got, want := config.Manifest.Metadata.ID, "dev.eshu.examples.scorecard"; got != want {
		t.Fatalf("Manifest ID = %q, want %q", got, want)
	}
	if got, want := config.OwnerID, "component-owner-a"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	processRunner, ok := config.Runner.(extensionhost.ProcessRunner)
	if !ok {
		t.Fatalf("Runner = %T, want extensionhost.ProcessRunner for the process adapter", config.Runner)
	}
	if got, want := processRunner.Command, "/usr/local/bin/scorecard-collector"; got != want {
		t.Fatalf("Runner.Command = %q, want %q", got, want)
	}
	if got, want := strings.Join(processRunner.Args, " "), "--sdk-stdio"; got != want {
		t.Fatalf("Runner.Args = %q, want %q", got, want)
	}
	source, ok := config.ExtensionConfig["source"].(map[string]any)
	if !ok {
		t.Fatalf("ExtensionConfig[source] = %T, want map[string]any", config.ExtensionConfig["source"])
	}
	if got, want := source["input"], "/fixtures/scorecard.json"; got != want {
		t.Fatalf("ExtensionConfig source.input = %q, want %q", got, want)
	}
}

func TestLoadRuntimeConfigRejectsUntrustedActivation(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, "process", writeComponentExtensionConfig(t))

	_, err := loadRuntimeConfig(mapEnv(map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.other",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
	}))
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want untrusted activation rejection")
	}
	if !strings.Contains(err.Error(), "no trusted claim-capable component activation") {
		t.Fatalf("loadRuntimeConfig() error = %q, want trusted activation message", err)
	}
}

func TestLoadRuntimeConfigBuildsOCIRunnerFromManifestArtifact(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, component.RuntimeAdapterOCI, writeComponentExtensionConfig(t))

	config, err := loadRuntimeConfig(mapEnv(map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.scorecard",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
	}))
	if err != nil {
		t.Fatalf("loadRuntimeConfig() error = %v, want nil for the OCI adapter", err)
	}
	ociRunner, ok := config.Runner.(extensionhost.OCIRunner)
	if !ok {
		t.Fatalf("Runner = %T, want extensionhost.OCIRunner for the oci adapter", config.Runner)
	}
	const wantImage = "ghcr.io/eshu-hq/examples/scorecard-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if ociRunner.ImageRef != wantImage {
		t.Fatalf("OCIRunner.ImageRef = %q, want manifest digest %q", ociRunner.ImageRef, wantImage)
	}
}

func TestLoadRuntimeConfigOmitsActivationConfigPathFromReadError(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	configPath := t.TempDir() + "/private-scorecard.yaml"
	installEnabledComponent(t, componentHome, "process", configPath)

	_, err := loadRuntimeConfig(mapEnv(map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.scorecard",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "scorecard-local",
	}))
	if err == nil {
		t.Fatal("loadRuntimeConfig() error = nil, want config read error")
	}
	if strings.Contains(err.Error(), configPath) || strings.Contains(err.Error(), "private-scorecard") {
		t.Fatalf("loadRuntimeConfig() error = %q, did not want raw activation config path", err)
	}
}

func TestBuildClaimedServiceWiresExtensionHost(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, "process", writeComponentExtensionConfig(t))

	service, err := buildClaimedService(
		&fakeExecQueryer{},
		mapEnv(map[string]string{
			envComponentHome:            componentHome,
			envComponentTrustMode:       component.TrustModeAllowlist,
			envComponentAllowIDs:        "dev.eshu.examples.scorecard",
			envComponentAllowPublishers: "eshu-hq",
			envComponentCoreVersion:     "dev",
			envCollectorOwnerID:         "component-owner-a",
		}),
		noop.NewTracerProvider().Tracer("test"),
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}
	if _, ok := service.Source.(*extensionhost.Source); !ok {
		t.Fatalf("Source = %T, want *extensionhost.Source", service.Source)
	}
	if got, want := service.CollectorKind, scope.CollectorKind("scorecard"); got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := service.CollectorInstanceID, "scorecard-local"; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := service.OwnerID, "component-owner-a"; got != want {
		t.Fatalf("OwnerID = %q, want %q", got, want)
	}
	if got, want := service.MaxAttempts, workflow.DefaultClaimMaxAttempts(); got != want {
		t.Fatalf("MaxAttempts = %d, want %d", got, want)
	}
}

func installEnabledComponent(t *testing.T, componentHome string, adapter string, configPath string) {
	t.Helper()

	manifestPath := writeComponentManifest(t, adapter)
	manifest, err := component.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil", err)
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{manifest.Metadata.ID},
		AllowedPublishers: []string{manifest.Metadata.Publisher},
		CoreVersion:       "dev",
	}
	registry := component.NewRegistry(componentHome)
	if _, err := registry.Install(manifestPath, policy.Verify(manifest)); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	if _, err := registry.Enable(manifest.Metadata.ID, component.Activation{
		InstanceID:    "scorecard-local",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
}

func writeComponentManifest(t *testing.T, adapter string) string {
	t.Helper()

	path := t.TempDir() + "/manifest.yaml"
	raw := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.examples.scorecard
  name: Reference Scorecard collector
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - scorecard
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: ` + adapter + `
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/examples/scorecard-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.eshu.examples.scorecard.snapshot
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func writeComponentExtensionConfig(t *testing.T) string {
	t.Helper()

	path := t.TempDir() + "/config.yaml"
	raw := `host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
process:
  command: /usr/local/bin/scorecard-collector
  args:
    - --sdk-stdio
config:
  source:
    input: /fixtures/scorecard.json
    sourceURI: https://api.securityscorecards.dev/projects/github.com/example/widgets
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

type fakeExecQueryer struct{}

func (f *fakeExecQueryer) QueryContext(context.Context, string, ...any) (postgres.Rows, error) {
	return nil, nil
}

func (f *fakeExecQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}
