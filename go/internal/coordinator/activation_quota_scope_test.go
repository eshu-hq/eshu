// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestLoadConfigQuotaLookupSkipsDormantConflictingManifest proves a
// dormant component (installed and admitted but planning zero
// claims-enabled instances) contributes no budget: the same kind with
// different quotas on a planned and a dormant component starts up and
// serves the planned component's limit (#8582).
func TestLoadConfigQuotaLookupSkipsDormantConflictingManifest(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installQuotaComponent(t, home, "dev.eshu.examples.quota-planned", "git", 24)
	installQuotaComponent(t, home, "dev.eshu.examples.quota-dormant", "git", 48)
	enableQuotaComponent(t, home, "dev.eshu.examples.quota-planned", true)
	enableQuotaComponent(t, home, "dev.eshu.examples.quota-dormant", false)

	cfg, err := LoadConfig(quotaCoordinatorEnv(home, nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil (dormant manifest must not conflict)", err)
	}
	quota, ok := cfg.QuotaLookup(scope.CollectorGit)
	if !ok {
		t.Fatal("QuotaLookup(git) ok = false, want true (planned manifest declares a quota)")
	}
	if quota.MaxGenerationsPerHour != 24 {
		t.Fatalf("QuotaLookup(git) limit = %d, want 24 (planned component's budget)", quota.MaxGenerationsPerHour)
	}
}

// TestLoadConfigQuotaLookupSkipsDormantSoleDeclarant proves a kind
// declared only by a dormant component stays unthrottled even when
// another claimant of the kind exists: no silent mis-throttle by a
// component that never runs (#8582).
func TestLoadConfigQuotaLookupSkipsDormantSoleDeclarant(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installQuotaComponent(t, home, "dev.eshu.examples.quota-dormant", "git", 48)
	enableQuotaComponent(t, home, "dev.eshu.examples.quota-dormant", false)

	cfg, err := LoadConfig(quotaCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if _, ok := cfg.QuotaLookup(scope.CollectorGit); ok {
		t.Fatal("QuotaLookup(git) ok = true, want false (dormant sole declarant contributes no budget)")
	}
}

// TestLoadConfigQuotaLookupOverrideAloneGrantsNoDormantBudget proves the
// uniform operator override creates no budget for a kind declared only
// by dormant manifests: the override wins over declarations, and a
// dormant declaration contributes nothing (#8582).
func TestLoadConfigQuotaLookupOverrideAloneGrantsNoDormantBudget(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installQuotaComponent(t, home, "dev.eshu.examples.quota-dormant", "git", 48)
	enableQuotaComponent(t, home, "dev.eshu.examples.quota-dormant", false)
	installQuotaComponent(t, home, "dev.eshu.examples.quota-live", "scorecard", 30)
	enableQuotaComponent(t, home, "dev.eshu.examples.quota-live", true)

	cfg, err := LoadConfig(quotaCoordinatorEnv(home, map[string]string{
		"ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR": "12",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if _, ok := cfg.QuotaLookup(scope.CollectorGit); ok {
		t.Fatal("QuotaLookup(git) ok = true, want false (override alone grants no dormant-declared budget)")
	}
}

// TestLoadConfigQuotaLookupSkipsNeverEnabledManifest proves an
// installed-but-never-enabled component (zero activations) contributes
// no budget, covering the empty-activations plan path (#8582).
func TestLoadConfigQuotaLookupSkipsNeverEnabledManifest(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	installQuotaComponent(t, home, "dev.eshu.examples.quota-dormant", "git", 48)

	cfg, err := LoadConfig(quotaCoordinatorEnv(home, map[string]string{
		"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
	}))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if _, ok := cfg.QuotaLookup(scope.CollectorGit); ok {
		t.Fatal("QuotaLookup(git) ok = true, want false (never-enabled manifest contributes no budget)")
	}
}

// installQuotaComponent installs a quota-declaring git-kind fixture
// component into home, verified against a policy that admits it.
func installQuotaComponent(t *testing.T, home, id, kind string, generationsPerHour int) {
	t.Helper()

	manifestPath := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(manifestPath, []byte(quotaManifestYAML(id, kind, generationsPerHour)), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifest, err := component.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil", err)
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{id},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.1.0",
	}
	verification := policy.Verify(manifest)
	if !verification.Allowed {
		t.Fatalf("policy denied manifest: %#v", verification)
	}
	if _, err := component.NewRegistry(home).Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
}

// enableQuotaComponent enables one activation for an installed fixture
// component, claims-enabled or dormant.
func enableQuotaComponent(t *testing.T, home, id string, claimsEnabled bool) {
	t.Helper()

	if _, err := component.NewRegistry(home).Enable(id, component.Activation{
		InstanceID:    id + "-primary",
		Mode:          "scheduled",
		ClaimsEnabled: claimsEnabled,
		ConfigPath:    writeScorecardActivationConfig(t),
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
}

// quotaCoordinatorEnv is componentCoordinatorEnv with the fixture
// component IDs admitted instead of the scorecard singleton.
func quotaCoordinatorEnv(home string, overrides map[string]string) func(string) string {
	if overrides == nil {
		overrides = map[string]string{}
	}
	overrides["ESHU_COMPONENT_ALLOW_IDS"] = strings.Join([]string{
		"dev.eshu.examples.quota-planned",
		"dev.eshu.examples.quota-dormant",
		"dev.eshu.examples.quota-live",
	}, ",")
	return componentCoordinatorEnv(home, overrides)
}

// quotaManifestYAML renders a single-kind collector manifest declaring
// the given generations-per-hour bound.
func quotaManifestYAML(id, kind string, generationsPerHour int) string {
	factKind := strings.ReplaceAll(strings.TrimPrefix(id, "dev.eshu.examples."), "-", "_")
	return fmt.Sprintf(`apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: %s
  name: Quota scope fixture
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - %s
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: oci
  quotas:
    maxGenerationsPerHour: %d
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/examples/quota-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.eshu.examples.%s.snapshot
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
  telemetry:
    metricsPrefix: eshu_dp_example_%s_
`, id, kind, generationsPerHour, factKind, factKind)
}
