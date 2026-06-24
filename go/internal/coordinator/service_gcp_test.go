// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeGCPPlanner struct {
	requests []GCPPlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeGCPPlanner) PlanGCPWork(
	_ context.Context,
	request GCPPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesGCPWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 15, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "gcp:gcp-primary:schedule:continuous-20260618T150000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorGCP),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "gcp-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGCP,
		CollectorInstanceID: "gcp-primary",
		SourceSystem:        string(scope.CollectorGCP),
		ScopeID:             "gcp:project:project-alpha:compute:resource:global",
		AcceptanceUnitID:    "gcp:project:project-alpha:compute:resource:global",
		SourceRunID:         "gcp:generation-1",
		GenerationID:        "gcp:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeGCPPlanner{run: run, items: []workflow.WorkItem{item}}
	instance := testServiceGCPInstance(now)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
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
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:      store,
		GCPPlanner: planner,
		Clock:      func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "continuous-20260618T150000Z"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func TestServiceRunActiveModeSkipsGCPWorkWhenPriorTargetIsOpen(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 16, 0, 0, 0, time.UTC)
	instance := testServiceGCPInstance(now)
	openItem := workflow.WorkItem{
		WorkItemID:          "open-gcp-item",
		RunID:               "prior-gcp-run",
		CollectorKind:       scope.CollectorGCP,
		CollectorInstanceID: "gcp-primary",
		SourceSystem:        string(scope.CollectorGCP),
		ScopeID:             "gcp:project:project-alpha:compute:resource:global",
		AcceptanceUnitID:    "gcp:project:project-alpha:compute:resource:global",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now.Add(-time.Minute),
		UpdatedAt:           now.Add(-time.Minute),
	}
	openItemBeta := openItem
	openItemBeta.WorkItemID = "open-gcp-item-beta"
	openItemBeta.ScopeID = "gcp:project:project-beta:storage:iam_policy:us"
	openItemBeta.AcceptanceUnitID = openItemBeta.ScopeID

	store := &fakeStore{
		instances:     []workflow.CollectorInstance{instance},
		enqueuedItems: []workflow.WorkItem{openItem, openItemBeta},
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
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:      store,
		GCPPlanner: GCPWorkPlanner{},
		Clock:      func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(store.createdRuns); got != 0 {
		t.Fatalf("created runs = %d, want 0 when all GCP targets are already open", got)
	}
	if got, want := len(store.enqueuedItems), 2; got != want {
		t.Fatalf("enqueued items = %d, want existing %d", got, want)
	}
}

func TestServiceRunActiveModeFiltersDeniedGCPTenantScopes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 16, 15, 0, 0, time.UTC)
	instance := testServiceGCPInstance(now)
	authorizedScope := "gcp:project:project-alpha:compute:resource:global"
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
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
			TenantBoundary: WorkflowTenantBoundary{
				TenantID:           "tenant-a",
				WorkspaceID:        "workspace-a",
				SubjectClass:       "collector",
				PolicyRevisionHash: "policy-a",
			},
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:      store,
		GCPPlanner: GCPWorkPlanner{},
		TenantGrantReader: &fakeTenantGrantReader{
			grants: []WorkflowTenantScopeGrant{{
				ScopeID:            authorizedScope,
				PolicyRevisionHash: "policy-a",
			}},
		},
		Clock: func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
	if got, want := store.enqueuedItems[0].ScopeID, authorizedScope; got != want {
		t.Fatalf("ScopeID = %q, want authorized scope %q", got, want)
	}
	if got := store.createdRuns[0].RequestedScopeSet; strings.Contains(got, "project-beta") {
		t.Fatalf("RequestedScopeSet leaked denied GCP scope: %s", got)
	}
	if got := store.createdRuns[0].RequestedScopeSet; !strings.Contains(got, "project-alpha") {
		t.Fatalf("RequestedScopeSet missing authorized GCP scope: %s", got)
	}
}

func testServiceGCPInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "gcp-primary",
		CollectorKind:  scope.CollectorGCP,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGCPConfigWithTwoEnabledScopes(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
