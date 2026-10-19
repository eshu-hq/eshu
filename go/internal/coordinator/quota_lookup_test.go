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

// TestComponentQuotaOverrideFromEnvEmpty proves an unset operator surface
// leaves both override fields nil, so manifest declarations stand.
func TestComponentQuotaOverrideFromEnvEmpty(t *testing.T) {
	t.Parallel()

	override, err := componentQuotaOverrideFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatalf("componentQuotaOverrideFromEnv() error = %v, want nil", err)
	}
	if override.MaxGenerationsPerHour != nil {
		t.Fatalf("override.MaxGenerationsPerHour = %d, want nil (unset stays unset)", *override.MaxGenerationsPerHour)
	}
	if override.MaxFactsPerGeneration != nil {
		t.Fatalf("override.MaxFactsPerGeneration = %d, want nil (unset stays unset)", *override.MaxFactsPerGeneration)
	}
}

// TestComponentQuotaOverrideFromEnvParsesFields proves each documented env
// var lands on its own override field and nothing else.
func TestComponentQuotaOverrideFromEnvParsesFields(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		switch key {
		case "ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR":
			return "12"
		case "ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION":
			return "5000"
		default:
			return ""
		}
	}
	override, err := componentQuotaOverrideFromEnv(getenv)
	if err != nil {
		t.Fatalf("componentQuotaOverrideFromEnv() error = %v, want nil", err)
	}
	if override.MaxGenerationsPerHour == nil || *override.MaxGenerationsPerHour != 12 {
		t.Fatalf("override.MaxGenerationsPerHour = %v, want 12", override.MaxGenerationsPerHour)
	}
	if override.MaxFactsPerGeneration == nil || *override.MaxFactsPerGeneration != 5000 {
		t.Fatalf("override.MaxFactsPerGeneration = %v, want 5000", override.MaxFactsPerGeneration)
	}
}

// TestComponentQuotaOverrideFromEnvRejectsNonPositive proves a set-but-
// invalid operator bound fails coordinator startup instead of reaching the
// claim path, where the store would fail it closed per claim.
func TestComponentQuotaOverrideFromEnvRejectsNonPositive(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{name: "zero generations", key: "ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR", value: "0"},
		{name: "negative generations", key: "ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR", value: "-3"},
		{name: "zero facts", key: "ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION", value: "0"},
		{name: "negative facts", key: "ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION", value: "-3"},
		{name: "unparseable generations", key: "ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR", value: "many"},
		{name: "unparseable facts", key: "ESHU_COMPONENT_QUOTA_MAX_FACTS_PER_GENERATION", value: "many"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			getenv := func(key string) string {
				if key == tc.key {
					return tc.value
				}
				return ""
			}
			if _, err := componentQuotaOverrideFromEnv(getenv); err == nil {
				t.Fatalf("componentQuotaOverrideFromEnv(%s=%q) error = nil, want rejection", tc.key, tc.value)
			} else if !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("componentQuotaOverrideFromEnv() error = %v, want it to name %q", err, tc.key)
			}
		})
	}
}

