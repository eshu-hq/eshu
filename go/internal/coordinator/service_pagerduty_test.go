// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/pagerdutyplanner"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakePagerDutyPlanner struct {
	requests []pagerdutyplanner.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

type pagerDutyAdmissionSpyStore struct {
	*fakeStore
	admissionCalls int
}

func (s *pagerDutyAdmissionSpyStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (workflow.RunAdmission, error) {
	s.admissionCalls++
	return s.fakeStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
}

func (f *fakePagerDutyPlanner) PlanPagerDutyWork(
	_ context.Context,
	request pagerdutyplanner.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesPagerDutyWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name        string
		bootstrap   bool
		wantPlanKey string
	}{
		{name: "scheduled", wantPlanKey: "continuous-20260531T180000Z"},
		{name: "bootstrap", bootstrap: true, wantPlanKey: "bootstrap"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			instance := testServicePagerDutyInstance(now)
			instance.Bootstrap = test.bootstrap
			run := workflow.Run{
				RunID:              "pagerduty-run-1",
				TriggerKind:        workflow.TriggerKindSchedule,
				Status:             workflow.RunStatusCollectionPending,
				RequestedScopeSet:  "{}",
				RequestedCollector: string(scope.CollectorPagerDuty),
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			item := workflow.WorkItem{
				WorkItemID:          "pagerduty-item-1",
				RunID:               run.RunID,
				CollectorKind:       scope.CollectorPagerDuty,
				CollectorInstanceID: instance.InstanceID,
				SourceSystem:        string(scope.CollectorPagerDuty),
				ScopeID:             "pagerduty:account:example",
				AcceptanceUnitID:    "pagerduty:account:example",
				SourceRunID:         "pagerduty:generation-1",
				GenerationID:        "pagerduty:generation-1",
				FairnessKey:         "pagerduty:pagerduty-primary:pagerduty",
				Status:              workflow.WorkItemStatusPending,
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			planner := &fakePagerDutyPlanner{run: run, items: []workflow.WorkItem{item}}
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
				Store:            store,
				PagerDutyPlanner: planner,
				Clock:            func() time.Time { return now },
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			wantRequest := pagerdutyplanner.PlanRequest{
				Instance:   instance,
				ObservedAt: now,
				PlanKey:    test.wantPlanKey,
			}
			if !reflect.DeepEqual(planner.requests, []pagerdutyplanner.PlanRequest{wantRequest}) {
				t.Fatalf("planner requests = %#v, want %#v", planner.requests, []pagerdutyplanner.PlanRequest{wantRequest})
			}
			if got, want := len(store.createdRuns), 1; got != want {
				t.Fatalf("created runs = %d, want %d", got, want)
			}
		})
	}
}

func TestSchedulePagerDutyWorkSkipsAdmissionForEmptyPlan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	instance := testServicePagerDutyInstance(now)
	planner := &fakePagerDutyPlanner{run: workflow.Run{
		RunID:              "pagerduty-empty",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorPagerDuty),
		CreatedAt:          now,
		UpdatedAt:          now,
	}}
	store := &pagerDutyAdmissionSpyStore{fakeStore: &fakeStore{instances: []workflow.CollectorInstance{instance}}}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
		},
		Store:            store,
		PagerDutyPlanner: planner,
	}

	if err := service.schedulePagerDutyWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("schedulePagerDutyWork() error = %v, want nil", err)
	}
	if got := store.admissionCalls; got != 0 {
		t.Fatalf("Store admission calls = %d, want 0", got)
	}
}

func testServicePagerDutyInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "pagerduty-primary",
		CollectorKind:  scope.CollectorPagerDuty,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServicePagerDutyConfig(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testServicePagerDutyConfig() string {
	return `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:account:example",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"incident_limit": 25,
			"log_entry_limit": 25,
			"change_event_limit": 25
		}]
	}`
}
