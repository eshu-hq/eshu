// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	cicdrun "github.com/eshu-hq/eshu/go/internal/coordinator/cicd/run"
	ociregistry "github.com/eshu-hq/eshu/go/internal/coordinator/oci/registry"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/aws/freshness"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/aws/scheduled"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/gcp"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/grafana"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/jira"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/loki"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/pagerduty"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/prometheus"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/tempo"
	"github.com/eshu-hq/eshu/go/internal/coordinator/planner/tfstate"
	packages "github.com/eshu-hq/eshu/go/internal/coordinator/registry/package"
	"github.com/eshu-hq/eshu/go/internal/coordinator/sbom/attestation"
	scannerworker "github.com/eshu-hq/eshu/go/internal/coordinator/scanner/worker"
	"github.com/eshu-hq/eshu/go/internal/coordinator/security/alert"
	coordinatorvaultlive "github.com/eshu-hq/eshu/go/internal/coordinator/vault/live"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// productionPlannerService wires every in-tree planner the way
// cmd/workflow-coordinator does (the planners that read Postgres get no
// ports, because a component-configured instance must never reach them) and
// the generic component planner behind a recorder.
func productionPlannerService(
	t *testing.T,
	store *fakeStore,
	instances []workflow.CollectorInstance,
	extensionPlanner ComponentExtensionPlanner,
	now time.Time,
) Service {
	t.Helper()

	desired := make([]workflow.DesiredCollectorInstance, 0, len(instances))
	for _, instance := range instances {
		desired = append(desired, workflow.DesiredCollectorInstance{
			InstanceID:    instance.InstanceID,
			CollectorKind: instance.CollectorKind,
			Mode:          instance.Mode,
			Enabled:       instance.Enabled,
			ClaimsEnabled: instance.ClaimsEnabled,
			Configuration: instance.Configuration,
		})
	}
	return Service{
		Config: Config{
			DeploymentMode:        deploymentModeActive,
			ClaimsEnabled:         true,
			ReconcileInterval:     time.Hour,
			ExtensionEgressPolicy: mustParseExtensionEgressPolicy(t, `{"mode":"broad"}`),
			CollectorInstances:    desired,
		},
		Store:                            store,
		TerraformStatePlanner:            tfstate.WorkPlanner{},
		OCIRegistryPlanner:               ociregistry.WorkPlanner{},
		PackageRegistryPlanner:           packages.WorkPlanner{},
		VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
		SBOMAttestationPlanner:           attestation.WorkPlanner{},
		ScannerWorkerPlanner:             scannerworker.WorkPlanner{},
		SecurityAlertPlanner:             alert.WorkPlanner{},
		CICDRunPlanner:                   cicdrun.WorkPlanner{},
		PagerDutyPlanner:                 pagerduty.WorkPlanner{},
		JiraPlanner:                      jira.WorkPlanner{},
		PrometheusMimirPlanner:           prometheus.WorkPlanner{},
		TempoPlanner:                     tempo.WorkPlanner{},
		GCPPlanner:                       gcp.WorkPlanner{},
		GrafanaPlanner:                   grafana.WorkPlanner{},
		LokiPlanner:                      loki.WorkPlanner{},
		VaultLivePlanner:                 coordinatorvaultlive.WorkPlanner{},
		ComponentExtensionPlanner:        extensionPlanner,
		AWSScheduledPlanner:              scheduled.WorkPlanner{},
		AWSFreshnessPlanner:              freshness.WorkPlanner{},
		Clock:                            func() time.Time { return now },
	}
}

func componentConfiguredInstance(kind scope.CollectorKind, now time.Time) workflow.CollectorInstance {
	instance := selectorTestInstance(kind, testComponentActivationConfig)
	instance.InstanceID = "component-" + string(kind)
	instance.LastObservedAt, instance.CreatedAt, instance.UpdatedAt = now, now, now
	return instance
}

// TestRunReconcileComponentConfiguredInstanceOfEveryKindBelongsToGenericPlanner
// is the #8919 reconcile regression: for every collector kind with an in-tree
// scheduler, one enabled, claims-enabled instance configured as an
// eshu.component.instance.v1 document must be planned by the generic component
// planner alone. Before the shared exclusion, the in-tree planner for the kind
// also selected it and failed the whole reconcile, because schedulers stop at
// the first error.
func TestRunReconcileComponentConfiguredInstanceOfEveryKindBelongsToGenericPlanner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	seen := map[scope.CollectorKind]bool{}
	for _, scheduler := range reconcileSchedulers() {
		kind := scheduler.kind
		if kind == "" || seen[kind] {
			continue
		}
		seen[kind] = true
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			instance := componentConfiguredInstance(kind, now)
			store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
			extensionPlanner := &recordingExtensionPlanner{}
			service := productionPlannerService(t, store, []workflow.CollectorInstance{instance}, extensionPlanner, now)

			if err := service.runReconcile(context.Background()); err != nil {
				t.Fatalf("runReconcile() error = %v, want nil", err)
			}
			if want := []string{instance.InstanceID}; !reflect.DeepEqual(extensionPlanner.instanceIDs, want) {
				t.Fatalf("component extension planner instances = %v, want %v", extensionPlanner.instanceIDs, want)
			}
			if got := len(store.enqueuedItems); got != 1 {
				t.Fatalf("enqueued items = %d, want 1 (the generic planner's)", got)
			}
		})
	}
}

// recordingPackageRegistryPlanner delegates to the production package-registry
// planner and records which instances the in-tree scheduler handed it.
type recordingPackageRegistryPlanner struct {
	instanceIDs []string
}

func (r *recordingPackageRegistryPlanner) PlanPackageRegistryWork(
	ctx context.Context,
	request packages.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	r.instanceIDs = append(r.instanceIDs, request.Instance.InstanceID)
	return packages.WorkPlanner{}.PlanPackageRegistryWork(ctx, request)
}

// TestRunReconcilePackageRegistryComponentInstanceBelongsToGenericPlannerOnly
// pins the mixed case for a kind that had no component exclusion before #8919:
// with the in-tree package-registry planner enabled, the in-tree instance goes
// to it and the component-configured instance goes to the generic planner.
func TestRunReconcilePackageRegistryComponentInstanceBelongsToGenericPlannerOnly(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 23, 18, 0, 0, 0, time.UTC)
	component := componentConfiguredInstance(scope.CollectorPackageRegistry, now)
	inTree := testServicePackageRegistryInstance(now)
	inTree.InstanceID = "package-registry-in-tree"
	inTree.Mode = workflow.CollectorModeScheduled
	instances := []workflow.CollectorInstance{component, inTree}
	store := &fakeStore{instances: instances}
	extensionPlanner := &recordingExtensionPlanner{}
	packagePlanner := &recordingPackageRegistryPlanner{}
	service := productionPlannerService(t, store, instances, extensionPlanner, now)
	service.PackageRegistryPlanner = packagePlanner

	if err := service.runReconcile(context.Background()); err != nil {
		t.Fatalf("runReconcile() error = %v, want nil", err)
	}
	if want := []string{inTree.InstanceID}; !reflect.DeepEqual(packagePlanner.instanceIDs, want) {
		t.Fatalf("package registry planner instances = %v, want %v", packagePlanner.instanceIDs, want)
	}
	if want := []string{component.InstanceID}; !reflect.DeepEqual(extensionPlanner.instanceIDs, want) {
		t.Fatalf("component extension planner instances = %v, want %v", extensionPlanner.instanceIDs, want)
	}
}
