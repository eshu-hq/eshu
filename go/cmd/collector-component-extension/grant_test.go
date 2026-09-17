// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// TestLoadRuntimeConfigAdmitsGrantedCoreFactKind is the #6709 slice-2b TDD
// red test: a recorded producer grant must carry a core-owned declaration
// through policy verification, install, enable, activation selection, and
// runtime config — including the grants themselves for extension-host
// construction — so an authorized first-party producer can actually run.
func TestLoadRuntimeConfigAdmitsGrantedCoreFactKind(t *testing.T) {
	t.Parallel()

	const (
		componentID = "dev.eshu.examples.granted"
		version     = "0.1.0"
		coreKind    = "aws_resource"
		grantScope  = "scorecard"
	)
	manifest := grantedComponentManifest(componentID, version, coreKind)
	grants := []component.ProducerGrant{{
		ProducerID:     componentID,
		Version:        version,
		Kind:           coreKind,
		SchemaVersions: []string{"1.0.0"},
		Scope:          grantScope,
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}}

	componentHome := t.TempDir()
	registry := component.NewRegistry(componentHome)
	if err := registry.RecordGrant(grants[0]); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{componentID},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "dev",
	}
	verification := policy.VerifyWithGrants(context.Background(), manifest, grants)
	if !verification.Allowed {
		t.Fatalf("VerifyWithGrants() Allowed = false, want true: %s", verification.Reason)
	}
	manifestPath := writeGrantedManifest(t, componentID, version, coreKind)
	if _, err := registry.Install(manifestPath, verification); err != nil {
		t.Fatalf("Install(granted core kind) error = %v, want nil", err)
	}
	configPath := writeComponentExtensionConfig(t)
	if _, err := registry.Enable(componentID, component.Activation{
		InstanceID:    "granted-local",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}

	config, err := loadRuntimeConfig(mapEnv(map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        componentID,
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "granted-local",
	}))
	if err != nil {
		t.Fatalf("loadRuntimeConfig(granted core kind) error = %v, want nil", err)
	}
	if got := config.Manifest.Metadata.ID; got != componentID {
		t.Fatalf("Manifest ID = %q, want %q", got, componentID)
	}
	if len(config.Grants) == 0 {
		t.Fatal("runtimeConfig.Grants is empty, want recorded grants for extension-host construction")
	}
}

// writeGrantedManifest writes the YAML twin of grantedComponentManifest for
// registry Install, which loads from a staged manifest path.
func writeGrantedManifest(t *testing.T, componentID, version, kind string) string {
	t.Helper()

	path := t.TempDir() + "/manifest.yaml"
	raw := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: ` + componentID + `
  name: Granted first-party collector
  publisher: eshu-hq
  version: ` + version + `
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - scorecard
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: process
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/examples/granted-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: ` + kind + `
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
  telemetry:
    metricsPrefix: eshu_component_granted
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

// grantedComponentManifest mirrors writeComponentManifest with a core-owned
// emitted kind. The manifest struct is built literally because the exported
// LoadManifest entry stays fail-closed without grant context by design.
func grantedComponentManifest(componentID, version, kind string) component.Manifest {
	return component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        componentID,
			Name:      "Granted first-party collector",
			Publisher: "eshu-hq",
			Version:   version,
		},
		Spec: component.Spec{
			CompatibleCore: ">=0.0.5 <0.2.0",
			ComponentType:  component.ComponentTypeCollector,
			CollectorKinds: []string{"scorecard"},
			Runtime: component.RuntimeContract{
				SDKProtocol: component.CollectorSDKProtocolV1Alpha1,
				Adapter:     component.RuntimeAdapterProcess,
			},
			Artifacts: []component.Artifact{{
				Platform: "linux/amd64",
				Image:    "ghcr.io/eshu-hq/examples/granted-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			}},
			EmittedFacts: []component.FactFamily{{
				Kind:             kind,
				SchemaVersions:   []string{"1.0.0"},
				SourceConfidence: []string{"reported"},
			}},
			ConsumerContracts: component.ConsumerContracts{
				Reducer: component.ReducerContract{Phases: []string{"source_evidence_only:no_graph_truth"}},
			},
			Telemetry: component.Telemetry{MetricsPrefix: "eshu_component_granted"},
		},
	}
}
