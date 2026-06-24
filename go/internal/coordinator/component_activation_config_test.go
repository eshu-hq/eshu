// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestLoadConfigAddsTrustedClaimCapableComponentActivation(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeScorecardActivationConfig(t),
	})

	cfg, err := LoadConfig(componentCoordinatorEnv(home, nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want %d", got, want)
	}
	instance := cfg.CollectorInstances[0]
	if got, want := instance.InstanceID, "scorecard-primary"; got != want {
		t.Fatalf("instance id = %q, want %q", got, want)
	}
	if got, want := instance.CollectorKind, scope.CollectorKind("scorecard"); got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
	if !instance.Enabled || !instance.ClaimsEnabled {
		t.Fatalf("instance enabled=%v claims_enabled=%v, want both true", instance.Enabled, instance.ClaimsEnabled)
	}
	if strings.Contains(instance.Configuration, "private-scorecard.yaml") {
		t.Fatalf("configuration = %s, did not want private config path persisted", instance.Configuration)
	}
	if !strings.Contains(instance.Configuration, `"component_id":"dev.eshu.examples.scorecard"`) {
		t.Fatalf("configuration = %s, want component identity", instance.Configuration)
	}
	if !strings.Contains(instance.Configuration, `"config_handle":"`) {
		t.Fatalf("configuration = %s, want safe config handle", instance.Configuration)
	}
}

func TestLoadConfigAddsActivationHostClaimMetadata(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeScorecardActivationConfig(t),
	})

	cfg, err := LoadConfig(componentCoordinatorEnv(home, nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	config, ok, err := parseComponentInstanceConfig(cfg.CollectorInstances[0].Configuration)
	if err != nil {
		t.Fatalf("parseComponentInstanceConfig() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("parseComponentInstanceConfig() ok = false, want true")
	}
	if config.Host == nil {
		t.Fatalf("component instance config host = nil, want activation host claim metadata")
	}
	if got, want := config.Host.SourceSystem, "openssf-scorecard"; got != want {
		t.Fatalf("host source system = %q, want %q", got, want)
	}
	if got, want := config.Host.Scope.ID, "github.com/example/widgets"; got != want {
		t.Fatalf("host scope id = %q, want %q", got, want)
	}
	if got, want := config.Host.Scope.Kind, string(scope.KindRepository); got != want {
		t.Fatalf("host scope kind = %q, want %q", got, want)
	}
	if strings.Contains(cfg.CollectorInstances[0].Configuration, "scorecard.yaml") {
		t.Fatalf("configuration = %s, did not want raw activation config path", cfg.CollectorInstances[0].Configuration)
	}
}

func TestLoadConfigSkipsRevokedComponentActivation(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeScorecardActivationConfig(t),
	})

	overrides := map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": `[
			{
				"instance_id":"collector-git-primary",
				"collector_kind":"git",
				"mode":"continuous",
				"enabled":true,
				"claims_enabled":true,
				"configuration":{"provider":"github"}
			}
		]`,
		"ESHU_COMPONENT_REVOKE_IDS": "dev.eshu.examples.scorecard",
	}
	cfg, err := LoadConfig(componentCoordinatorEnv(home, overrides))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want only static instance %d", got, want)
	}
	if got, want := cfg.CollectorInstances[0].InstanceID, "collector-git-primary"; got != want {
		t.Fatalf("instance id = %q, want %q", got, want)
	}
}

func TestLoadConfigSkipsUntrustedAndIncompatibleComponentActivations(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name      string
		overrides map[string]string
	}{
		{
			name: "trust disabled",
			overrides: map[string]string{
				"ESHU_COMPONENT_TRUST_MODE": component.TrustModeDisabled,
			},
		},
		{
			name: "incompatible core",
			overrides: map[string]string{
				"ESHU_COMPONENT_CORE_VERSION": "v9.0.0",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home := installScorecardComponent(t)
			enableScorecardComponent(t, home, component.Activation{
				InstanceID:    "scorecard-primary",
				Mode:          "scheduled",
				ClaimsEnabled: true,
				ConfigPath:    writeScorecardActivationConfig(t),
			})
			overrides := map[string]string{
				"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
			}
			for key, value := range tt.overrides {
				overrides[key] = value
			}

			cfg, err := LoadConfig(componentCoordinatorEnv(home, overrides))
			if err != nil {
				t.Fatalf("LoadConfig() error = %v, want nil", err)
			}
			if got, want := len(cfg.CollectorInstances), 1; got != want {
				t.Fatalf("collector instances = %d, want only static instance %d", got, want)
			}
			if got, want := cfg.CollectorInstances[0].InstanceID, "collector-git-primary"; got != want {
				t.Fatalf("instance id = %q, want %q", got, want)
			}
		})
	}
}

func TestLoadConfigSkipsComponentActivationWithoutClaims(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-primary",
		Mode:          "scheduled",
		ClaimsEnabled: false,
		ConfigPath:    writeScorecardActivationConfig(t),
	})

	cfg, err := LoadConfig(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want only static instance %d", got, want)
	}
	if got, want := cfg.CollectorInstances[0].InstanceID, "collector-git-primary"; got != want {
		t.Fatalf("instance id = %q, want %q", got, want)
	}
}

func installScorecardComponent(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(scorecardManifestYAML()), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	policy := scorecardAllowlistPolicy()
	manifest, err := component.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil", err)
	}
	verification := policy.Verify(manifest)
	if !verification.Allowed {
		t.Fatalf("policy denied manifest: %#v", verification)
	}
	home := filepath.Join(dir, "components")
	if _, err := component.NewRegistry(home).Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	return home
}

func enableScorecardComponent(t *testing.T, home string, activation component.Activation) {
	t.Helper()

	if _, err := component.NewRegistry(home).Enable("dev.eshu.examples.scorecard", activation); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
}

func writeScorecardActivationConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "scorecard.yaml")
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
		t.Fatalf("write scorecard activation config: %v", err)
	}
	return path
}

func componentCoordinatorEnv(home string, overrides map[string]string) func(string) string {
	values := map[string]string{
		"ESHU_WORKFLOW_COORDINATOR_DEPLOYMENT_MODE": "active",
		"ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED":  "true",
		"ESHU_COMPONENT_HOME":                       home,
		"ESHU_COMPONENT_TRUST_MODE":                 component.TrustModeAllowlist,
		"ESHU_COMPONENT_ALLOW_IDS":                  "dev.eshu.examples.scorecard",
		"ESHU_COMPONENT_ALLOW_PUBLISHERS":           "eshu-hq",
		"ESHU_COMPONENT_CORE_VERSION":               "v0.1.0",
	}
	for key, value := range overrides {
		values[key] = value
	}
	return func(key string) string {
		return values[key]
	}
}

func staticGitCollectorJSON() string {
	return `[
		{
			"instance_id":"collector-git-primary",
			"collector_kind":"git",
			"mode":"continuous",
			"enabled":true,
			"claims_enabled":true,
			"configuration":{"provider":"github"}
		}
	]`
}

func scorecardAllowlistPolicy() component.Policy {
	return component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.examples.scorecard"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.1.0",
	}
}

func scorecardManifestYAML() string {
	return `apiVersion: eshu.dev/v1alpha1
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
    adapter: oci
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
  telemetry:
    metricsPrefix: eshu_dp_example_scorecard_
`
}
