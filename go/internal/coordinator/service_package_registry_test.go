// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakePackageRegistryPlanner struct {
	requests []PackageRegistryPlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakePackageRegistryPlanner) PlanPackageRegistryWork(
	_ context.Context,
	request PackageRegistryPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesPackageRegistryWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 13, 18, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "package_registry:collector-package-registry:schedule:continuous-20260513T183000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorPackageRegistry),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "package-registry-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorPackageRegistry,
		CollectorInstanceID: "collector-package-registry",
		SourceSystem:        string(scope.CollectorPackageRegistry),
		ScopeID:             "package-registry://jfrog/generic/team-api",
		AcceptanceUnitID:    "package-registry://jfrog/generic/team-api",
		SourceRunID:         "package_registry:generation-1",
		GenerationID:        "package_registry:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakePackageRegistryPlanner{run: run, items: []workflow.WorkItem{item}}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServicePackageRegistryInstance(now)},
	}
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-package-registry",
				CollectorKind: scope.CollectorPackageRegistry,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testPackageRegistryConfiguration(),
			}},
		},
		Store:                  store,
		PackageRegistryPlanner: planner,
		Clock:                  func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "continuous-20260513T180000Z"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func TestServiceRunActiveModePassesOwnedPackageEvidenceToPackageRegistryPlanner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 23, 22, 0, 0, 0, time.UTC)
	planner := &fakePackageRegistryPlanner{
		run: workflow.Run{
			RunID:              "package_registry:collector-package-registry:schedule:continuous-20260523T220000Z",
			TriggerKind:        workflow.TriggerKindSchedule,
			Status:             workflow.RunStatusCollectionPending,
			RequestedScopeSet:  "{}",
			RequestedCollector: string(scope.CollectorPackageRegistry),
			CreatedAt:          now,
			UpdatedAt:          now,
		},
	}
	targetReader := &fakeOwnedPackageTargetReader{targets: []workflow.OwnedPackageDependencyTarget{{
		Ecosystem:    "npm",
		PackageName:  "vite",
		Version:      "^5.4.11",
		RepositoryID: "repo-eshu",
	}}}
	instance := testServicePackageRegistryInstance(now)
	instance.Configuration = `{"derive_from_owned_packages":{"enabled":true,"ecosystems":["npm"],"target_limit":125}}`
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    "collector-package-registry",
				CollectorKind: scope.CollectorPackageRegistry,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                    &fakeStore{instances: []workflow.CollectorInstance{instance}},
		PackageRegistryPlanner:   planner,
		OwnedPackageTargetReader: targetReader,
		Clock:                    func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(targetReader.requests), 1; got != want {
		t.Fatalf("target reader requests = %d, want %d", got, want)
	}
	if got, want := targetReader.requests[0].Ecosystems, []string{"npm"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("target reader ecosystems = %#v, want %#v", got, want)
	}
	if got, want := targetReader.requests[0].Limit, 126; got != want {
		t.Fatalf("target reader limit = %d, want %d", got, want)
	}
	if targetReader.requests[0].VersionSpecific {
		t.Fatalf("package-registry target reader requested version-specific rows")
	}
	if got, want := targetReader.requests[0].RotationOffset, derivedTargetRotationOffset(now, time.Hour, 125); got != want {
		t.Fatalf("target reader rotation offset = %d, want %d", got, want)
	}
	if got, want := len(planner.requests[0].OwnedPackageTargets), 1; got != want {
		t.Fatalf("planner owned targets = %d, want %d", got, want)
	}
}

func testServicePackageRegistryInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-package-registry",
		CollectorKind:  scope.CollectorPackageRegistry,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPackageRegistryConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
