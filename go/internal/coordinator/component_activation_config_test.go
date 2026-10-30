// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
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
	config, ok, err := activation.ParseConfig(cfg.CollectorInstances[0].Configuration)
	if err != nil {
		t.Fatalf("ParseConfig() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("ParseConfig() ok = false, want true")
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

func TestLoadConfigSkipsActivationWithMissingConfigFile(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	missing := filepath.Join(t.TempDir(), "removed-activation.yaml")
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-stale",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    missing,
	})

	cfg, err := LoadConfig(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil (stale activation skips, #9634)", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want only static instance %d", got, want)
	}
	if got, want := cfg.CollectorInstances[0].InstanceID, "collector-git-primary"; got != want {
		t.Fatalf("instance id = %q, want %q", got, want)
	}
	if got, want := len(cfg.StaleComponentActivationSkips), 1; got != want {
		t.Fatalf("stale skips = %d, want %d", got, want)
	}
	skip := cfg.StaleComponentActivationSkips[0]
	if got, want := skip.ComponentID, "dev.eshu.examples.scorecard"; got != want {
		t.Fatalf("stale skip component = %q, want %q", got, want)
	}
	if got, want := skip.InstanceID, "scorecard-stale"; got != want {
		t.Fatalf("stale skip instance = %q, want %q", got, want)
	}
}

func TestLoadConfigPlansLiveActivationAlongsideMissingConfigFile(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeScorecardActivationConfig(t),
	})
	missing := filepath.Join(t.TempDir(), "removed-activation.yaml")
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-stale",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    missing,
	})

	cfg, err := LoadConfig(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil (stale activation skips, #9634)", err)
	}
	if got, want := len(cfg.CollectorInstances), 2; got != want {
		t.Fatalf("collector instances = %d, want static plus live activation %d", got, want)
	}
	ids := map[string]bool{}
	for _, instance := range cfg.CollectorInstances {
		ids[instance.InstanceID] = true
	}
	if !ids["collector-git-primary"] || !ids["scorecard-primary"] {
		t.Fatalf("planned instances = %v, want static and live activation", ids)
	}
	if ids["scorecard-stale"] {
		t.Fatalf("planned instances = %v, did not want stale activation", ids)
	}
	if got, want := len(cfg.StaleComponentActivationSkips), 1; got != want {
		t.Fatalf("stale skips = %d, want %d", got, want)
	}
	skip := cfg.StaleComponentActivationSkips[0]
	if got, want := skip.ComponentID, "dev.eshu.examples.scorecard"; got != want {
		t.Fatalf("stale skip component = %q, want %q", got, want)
	}
	if got, want := skip.InstanceID, "scorecard-stale"; got != want {
		t.Fatalf("stale skip instance = %q, want %q", got, want)
	}
}

func TestReconcileStaleComponentActivationsDropsMissingFile(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	missing := filepath.Join(t.TempDir(), "removed-activation.yaml")
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-stale",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    missing,
	})

	cfg, err := LoadConfig(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := len(cfg.StaleComponentActivationSkips), 1; got != want {
		t.Fatalf("stale skips = %d, want %d", got, want)
	}
	outcomes := ReconcileStaleComponentActivations(home, cfg.StaleComponentActivationSkips)
	if got, want := len(outcomes), 1; got != want {
		t.Fatalf("reconcile outcomes = %d, want %d", got, want)
	}
	if !outcomes[0].Dropped || outcomes[0].Err != nil {
		t.Fatalf("reconcile outcome = %+v, want dropped with nil error", outcomes[0])
	}

	// The stale entry is gone: a second planning pass sees nothing to skip
	// and the registry no longer references the removed file.
	after, err := LoadConfig(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() after reconcile error = %v, want nil", err)
	}
	if got := len(after.StaleComponentActivationSkips); got != 0 {
		t.Fatalf("stale skips after reconcile = %d, want 0", got)
	}
	remaining, err := component.NewRegistry(home).List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	for _, entry := range remaining {
		for _, activation := range entry.Activations {
			if activation.InstanceID == "scorecard-stale" {
				t.Fatalf("stale activation still registered: %+v", activation)
			}
		}
	}
}

