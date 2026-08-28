// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/grafanaplanner"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeGrafanaPlanner struct {
	requests []grafanaplanner.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

type grafanaAdmissionSpyStore struct {
	*fakeStore
	admissionCalls int
}

func (s *grafanaAdmissionSpyStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (workflow.RunAdmission, error) {
	s.admissionCalls++
	return s.fakeStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
}

func (f *fakeGrafanaPlanner) PlanGrafanaWork(
	_ context.Context,
	request grafanaplanner.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunForwardsExactGrafanaRequestAndAdmitsWork(t *testing.T) {
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
			instance := testServiceGrafanaInstance(now, test.bootstrap)
			run := workflow.Run{
				RunID:              "grafana-run-1",
				TriggerKind:        workflow.TriggerKindSchedule,
				Status:             workflow.RunStatusCollectionPending,
				RequestedScopeSet:  "{}",
				RequestedCollector: string(scope.CollectorGrafana),
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			item := workflow.WorkItem{
				WorkItemID:          "grafana-item-1",
				RunID:               run.RunID,
				CollectorKind:       scope.CollectorGrafana,
				CollectorInstanceID: instance.InstanceID,
				SourceSystem:        string(scope.CollectorGrafana),
				ScopeID:             "grafana:instance:platform-prod",
				AcceptanceUnitID:    "grafana:instance:platform-prod",
				SourceRunID:         "grafana-generation-1",
				GenerationID:        "grafana-generation-1",
				FairnessKey:         "grafana:grafana-primary:platform-prod",
				Status:              workflow.WorkItemStatusPending,
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			planner := &fakeGrafanaPlanner{run: run, items: []workflow.WorkItem{item}}
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
				Store:          store,
				GrafanaPlanner: planner,
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
			wantRequest := grafanaplanner.PlanRequest{
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

func TestScheduleGrafanaWorkSkipsAdmissionForEmptyItems(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	instance := testServiceGrafanaInstance(now, false)
	planner := &fakeGrafanaPlanner{run: workflow.Run{
		RunID:              "grafana:grafana-primary:schedule:continuous-20260605T180000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  `{"collector_instance_id":"grafana-primary","targets":[]}`,
		RequestedCollector: string(scope.CollectorGrafana),
		CreatedAt:          now,
		UpdatedAt:          now,
	}}
	store := &grafanaAdmissionSpyStore{fakeStore: &fakeStore{}}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
		},
		Store:          store,
		GrafanaPlanner: planner,
	}

	if err := service.scheduleGrafanaWork(t.Context(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleGrafanaWork() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got := store.admissionCalls; got != 0 {
		t.Fatalf("admission calls = %d, want 0 for an empty item slice", got)
	}
	if got := len(store.createdRuns); got != 0 {
		t.Fatalf("created runs = %d, want 0 for an empty item slice", got)
	}
}

func testServiceGrafanaInstance(observedAt time.Time, bootstrap bool) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorGrafana,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Bootstrap:      bootstrap,
		Configuration:  `{"targets":[{"provider":"grafana","scope_id":"grafana:instance:platform-prod","instance_id":"platform-prod","enabled":true}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
