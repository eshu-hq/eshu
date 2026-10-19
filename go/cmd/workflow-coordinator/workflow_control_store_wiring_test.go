// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/coordinator"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestNewWorkflowControlStoreWiresInstruments guards the codex review finding
// on PR #4518: run() must assign the shared meter-provider Instruments onto
// the WorkflowControlStore it constructs, or the #4459 terminal-dead-letter
// operator metric (eshu_dp_workflow_run_terminal_dead_letter_blocks_total)
// never emits on the real production path even though ReconcileWorkflowRuns
// still fails and logs the blocked run correctly. This test fails if a
// future edit drops the store.Instruments assignment inside
// newWorkflowControlStore.
func TestNewWorkflowControlStoreWiresInstruments(t *testing.T) {
	instruments := &telemetry.Instruments{}

	store := newWorkflowControlStore(postgres.SQLDB{}, instruments, nil)

	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.Instruments != instruments {
		t.Fatalf("store.Instruments = %p, want the same instance passed in (%p) — the production wiring must assign it, not leave it nil", store.Instruments, instruments)
	}
}

// TestNewWorkflowControlStoreAllowsNilInstruments proves the wiring helper
// does not require Instruments to be non-nil: WorkflowControlStore treats a
// nil Instruments as a no-op for the dead-letter-block metric (never a
// panic), so a caller without a wired meter provider is unaffected.
func TestNewWorkflowControlStoreAllowsNilInstruments(t *testing.T) {
	store := newWorkflowControlStore(postgres.SQLDB{}, nil, nil)

	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.Instruments != nil {
		t.Fatalf("store.Instruments = %v, want nil when the caller passes nil", store.Instruments)
	}
	if store.QuotaLookup != nil {
		t.Fatal("store.QuotaLookup != nil, want nil when the caller passes nil (every claim takes the plain statement)")
	}
}

// TestNewWorkflowControlStoreWiresQuotaLookup proves the coordinator
// construction site carries a manifest-derived quota lookup into the store,
// so the gated claim path is reachable from construction: the lookup is
// built through component.Quota.Resolve (manifest declares, operator
// override wins per field), the store serves the resolved budget for the
// covered kind, and uncovered kinds stay unthrottled.
func TestNewWorkflowControlStoreWiresQuotaLookup(t *testing.T) {
	manifestLimit := 10
	overrideLimit := 5
	manifest := component.Quota{MaxGenerationsPerHour: &manifestLimit}
	newQuotaLookup := func(override component.Quota) workflow.QuotaLookup {
		effective := manifest.Resolve(override)
		return func(kind scope.CollectorKind) (workflow.ClaimQuota, bool) {
			if kind != scope.CollectorGit {
				return workflow.ClaimQuota{}, false
			}
			limit, ok := effective.GenerationsPerHourLimit()
			if !ok {
				return workflow.ClaimQuota{}, false
			}
			return workflow.ClaimQuota{MaxGenerationsPerHour: limit}, true
		}
	}

	store := newWorkflowControlStore(postgres.SQLDB{}, nil, newQuotaLookup(component.Quota{MaxGenerationsPerHour: &overrideLimit}))
	if store == nil {
		t.Fatal("newWorkflowControlStore() = nil, want a constructed store")
	}
	if store.QuotaLookup == nil {
		t.Fatal("store.QuotaLookup = nil, want the manifest-derived lookup (a nil lookup keeps every claim on the plain statement)")
	}
	quota, ok := store.QuotaLookup(scope.CollectorGit)
	if !ok {
		t.Fatal("store.QuotaLookup(git) ok = false, want true (manifest declares a quota)")
	}
	if quota.MaxGenerationsPerHour != overrideLimit {
		t.Fatalf("store.QuotaLookup(git) limit = %d, want %d (operator override wins per field)", quota.MaxGenerationsPerHour, overrideLimit)
	}
	if _, ok := store.QuotaLookup(scope.CollectorAWS); ok {
		t.Fatal("store.QuotaLookup(aws) ok = true, want false (uncovered kinds stay unthrottled)")
	}

	// Without an override the manifest declaration stands.
	manifestStore := newWorkflowControlStore(postgres.SQLDB{}, nil, newQuotaLookup(component.Quota{}))
	quota, ok = manifestStore.QuotaLookup(scope.CollectorGit)
	if !ok {
		t.Fatal("store.QuotaLookup(git) ok = false with empty override, want true (manifest declaration stands)")
	}
	if quota.MaxGenerationsPerHour != manifestLimit {
		t.Fatalf("store.QuotaLookup(git) limit = %d, want %d (empty override keeps the manifest declaration)", quota.MaxGenerationsPerHour, manifestLimit)
	}
}

