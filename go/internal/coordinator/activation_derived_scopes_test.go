// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/component/extension"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// testVulnerabilityActivationHostScope is the host scope the generic planner
// claims for a vulnerability-intelligence activation (the KEV feed).
const testVulnerabilityActivationHostScope = "vuln-intel://cisa/kev"

// testVulnerabilityActivationConfiguration renders the readback configuration
// of one vulnerability-intelligence activation. componentConfig is the
// activation's opaque config block as JSON, or empty to omit it. hostKind and
// hostSource set host.scope.kind and host.sourceSystem.
func testVulnerabilityActivationConfiguration(componentConfig, hostSource, hostKind string) string {
	raw := `{
		"schema_version":"eshu.component.instance.v1",
		"component_id":"dev.eshu.collectors.vulnerability-intelligence",
		"component_version":"0.1.0",
		"publisher":"eshu-hq",
		"manifest_digest":"sha256:1234",
		"config_handle":"component-config:abcd",
		"host":{"source_system":"` + hostSource + `","scope":{"id":"` + testVulnerabilityActivationHostScope + `","kind":"` + hostKind + `"}},
		"runtime":{"sdk_protocol":"collector-sdk/v1alpha1","adapter":"oci"}`
	if componentConfig != "" {
		raw += `,"component_config":` + componentConfig
	}
	return raw + `}`
}

func testVulnerabilityActivationInstance(now time.Time, configuration string) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "vuln-intel-activation",
		CollectorKind:  scope.CollectorVulnerabilityIntelligence,
		Mode:           workflow.CollectorModeScheduled,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  configuration,
		CreatedAt:      now,
		UpdatedAt:      now,
		LastObservedAt: now,
	}
}

func testVulnerabilityActivationService(
	t *testing.T,
	now time.Time,
	instance workflow.CollectorInstance,
	store *fakeStore,
	reader OwnedPackageTargetReader,
	egressPolicy string,
) Service {
	return Service{
		Config: Config{
			DeploymentMode:        deploymentModeActive,
			ClaimsEnabled:         true,
			ReconcileInterval:     time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, egressPolicy),
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                     store,
		ComponentExtensionPlanner: extension.WorkPlanner{},
		OwnedPackageTargetReader:  reader,
		GovernanceAudit:           &fakeGovernanceAuditAppender{},
		Clock:                     func() time.Time { return now },
	}
}

func runCancelledReconcile(t *testing.T, service Service) error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return service.Run(ctx)
}

