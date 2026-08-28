// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/lokiplanner"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeLokiPlanner struct {
	requests []lokiplanner.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeLokiPlanner) PlanLokiWork(
	_ context.Context,
	request lokiplanner.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesLokiWorkThroughChildPlanner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "loki:loki-primary:schedule:continuous-20260605T180000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorLoki),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "loki-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorLoki,
		CollectorInstanceID: "loki-primary",
		SourceSystem:        string(scope.CollectorLoki),
		ScopeID:             "loki:source:platform-prod",
		AcceptanceUnitID:    "loki:source:platform-prod",
		SourceRunID:         "loki:generation-1",
		GenerationID:        "loki:generation-1",
		FairnessKey:         "loki:loki-primary:loki:source:platform-prod",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeLokiPlanner{run: run, items: []workflow.WorkItem{item}}
	instance := testServiceLokiInstance(now)
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
		Store:       &fakeStore{instances: []workflow.CollectorInstance{instance}},
		LokiPlanner: planner,
		Clock:       func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	wantRequest := lokiplanner.PlanRequest{
		Instance:   instance,
		ObservedAt: now,
		PlanKey:    "continuous-20260605T180000Z",
	}
	if got := planner.requests[0]; !reflect.DeepEqual(got, wantRequest) {
		t.Fatalf("planner request = %#v, want %#v", got, wantRequest)
	}
	if got, want := len(service.Store.(*fakeStore).createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
}

func testServiceLokiInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    "loki-primary",
		CollectorKind: scope.CollectorLoki,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Configuration: `{"targets":[{
			"scope_id":"loki:source:platform-prod",
			"instance_id":"platform-prod",
			"base_url":"https://loki.platform-prod.example.com",
			"enabled":true
		}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
