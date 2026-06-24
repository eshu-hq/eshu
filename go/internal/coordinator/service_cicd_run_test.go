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

type fakeCICDRunPlanner struct {
	requests []CICDRunPlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeCICDRunPlanner) PlanCICDRunWork(
	_ context.Context,
	request CICDRunPlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesCICDRunWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 7, 15, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "ci_cd_run:collector-ci-cd-run:schedule:continuous-20260607T150000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorCICDRun),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "ci-cd-run-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorCICDRun,
		CollectorInstanceID: "collector-ci-cd-run",
		SourceSystem:        string(scope.CollectorCICDRun),
		ScopeID:             "github-actions://eshu-hq/eshu",
		AcceptanceUnitID:    "github-actions://eshu-hq/eshu",
		SourceRunID:         "ci_cd_run:generation-1",
		GenerationID:        "ci_cd_run:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeCICDRunPlanner{run: run, items: []workflow.WorkItem{item}}
	store := &fakeStore{
		instances: []workflow.CollectorInstance{testServiceCICDRunInstance(now)},
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
				InstanceID:    "collector-ci-cd-run",
				CollectorKind: scope.CollectorCICDRun,
				Mode:          workflow.CollectorModeContinuous,
				Enabled:       true,
				ClaimsEnabled: true,
				Configuration: testServiceCICDRunConfiguration(),
			}},
		},
		Store:          store,
		CICDRunPlanner: planner,
		Clock:          func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "continuous-20260607T150000Z"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func testServiceCICDRunInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "collector-ci-cd-run",
		CollectorKind:  scope.CollectorCICDRun,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServiceCICDRunConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testServiceCICDRunConfiguration() string {
	return `{
		"targets": [{
			"provider": "github_actions",
			"scope_id": "github-actions://eshu-hq/eshu",
			"repository": "eshu-hq/eshu",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["eshu-hq/eshu"],
			"max_runs": 5,
			"max_jobs": 20,
			"max_artifacts": 10
		}]
	}`
}