// TestServiceRunPlansDerivedOSVScopesForVulnerabilityActivation is the #9311
// RED: an activation whose config block enables derive_from_owned_packages
// must get derived OSV scope work items next to its host (KEV) item. Before
// the fix the generic planner planned the host scope only.
func TestServiceRunPlansDerivedOSVScopesForVulnerabilityActivation(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := testVulnerabilityActivationInstance(now, testVulnerabilityActivationConfiguration(
		`{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":125}}`,
		string(scope.CollectorVulnerabilityIntelligence),
		string(scope.CollectorVulnerabilityIntelligence),
	))
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reader := &fakeOwnedPackageTargetReader{targets: []workflow.OwnedPackageDependencyTarget{{
		Ecosystem: "npm", PackageName: "vite", Version: "5.4.21", Lockfile: true, RepositoryID: "repo-eshu",
	}}}
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	scopes := map[string]workflow.WorkItem{}
	for _, item := range store.enqueuedItems {
		scopes[item.ScopeID] = item
	}
	if _, ok := scopes[testVulnerabilityActivationHostScope]; !ok {
		t.Fatalf("enqueued scopes = %v, want the host KEV scope", keysOf(scopes))
	}
	derived, ok := scopes["vuln-intel://osv/npm/vite?version=5.4.21"]
	if !ok {
		t.Fatalf("enqueued scopes = %v, want derived OSV scope vuln-intel://osv/npm/vite?version=5.4.21", keysOf(scopes))
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want %d (KEV once plus one derived scope)", got, want)
	}
	if got, want := derived.CollectorInstanceID, instance.InstanceID; got != want {
		t.Fatalf("derived CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := derived.SourceSystem, string(scope.CollectorVulnerabilityIntelligence); got != want {
		t.Fatalf("derived SourceSystem = %q, want %q", got, want)
	}
	if derived.GenerationID == "" || derived.GenerationID != derived.SourceRunID {
		t.Fatalf("derived GenerationID = %q SourceRunID = %q, want equal and non-empty", derived.GenerationID, derived.SourceRunID)
	}
	if strings.HasPrefix(derived.WorkItemID, "component-work:") || strings.HasPrefix(derived.GenerationID, "component-generation:") {
		t.Fatalf("derived ids %q / %q collide with the generic component-extension id space", derived.WorkItemID, derived.GenerationID)
	}
	if got, want := len(reader.requests), 1; got != want {
		t.Fatalf("owned target reads = %d, want %d", got, want)
	}
	if got, want := reader.requests[0].Limit, 126; got != want {
		t.Fatalf("owned target read limit = %d, want %d (target_limit 125 plus lookahead)", got, want)
	}
}

// TestDesiredInstancesCarryVulnerabilityActivationConfigBlock is the readback
// half of the #9311 RED: the coordinator must carry a vulnerability
// activation's config block, from the file it already opens for host, as the
// opaque component_config field.
func TestDesiredInstancesCarryVulnerabilityActivationConfigBlock(t *testing.T) {
	t.Parallel()

	path := writeActivationFile(t, `host:
  sourceSystem: vulnerability_intelligence
  scope:
    id: vuln-intel://cisa/kev
    kind: vulnerability_intelligence
config:
  derive_from_owned_packages:
    enabled: true
    sources: [osv]
    ecosystems: [npm, maven]
    target_limit: 1000
`)
	instances, err := desiredInstancesForComponent(
		testActivationReadbackEntry(path),
		testActivationManifest(string(scope.CollectorVulnerabilityIntelligence)),
	)
	if err != nil {
		t.Fatalf("desiredInstancesForComponent() error = %v, want nil", err)
	}
	if got, want := len(instances), 1; got != want {
		t.Fatalf("instances = %d, want %d", got, want)
	}
	want := `"component_config":{"derive_from_owned_packages":{"ecosystems":["npm","maven"],"enabled":true,"sources":["osv"],"target_limit":1000}}`
	if !strings.Contains(instances[0].Configuration, want) {
		t.Fatalf("configuration = %s, want it to contain %s", instances[0].Configuration, want)
	}
}

func keysOf(items map[string]workflow.WorkItem) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return keys
}

func writeActivationFile(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "activation.yaml")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write activation file: %v", err)
	}
	return path
}

func testActivationReadbackEntry(configPath string) component.RegistryReadbackComponent {
	return component.RegistryReadbackComponent{InstalledComponent: component.InstalledComponent{
		ID:             "dev.eshu.collectors.vulnerability-intelligence",
		Version:        "0.1.0",
		ManifestDigest: "sha256:1234",
		Activations: []component.Activation{{
			InstanceID:    "vuln-intel-activation",
			Mode:          "scheduled",
			ClaimsEnabled: true,
			ConfigPath:    configPath,
		}},
	}}
}

func testActivationManifest(collectorKind string) component.Manifest {
	return component.Manifest{
		Metadata: component.Metadata{
			ID:        "dev.eshu.collectors.vulnerability-intelligence",
			Name:      "Vulnerability intelligence collector",
			Publisher: "eshu-hq",
			Version:   "0.1.0",
		},
		Spec: component.Spec{
			CollectorKinds: []string{collectorKind},
			Runtime:        component.RuntimeContract{SDKProtocol: "collector-sdk/v1alpha1", Adapter: "oci"},
		},
	}
}