// TestProductionWiringPassesManifestQuotaToStore proves the acceptance
// criterion on #8561 through the real production construction path — not a
// hand-built closure: a manifest YAML declaring spec.quotas is installed
// into a registry, coordinator.LoadConfig reads it back, and the lookup it
// builds is the exact value run() hands to newWorkflowControlStore. The
// store then serves the manifest-declared budget for the manifest's kind,
// and the operator override wins when set.
func TestProductionWiringPassesManifestQuotaToStore(t *testing.T) {
	t.Parallel()

	home := installQuotaComponent(t)

	getenv := quotaCoordinatorEnv(home, nil)
	cfg, err := coordinator.LoadConfig(getenv)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if cfg.QuotaLookup == nil {
		t.Fatal("cfg.QuotaLookup = nil, want the manifest-derived lookup run() passes to the store")
	}
	// This is the run() wiring line: whatever LoadConfig built goes into
	// the store unmodified.
	store := newWorkflowControlStore(postgres.SQLDB{}, nil, cfg.QuotaLookup)
	if store.QuotaLookup == nil {
		t.Fatal("store.QuotaLookup = nil, want non-nil (run() must supply the manifest/override lookup)")
	}
	quota, ok := store.QuotaLookup(scope.CollectorGit)
	if !ok {
		t.Fatal("store.QuotaLookup(git) ok = false, want true (installed manifest declares a quota)")
	}
	if quota.MaxGenerationsPerHour != 12 {
		t.Fatalf("store.QuotaLookup(git) limit = %d, want 12 (installed manifest declaration)", quota.MaxGenerationsPerHour)
	}

	overridden, err := coordinator.LoadConfig(quotaCoordinatorEnv(home, map[string]string{
		"ESHU_COMPONENT_QUOTA_MAX_GENERATIONS_PER_HOUR": "5",
	}))
	if err != nil {
		t.Fatalf("LoadConfig() with override error = %v, want nil", err)
	}
	quota, ok = overridden.QuotaLookup(scope.CollectorGit)
	if !ok {
		t.Fatal("overridden lookup(git) ok = false, want true")
	}
	if quota.MaxGenerationsPerHour != 5 {
		t.Fatalf("overridden lookup(git) limit = %d, want 5 (operator override wins per field)", quota.MaxGenerationsPerHour)
	}
}

// installQuotaComponent installs a claims-enabled component whose manifest
// declares spec.quotas.maxGenerationsPerHour into a throwaway registry home.
func installQuotaComponent(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	raw := `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.eshu.examples.quota-wired
  name: Quota wiring fixture
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
    maxGenerationsPerHour: 12
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/examples/quota-wired-collector@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  emittedFacts:
    - kind: dev.eshu.examples.quota-wired.snapshot
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - source_evidence_only:no_graph_truth
  telemetry:
    metricsPrefix: eshu_dp_example_quota_wired_
`
	if err := os.WriteFile(manifestPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	policy := component.Policy{
		Mode:              component.TrustModeAllowlist,
		AllowedIDs:        []string{"dev.eshu.examples.quota-wired"},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "v0.1.0",
	}
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
	configPath := filepath.Join(dir, "quota.yaml")
	if err := os.WriteFile(configPath, []byte("config:\n  source:\n    input: /fixtures/quota.json\n"), 0o600); err != nil {
		t.Fatalf("write activation config: %v", err)
	}
	if _, err := component.NewRegistry(home).Enable("dev.eshu.examples.quota-wired", component.Activation{
		InstanceID:    "quota-wired-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    configPath,
	}); err != nil {
		t.Fatalf("Enable() error = %v, want nil", err)
	}
	return home
}

// quotaCoordinatorEnv mirrors the coordinator test env for a claims-enabled
// dark-mode load: dark needs no static instances, claims enabled admits the
// component activation.
func quotaCoordinatorEnv(home string, overrides map[string]string) func(string) string {
	values := map[string]string{
		"ESHU_WORKFLOW_COORDINATOR_CLAIMS_ENABLED": "true",
		"ESHU_COMPONENT_HOME":                      home,
		"ESHU_COMPONENT_TRUST_MODE":                component.TrustModeAllowlist,
		"ESHU_COMPONENT_ALLOW_IDS":                 "dev.eshu.examples.quota-wired",
		"ESHU_COMPONENT_ALLOW_PUBLISHERS":          "eshu-hq",
		"ESHU_COMPONENT_CORE_VERSION":              "v0.1.0",
	}
	for key, value := range overrides {
		values[key] = value
	}
	return func(key string) string {
		return values[key]
	}
}