// quotaTestManifest builds an admitted-manifest stand-in: the production
// builder consumes validated component.Manifest values, and registry loading
// plus manifest validation are pinned by the component package's own tests.
func quotaTestManifest(t *testing.T, kinds []string, quotas component.Quota) component.Manifest {
	t.Helper()

	manifest := component.Manifest{
		APIVersion: "eshu.dev/v1alpha1",
		Kind:       "ComponentPackage",
		Metadata: component.Metadata{
			ID:        "dev.eshu.examples.quota",
			Name:      "Quota fixture",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
	}
	manifest.Spec.CompatibleCore = ">=0.0.5 <0.2.0"
	manifest.Spec.ComponentType = component.ComponentTypeCollector
	manifest.Spec.CollectorKinds = kinds
	manifest.Spec.Runtime = component.RuntimeContract{
		SDKProtocol: component.CollectorSDKProtocolV1Alpha1,
		Adapter:     component.RuntimeAdapterOCI,
	}
	manifest.Spec.Quotas = quotas
	return manifest
}

// TestBuildComponentQuotaLookupServesManifestQuota proves a manifest-declared
// bound reaches the claim-path lookup for its collector kind, and that
// uncovered kinds stay unthrottled.
func TestBuildComponentQuotaLookupServesManifestQuota(t *testing.T) {
	t.Parallel()

	limit := 10
	lookup, err := buildComponentQuotaLookup(
		[]component.Manifest{quotaTestManifest(t, []string{"git"}, component.Quota{MaxGenerationsPerHour: &limit})},
		component.Quota{},
	)
	if err != nil {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want nil", err)
	}
	if lookup == nil {
		t.Fatal("buildComponentQuotaLookup() = nil, want a non-nil lookup (run() passes it straight to the store)")
	}
	quota, ok := lookup(scope.CollectorGit)
	if !ok {
		t.Fatal("lookup(git) ok = false, want true (manifest declares a quota)")
	}
	if quota.MaxGenerationsPerHour != limit {
		t.Fatalf("lookup(git) limit = %d, want %d (manifest declaration stands)", quota.MaxGenerationsPerHour, limit)
	}
	if _, ok := lookup(scope.CollectorAWS); ok {
		t.Fatal("lookup(aws) ok = true, want false (uncovered kinds stay unthrottled)")
	}
}

// TestBuildComponentQuotaLookupServesParsedManifestQuota proves the same
// flow starting from real manifest YAML: parse and validate, then build.
// The registry-install round trip is pinned by the component tests and the
// store-wiring round trip by the workflow-coordinator test.
func TestBuildComponentQuotaLookupServesParsedManifestQuota(t *testing.T) {
	t.Parallel()

	raw := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.examples.quota
  name: Quota fixture
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.2.0"
  componentType: collector
  collectorKinds:
    - git
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: oci
  quotas:
    maxGenerationsPerHour: 24
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/examples/quota-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: dev.eshu.examples.quota.snapshot
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
  telemetry:
    metricsPrefix: eshu_dp_example_quota_
`
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	manifest, err := component.LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest() error = %v, want nil", err)
	}
	lookup, err := buildComponentQuotaLookup([]component.Manifest{manifest}, component.Quota{})
	if err != nil {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want nil", err)
	}
	quota, ok := lookup(scope.CollectorGit)
	if !ok {
		t.Fatal("lookup(git) ok = false, want true (parsed manifest declares a quota)")
	}
	if quota.MaxGenerationsPerHour != 24 {
		t.Fatalf("lookup(git) limit = %d, want 24 (parsed manifest declaration)", quota.MaxGenerationsPerHour)
	}
}

// TestBuildComponentQuotaLookupFansOutAcrossKinds proves one manifest fans
// out to every collector kind it declares: the lookup is keyed by one kind,
// the manifest may carry many.
func TestBuildComponentQuotaLookupFansOutAcrossKinds(t *testing.T) {
	t.Parallel()

	limit := 8
	lookup, err := buildComponentQuotaLookup(
		[]component.Manifest{quotaTestManifest(t, []string{"git", "aws"}, component.Quota{MaxGenerationsPerHour: &limit})},
		component.Quota{},
	)
	if err != nil {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want nil", err)
	}
	for _, kind := range []scope.CollectorKind{scope.CollectorGit, scope.CollectorAWS} {
		quota, ok := lookup(kind)
		if !ok {
			t.Fatalf("lookup(%q) ok = false, want true (one manifest fans out to every kind)", string(kind))
		}
		if quota.MaxGenerationsPerHour != limit {
			t.Fatalf("lookup(%q) limit = %d, want %d", kind, quota.MaxGenerationsPerHour, limit)
		}
	}
}

// TestBuildComponentQuotaLookupOverrideWins proves the uniform operator
// override wins per field (component.Quota.Resolve): the generations bound
// comes from the override.
func TestBuildComponentQuotaLookupOverrideWins(t *testing.T) {
	t.Parallel()

	manifestLimit, overrideLimit, factsLimit := 10, 5, 5000
	lookup, err := buildComponentQuotaLookup(
		[]component.Manifest{quotaTestManifest(t, []string{"git"}, component.Quota{
			MaxFactsPerGeneration: &factsLimit,
			MaxGenerationsPerHour: &manifestLimit,
		})},
		component.Quota{MaxGenerationsPerHour: &overrideLimit},
	)
	if err != nil {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want nil", err)
	}
	quota, ok := lookup(scope.CollectorGit)
	if !ok {
		t.Fatal("lookup(git) ok = false, want true")
	}
	if quota.MaxGenerationsPerHour != overrideLimit {
		t.Fatalf("lookup(git) limit = %d, want %d (operator override wins)", quota.MaxGenerationsPerHour, overrideLimit)
	}
}

// TestBuildComponentQuotaLookupOverrideAloneGrantsLimit proves a uniform
// operator override throttles every admitted kind even when no manifest
// declares a quota: the override is deployment policy, not a per-manifest
// patch.
func TestBuildComponentQuotaLookupOverrideAloneGrantsLimit(t *testing.T) {
	t.Parallel()

	overrideLimit := 7
	lookup, err := buildComponentQuotaLookup(
		[]component.Manifest{quotaTestManifest(t, []string{"git"}, component.Quota{})},
		component.Quota{MaxGenerationsPerHour: &overrideLimit},
	)
	if err != nil {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want nil", err)
	}
	quota, ok := lookup(scope.CollectorGit)
	if !ok {
		t.Fatal("lookup(git) ok = false, want true (override alone grants a limit)")
	}
	if quota.MaxGenerationsPerHour != overrideLimit {
		t.Fatalf("lookup(git) limit = %d, want %d", quota.MaxGenerationsPerHour, overrideLimit)
	}
}

// TestBuildComponentQuotaLookupSkipsUnthrottledKinds proves a kind with no
// effective generations bound stays on the plain statement: the lookup
// reports no quota rather than a zero limit (which the store rejects as a
// configuration error).
func TestBuildComponentQuotaLookupSkipsUnthrottledKinds(t *testing.T) {
	t.Parallel()

	lookup, err := buildComponentQuotaLookup(
		[]component.Manifest{quotaTestManifest(t, []string{"git"}, component.Quota{})},
		component.Quota{},
	)
	if err != nil {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want nil", err)
	}
	if lookup == nil {
		t.Fatal("buildComponentQuotaLookup() = nil, want non-nil even with nothing to enforce")
	}
	if _, ok := lookup(scope.CollectorGit); ok {
		t.Fatal("lookup(git) ok = true, want false (no manifest quota and no override means unthrottled)")
	}
}

// TestBuildComponentQuotaLookupRejectsConflictingKindBudgets proves one
// kind gets one budget: two planned manifests declaring different resolved
// limits for the same kind fail startup with the kind named, instead of
// silently picking a winner the operator did not choose.
func TestBuildComponentQuotaLookupRejectsConflictingKindBudgets(t *testing.T) {
	t.Parallel()

	low, high := 5, 10
	first := quotaTestManifest(t, []string{"git"}, component.Quota{MaxGenerationsPerHour: &low})
	second := quotaTestManifest(t, []string{"git"}, component.Quota{MaxGenerationsPerHour: &high})
	second.Metadata.ID = "dev.eshu.examples.other"
	if _, err := buildComponentQuotaLookup([]component.Manifest{first, second}, component.Quota{}); err == nil {
		t.Fatal("buildComponentQuotaLookup() error = nil, want rejection (one kind, one budget)")
	} else if !strings.Contains(err.Error(), "git") {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want it to name the contested kind", err)
	}
}

// TestBuildComponentQuotaLookupIgnoresBlankKinds proves a blank collector
// kind entry never becomes a lookup key: trimming is defensive, registry
// manifests are already validated upstream.
func TestBuildComponentQuotaLookupIgnoresBlankKinds(t *testing.T) {
	t.Parallel()

	limit := 9
	lookup, err := buildComponentQuotaLookup(
		[]component.Manifest{quotaTestManifest(t, []string{"  ", "git"}, component.Quota{MaxGenerationsPerHour: &limit})},
		component.Quota{},
	)
	if err != nil {
		t.Fatalf("buildComponentQuotaLookup() error = %v, want nil", err)
	}
	if _, ok := lookup(scope.CollectorKind("")); ok {
		t.Fatal("lookup(\"\") ok = true, want false (blank kinds never register)")
	}
	if quota, ok := lookup(scope.CollectorGit); !ok || quota.MaxGenerationsPerHour != limit {
		t.Fatalf("lookup(git) = (%+v, %v), want ({%d}, true)", quota, ok, limit)
	}
}

// TestLoadConfigBuildsNonNilQuotaLookup proves the production construction
// site always carries a lookup: with no component home and no override it
// serves nothing, so quota-less deployments keep the plain statement.
func TestLoadConfigBuildsNonNilQuotaLookup(t *testing.T) {
	t.Parallel()

	cfg, err := LoadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if cfg.QuotaLookup == nil {
		t.Fatal("cfg.QuotaLookup = nil, want non-nil (run() passes it straight to the store)")
	}
	if _, ok := cfg.QuotaLookup(scope.CollectorGit); ok {
		t.Fatal("cfg.QuotaLookup(git) ok = true, want false (nothing configured)")
	}
}
