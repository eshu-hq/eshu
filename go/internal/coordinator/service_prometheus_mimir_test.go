// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/prometheusmimir"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakePrometheusMimirPlanner struct {
	requests []prometheusmimir.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakePrometheusMimirPlanner) PlanPrometheusMimirWork(
	_ context.Context,
	request prometheusmimir.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunForwardsExactPrometheusMimirRequestAndAdmitsWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name        string
		bootstrap   bool
		wantPlanKey string
	}{
		{name: "scheduled", wantPlanKey: "continuous-20260605T180000Z"},
		{name: "bootstrap", bootstrap: true, wantPlanKey: "bootstrap"},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			instance := testServicePrometheusMimirInstance(now, test.bootstrap)
			run := workflow.Run{
				RunID:              "prometheus-mimir-run-1",
				TriggerKind:        workflow.TriggerKindSchedule,
				Status:             workflow.RunStatusCollectionPending,
				RequestedScopeSet:  "{}",
				RequestedCollector: string(scope.CollectorPrometheusMimir),
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			item := workflow.WorkItem{
				WorkItemID:          "prometheus-mimir-item-1",
				RunID:               run.RunID,
				CollectorKind:       scope.CollectorPrometheusMimir,
				CollectorInstanceID: instance.InstanceID,
				SourceSystem:        string(scope.CollectorPrometheusMimir),
				ScopeID:             "prometheus:source:platform-prod",
				AcceptanceUnitID:    "prometheus:source:platform-prod",
				SourceRunID:         "prometheus-mimir-generation-1",
				GenerationID:        "prometheus-mimir-generation-1",
				FairnessKey:         "prometheus_mimir:prometheus-mimir-primary:prometheus:source:platform-prod",
				Status:              workflow.WorkItemStatusPending,
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			planner := &fakePrometheusMimirPlanner{run: run, items: []workflow.WorkItem{item}}
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
						Bootstrap:     instance.Bootstrap,
						Configuration: instance.Configuration,
					}},
				},
				Store:                  store,
				PrometheusMimirPlanner: planner,
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
			wantRequest := prometheusmimir.PlanRequest{
				Instance:   instance,
				ObservedAt: now,
				PlanKey:    test.wantPlanKey,
			}
			if got := planner.requests[0]; !reflect.DeepEqual(got, wantRequest) {
				t.Fatalf("planner request = %#v, want %#v", got, wantRequest)
			}
			if planner.requests[0].TriggerKind != "" || planner.requests[0].ScopeIDs != nil {
				t.Fatalf("planner request trigger/scope = %q/%v, want empty/nil", planner.requests[0].TriggerKind, planner.requests[0].ScopeIDs)
			}
			if got, want := len(store.createdRuns), 1; got != want {
				t.Fatalf("created runs = %d, want %d", got, want)
			}
		})
	}
}

func TestSchedulePrometheusMimirWorkSkipsAdmissionForEmptyItems(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	instance := testServicePrometheusMimirInstance(now, false)
	planner := &fakePrometheusMimirPlanner{run: workflow.Run{
		RunID:              "prometheus_mimir:prometheus-mimir-primary:schedule:continuous-20260605T180000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  `{"collector_instance_id":"prometheus-mimir-primary","targets":[]}`,
		RequestedCollector: string(scope.CollectorPrometheusMimir),
		CreatedAt:          now,
		UpdatedAt:          now,
	}}
	store := &fakeStore{}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
		},
		Store:                  store,
		PrometheusMimirPlanner: planner,
	}

	if err := service.schedulePrometheusMimirWork(t.Context(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("schedulePrometheusMimirWork() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got := len(store.createdRuns); got != 0 {
		t.Fatalf("created runs = %d, want 0 for an empty item slice", got)
	}
}

func testServicePrometheusMimirInstance(observedAt time.Time, bootstrap bool) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:    "prometheus-mimir-primary",
		CollectorKind: scope.CollectorPrometheusMimir,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Bootstrap:     bootstrap,
		Configuration: `{"targets":[{
			"provider":"prometheus",
			"scope_id":"prometheus:source:platform-prod",
			"instance_id":"platform-prod",
			"base_url":"https://prometheus.platform-prod.example.com",
			"enabled":true
		}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
