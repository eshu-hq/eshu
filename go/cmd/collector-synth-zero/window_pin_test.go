// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	remoteclaim "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/remote/claim"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/coordinator/egress"
)

const (
	synthComponentID = "dev.eshu.collector.synth-8877-cutover"
	synthVersion     = "0.1.0"
	synthKind        = "synth_zero_fact"
	synthInstanceID  = "synth-8877-cutover-instance-1"
	synthFactKind    = "dev.eshu.synth_zero_fact.emission"
	synthBinaryPath  = "/usr/local/bin/eshu-collector-synth-zero"
)

var digestPattern = regexp.MustCompile(`@sha256:[A-Fa-f0-9]{64}$`)

// seedRegistry installs the authored manifest, grant, and activation into a
// throwaway registry home and returns the home path.
func seedRegistry(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	registry := component.NewRegistry(home)
	manifestPath, err := filepath.Abs("manifest.yaml")
	if err != nil {
		t.Fatalf("abs manifest: %v", err)
	}
	if _, err := registry.Install(manifestPath, component.VerificationResult{
		Allowed:   true,
		Mode:      component.TrustModeAllowlist,
		Component: synthComponentID,
		Publisher: "eshu-hq",
		Version:   synthVersion,
	}); err != nil {
		t.Fatalf("Install(manifest.yaml) error = %v, want nil", err)
	}
	grantRaw, err := os.ReadFile("trust-grant.json")
	if err != nil {
		t.Fatalf("read trust-grant.json: %v", err)
	}
	var grant component.ProducerGrant
	if err := json.Unmarshal(grantRaw, &grant); err != nil {
		t.Fatalf("decode trust-grant.json: %v", err)
	}
	if err := registry.RecordGrant(grant); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	configPath, err := filepath.Abs("runtime-config.yaml")
	if err != nil {
		t.Fatalf("abs runtime-config: %v", err)
	}
	if _, err := registry.Enable(synthComponentID, component.Activation{
		InstanceID:    synthInstanceID,
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	return home
}

// TestWindowManifestInstallsWithDedicatedKind proves the authored manifest
// loads through the real registry path: identity, runtime, digest-pinned
// artifact, the never-emitted namespaced family, and exactly one collector
// kind. A core-owned kind or a missing family would fail here, not in the
// window.
func TestWindowManifestInstallsWithDedicatedKind(t *testing.T) {
	t.Parallel()

	home := seedRegistry(t)
	readback, err := component.NewRegistry(home).Readback(component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{synthComponentID},
		AllowedPublishers: []string{"eshu-hq"},
	})
	if err != nil {
		t.Fatalf("Readback() error = %v, want nil", err)
	}
	if len(readback) != 1 {
		t.Fatalf("readback components = %d, want 1", len(readback))
	}
	manifest, err := component.NewRegistry(home).LoadInstalledManifest(synthComponentID, synthVersion)
	if err != nil {
		t.Fatalf("LoadInstalledManifest() error = %v, want nil", err)
	}
	if len(manifest.Spec.CollectorKinds) != 1 || manifest.Spec.CollectorKinds[0] != synthKind {
		t.Fatalf("collector kinds = %q, want exactly [%q]", manifest.Spec.CollectorKinds, synthKind)
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
}

// stubRunner never runs: the pin test proves admission, not execution.
type stubRunner struct{}

func (stubRunner) RunCollector(_ context.Context, _ extensionhost.Request) (sdkcollector.Result, error) {
	return sdkcollector.Result{}, errors.New("stub runner never runs")
}

// TestWindowHostAdmitsSynthManifest proves the extension host constructs a
// source from the installed manifest with no producer grants: the dedicated
// kind is not core-owned, so construction must not demand grant coverage.
func TestWindowHostAdmitsSynthManifest(t *testing.T) {
	t.Parallel()

	home := seedRegistry(t)
	manifest, err := component.NewRegistry(home).LoadInstalledManifest(synthComponentID, synthVersion)
	if err != nil {
		t.Fatalf("LoadInstalledManifest() error = %v, want nil", err)
	}
	if _, err := extensionhost.NewSource(extensionhost.Config{
		Manifest:            manifest,
		CollectorInstanceID: synthInstanceID,
		ScopeKind:           "component",
		ConfigHandle:        "synth-handle",
		Runner:              stubRunner{},
	}); err != nil {
		t.Fatalf("NewSource() error = %v, want nil: non-core kind needs no grant", err)
	}
}

// TestWindowGrantReadbackSettlesSynthKind settles the lane-entry NOT_CHECKED:
// the live grant readback works with the synth manifest installed, and the
// seam allows the synth emission without observation because the kind is not
// core-owned.
func TestWindowGrantReadbackSettlesSynthKind(t *testing.T) {
	t.Parallel()

	home := seedRegistry(t)
	ctx := context.Background()
	grants, err := remoteclaim.RegistryGrantReader(home)(ctx)
	if err != nil {
		t.Fatalf("RegistryGrantReader() error = %v, want nil", err)
	}
	found := false
	for _, grant := range grants {
		if grant.ProducerID == synthComponentID && grant.Version == synthVersion {
			found = true
		}
	}
	if !found {
		t.Fatal("grant readback holds no grant for the synth holder")
	}
	client, err := remoteclaim.NewClient(remoteclaim.Config{
		BaseURL:         "https://claim-plane.invalid",
		ProducerID:      synthComponentID,
		ProducerVersion: synthVersion,
		Scopes:          []string{synthKind},
		ReadGrants:      remoteclaim.RegistryGrantReader(home),
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v, want nil", err)
	}
	if err := client.VerifyWiring(ctx, []remoteclaim.DeclaredEmission{
		{Kind: synthFactKind, SchemaVersion: "1.0.0"},
	}); err != nil {
		t.Fatalf("VerifyWiring() error = %v, want nil for non-core kind", err)
	}
	if err := client.AuthorizeEmission(ctx, synthFactKind, "1.0.0"); err != nil {
		t.Fatalf("AuthorizeEmission() error = %v, want nil for non-core kind", err)
	}
}

// deploymentEnv parses the authored Deployment's first container env into a
// name/value map.
func deploymentEnv(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile("deployment.yaml")
	if err != nil {
		t.Fatalf("read deployment.yaml: %v", err)
	}
	var deployment struct {
		Spec struct {
			Template struct {
				Spec struct {
					Containers []struct {
						Env []struct {
							Name  string `yaml:"name"`
							Value string `yaml:"value"`
						} `yaml:"env"`
						Image string `yaml:"image"`
					} `yaml:"containers"`
				} `yaml:"spec"`
			} `yaml:"template"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(raw, &deployment); err != nil {
		t.Fatalf("decode deployment.yaml: %v", err)
	}
	if len(deployment.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("containers = %d, want 1", len(deployment.Spec.Template.Spec.Containers))
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if !digestPattern.MatchString(container.Image) {
		t.Fatalf("image %q is not digest-pinned", container.Image)
	}
	env := make(map[string]string, len(container.Env))
	for _, entry := range container.Env {
		env[entry.Name] = entry.Value
	}
	return env
}

// TestWindowDeploymentCarriesCutoverPin proves the authored worker Deployment
// carries the 4 cutover env vars with the holder pin equal to the manifest
// identity: any drift fails worker startup, so the files must agree here.
func TestWindowDeploymentCarriesCutoverPin(t *testing.T) {
	t.Parallel()

	home := seedRegistry(t)
	manifest, err := component.NewRegistry(home).LoadInstalledManifest(synthComponentID, synthVersion)
	if err != nil {
		t.Fatalf("LoadInstalledManifest() error = %v, want nil", err)
	}
	env := deploymentEnv(t)
	for _, key := range []string{
		"ESHU_EXTENSIONHOST_CUTOVER_ENABLED",
		"ESHU_WORKFLOW_CLAIM_PLANE_URL",
		"ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_ID",
		"ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_VERSION",
	} {
		value, ok := env[key]
		if !ok || strings.TrimSpace(value) == "" {
			t.Fatalf("deployment env %s is missing or blank", key)
		}
	}
	if got := env["ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_ID"]; got != strings.TrimSpace(manifest.Metadata.ID) {
		t.Fatalf("producer id %q != manifest id %q", got, manifest.Metadata.ID)
	}
	if got := env["ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_VERSION"]; got != strings.TrimSpace(manifest.Metadata.Version) {
		t.Fatalf("producer version %q != manifest version %q", got, manifest.Metadata.Version)
	}
	if got := env["ESHU_COMPONENT_COLLECTOR_INSTANCE_ID"]; got != synthInstanceID {
		t.Fatalf("instance id %q, want %q", got, synthInstanceID)
	}
}

// TestWindowEgressAllowsSynthTriple proves the authored egress policy admits
// exactly the window's component, instance, and kind: a missing allow would
// skip planning with an audit instead of failing loudly.
func TestWindowEgressAllowsSynthTriple(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("egress-policy.json")
	if err != nil {
		t.Fatalf("read egress-policy.json: %v", err)
	}
	policy, err := egress.ParseExtensionPolicyJSON(string(raw))
	if err != nil {
		t.Fatalf("ParseExtensionPolicyJSON() error = %v, want nil", err)
	}
	decision := policy.Decide(egress.ExtensionRequest{
		ComponentID:   synthComponentID,
		InstanceID:    synthInstanceID,
		CollectorKind: synthKind,
	})
	if decision.Action != egress.ExtensionActionAllow {
		t.Fatalf("egress decision = %q (%s), want allow", decision.Action, decision.Reason)
	}
}

// TestWindowRuntimeCommandMatchesSeedLayout proves the authored runtime file
// launches the binary path the seed image installs: a mismatch would fail
// every claim at process spawn.
func TestWindowRuntimeCommandMatchesSeedLayout(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("runtime-config.yaml")
	if err != nil {
		t.Fatalf("read runtime-config.yaml: %v", err)
	}
	var runtime struct {
		Process struct {
			Command string `yaml:"command"`
		} `yaml:"process"`
	}
	if err := yaml.Unmarshal(raw, &runtime); err != nil {
		t.Fatalf("decode runtime-config.yaml: %v", err)
	}
	if runtime.Process.Command != synthBinaryPath {
		t.Fatalf("process command %q, want %q", runtime.Process.Command, synthBinaryPath)
	}
	dockerfile, err := os.ReadFile("Dockerfile.seed")
	if err != nil {
		t.Fatalf("read Dockerfile.seed: %v", err)
	}
	if !strings.Contains(string(dockerfile), synthBinaryPath) {
		t.Fatal("Dockerfile.seed installs no binary at the runtime command path")
	}
}
