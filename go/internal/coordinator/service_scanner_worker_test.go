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

type fakeScannerWorkerPlanner struct {
	requests []ScannerWorkerPlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
}

func (f *fakeScannerWorkerPlanner) PlanScannerWorkerWork(
	_ context.Context,
	request ScannerWorkerPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesScannerWorkerWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 4, 21, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "scanner-worker-sbom",
		CollectorKind:  scope.CollectorScannerWorker,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"analyzer":"sbom_generation","sbom_targets":[{"scope_id":"scanner-worker://repository/repository-corpus","root_path":"/fixtures"}]}`,
		LastObservedAt: now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	run := workflow.Run{
		RunID:              "scanner_worker:scanner-worker-sbom:schedule:continuous-20260604T210000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  `{"targets":[{"scope_id":"scanner-worker://repository/repository-corpus"}]}`,
		RequestedCollector: string(scope.CollectorScannerWorker),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "scanner_worker:scanner-worker-sbom:work-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorScannerWorker,
		CollectorInstanceID: instance.InstanceID,
		SourceSystem:        string(scope.CollectorScannerWorker),
		ScopeID:             "scanner-worker://repository/repository-corpus",
		AcceptanceUnitID:    "scanner-worker://repository/repository-corpus",
		SourceRunID:         "scanner_worker:generation-1",
		GenerationID:        "scanner_worker:generation-1",
		FairnessKey:         "scanner_worker:scanner-worker-sbom:repository",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeScannerWorkerPlanner{run: run, items: []workflow.WorkItem{item}}
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
				CollectorKind: scope.CollectorScannerWorker,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: instance.Configuration,
			}},
		},
		Store:                store,
		ScannerWorkerPlanner: planner,
		Clock:                func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}
