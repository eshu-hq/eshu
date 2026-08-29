// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/jiraplanner"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeJiraPlanner struct {
	requests []jiraplanner.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

type jiraAdmissionSpyStore struct {
	*fakeStore
	admissionCalls int
}

func (s *jiraAdmissionSpyStore) CreateRunWithWorkItemsIfNoOpenTargets(
	ctx context.Context,
	run workflow.Run,
	items []workflow.WorkItem,
) (workflow.RunAdmission, error) {
	s.admissionCalls++
	return s.fakeStore.CreateRunWithWorkItemsIfNoOpenTargets(ctx, run, items)
}

func (f *fakeJiraPlanner) PlanJiraWork(
	_ context.Context,
	request jiraplanner.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesJiraWork(t *testing.T) {
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
			instance := testServiceJiraInstance(now)
			instance.Bootstrap = test.bootstrap
			run := workflow.Run{
				RunID:              "jira-run-1",
				TriggerKind:        workflow.TriggerKindSchedule,
				Status:             workflow.RunStatusCollectionPending,
				RequestedScopeSet:  "{}",
				RequestedCollector: string(scope.CollectorJira),
				CreatedAt:          now,
				UpdatedAt:          now,
			}
			item := workflow.WorkItem{
				WorkItemID:          "jira-item-1",
				RunID:               run.RunID,
				CollectorKind:       scope.CollectorJira,
				CollectorInstanceID: instance.InstanceID,
				SourceSystem:        string(scope.CollectorJira),
				ScopeID:             "jira:site:example",
				AcceptanceUnitID:    "jira:site:example",
				SourceRunID:         "jira:generation-1",
				GenerationID:        "jira:generation-1",
				FairnessKey:         "jira:jira-primary:example.atlassian.net",
				Status:              workflow.WorkItemStatusPending,
				CreatedAt:           now,
				UpdatedAt:           now,
			}
			planner := &fakeJiraPlanner{run: run, items: []workflow.WorkItem{item}}
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
				Store:       store,
				JiraPlanner: planner,
				Clock:       func() time.Time { return now },
			}

			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
			wantRequest := jiraplanner.PlanRequest{
				Instance:   instance,
				ObservedAt: now,
				PlanKey:    test.wantPlanKey,
			}
			if !reflect.DeepEqual(planner.requests, []jiraplanner.PlanRequest{wantRequest}) {
				t.Fatalf("planner requests = %#v, want %#v", planner.requests, []jiraplanner.PlanRequest{wantRequest})
			}
			if got, want := len(store.createdRuns), 1; got != want {
				t.Fatalf("created runs = %d, want %d", got, want)
			}
		})
	}
}

func TestScheduleJiraWorkSkipsAdmissionForEmptyPlan(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	instance := testServiceJiraInstance(now)
	planner := &fakeJiraPlanner{run: workflow.Run{
		RunID:              "jira-empty",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorJira),
		CreatedAt:          now,
		UpdatedAt:          now,
	}}
	store := &jiraAdmissionSpyStore{fakeStore: &fakeStore{instances: []workflow.CollectorInstance{instance}}}
	service := Service{
		Config: Config{
			DeploymentMode:    deploymentModeActive,
			ClaimsEnabled:     true,
			ReconcileInterval: time.Hour,
		},
		Store:       store,
		JiraPlanner: planner,
	}

	if err := service.scheduleJiraWork(context.Background(), now, []workflow.CollectorInstance{instance}); err != nil {
		t.Fatalf("scheduleJiraWork() error = %v, want nil", err)
	}
	if got := store.admissionCalls; got != 0 {
		t.Fatalf("Store admission calls = %d, want 0", got)
	}
}

func testServiceJiraInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "jira-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testJiraConfig(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testJiraConfig() string {
	return `{
		"targets": [{
			"provider": "jira_cloud",
			"scope_id": "jira:site:example",
			"site_id": "example.atlassian.net",
			"base_url": "https://example.atlassian.net",
			"email_env": "JIRA_EMAIL",
			"token_env": "JIRA_API_TOKEN",
			"jql": "project = OPS ORDER BY updated ASC",
			"issue_limit": 25,
			"updated_lookback": "24h",
			"changelog_limit": 25,
			"remote_link_limit": 25
		}]
	}`
}