func TestReconcileStaleComponentActivationsKeepsLiveActivation(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeScorecardActivationConfig(t),
	})
	skips := []StaleActivationSkip{{
		ComponentID: "dev.eshu.examples.scorecard",
		InstanceID:  "scorecard-stale",
	}}
	outcomes := ReconcileStaleComponentActivations(home, skips)
	if got, want := len(outcomes), 1; got != want {
		t.Fatalf("reconcile outcomes = %d, want %d", got, want)
	}
	// The unknown instance is not registered, so there is nothing to drop,
	// and the live activation the skip list did not name is untouched.
	if outcomes[0].Dropped || outcomes[0].Err != nil {
		t.Fatalf("reconcile outcome = %+v, want no drop and nil error", outcomes[0])
	}
	remaining, err := component.NewRegistry(home).List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	found := false
	for _, entry := range remaining {
		for _, activation := range entry.Activations {
			if activation.InstanceID == "scorecard-primary" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("live activation missing after reconcile, want it untouched")
	}
}

func TestReconcileStaleComponentActivationsSkipsEmpty(t *testing.T) {
	t.Parallel()

	if outcomes := ReconcileStaleComponentActivations(t.TempDir(), nil); outcomes != nil {
		t.Fatalf("reconcile outcomes = %+v, want nil for empty skips", outcomes)
	}
}

func TestReconcileStaleComponentActivationsKeepsEntryWhoseFileReappeared(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	missing := filepath.Join(t.TempDir(), "flapping-activation.yaml")
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-flapping",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    missing,
	})
	// The file arrives between planning and reconcile (for example a slow
	// volume mount catching up): the entry is live again and must stay.
	if err := os.WriteFile(missing, []byte("host: {}\n"), 0o600); err != nil {
		t.Fatalf("write reappeared activation config: %v", err)
	}

	outcomes := ReconcileStaleComponentActivations(home, []StaleActivationSkip{{
		ComponentID: "dev.eshu.examples.scorecard",
		InstanceID:  "scorecard-flapping",
	}})
	if got, want := len(outcomes), 1; got != want {
		t.Fatalf("reconcile outcomes = %d, want %d", got, want)
	}
	if outcomes[0].Dropped || outcomes[0].Err != nil {
		t.Fatalf("reconcile outcome = %+v, want entry kept with nil error", outcomes[0])
	}
	remaining, err := component.NewRegistry(home).List()
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	found := false
	for _, entry := range remaining {
		for _, activation := range entry.Activations {
			if activation.InstanceID == "scorecard-flapping" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("live activation dropped on reappeared file, want it kept")
	}
}

func TestReconcileStaleComponentActivationsReportsCorruptRegistry(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "registry.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt registry: %v", err)
	}

	outcomes := ReconcileStaleComponentActivations(home, []StaleActivationSkip{{
		ComponentID: "dev.eshu.examples.scorecard",
		InstanceID:  "scorecard-stale",
	}})
	if got, want := len(outcomes), 1; got != want {
		t.Fatalf("reconcile outcomes = %d, want %d", got, want)
	}
	if outcomes[0].Dropped || outcomes[0].Err == nil {
		t.Fatalf("reconcile outcome = %+v, want no drop and a reported error", outcomes[0])
	}
}

func TestLoadConfigStillFailsClosedOnUnreadableActivationConfig(t *testing.T) {
	t.Parallel()

	home := installScorecardComponent(t)
	path := filepath.Join(t.TempDir(), "scorecard.yaml")
	if err := os.WriteFile(path, []byte("host:\n  - not-a-mapping\n"), 0o600); err != nil {
		t.Fatalf("write corrupt activation config: %v", err)
	}
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-corrupt",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    path,
	})

	if _, err := LoadConfig(componentCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	})); err == nil {
		t.Fatal("LoadConfig() error = nil, want fail-closed on corrupt activation config")
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
