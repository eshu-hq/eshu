// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestServiceRunActiveModeAuthorizesJiraFreshnessBeforePlanning(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 31, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name                  string
		configuration         string
		scopeID               string
		wantFailureClass      string
		wantPlannerCalls      int
		wantAdmissionCalls    int
		wantHandedOffTriggers []string
	}{
		{
			name:             "unconfigured scope",
			configuration:    testJiraConfig(),
			scopeID:          "jira:site:unconfigured",
			wantFailureClass: "unauthorized_target",
		},
		{
			name: "malformed configured scope",
			configuration: `{"targets":[{
				"provider":"jira_cloud",
				"scope_id":" ",
				"site_id":"example.atlassian.net",
				"token_env":"JIRA_API_TOKEN",
				"jql":"project = OPS"
			}]}`,
			scopeID:          "jira:site:example",
			wantFailureClass: "unauthorized_target",
		},
		{
			name:                  "configured scope",
			configuration:         testJiraConfig(),
			scopeID:               "jira:site:example",
			wantPlannerCalls:      1,
			wantAdmissionCalls:    1,
			wantHandedOffTriggers: []string{"trigger-jira"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			instance := testServiceJiraInstance(now)
			instance.Configuration = test.configuration
			run, item := jiraFreshnessPlannedWork(now, instance, test.scopeID)
			planner := &fakeJiraPlanner{run: run, items: []workflow.WorkItem{item}}
			store := &jiraAdmissionSpyStore{fakeStore: &fakeStore{
				instances: []workflow.CollectorInstance{instance},
			}}
			triggerStore := &fakeIncidentFreshnessTriggerStore{
				claimed: []webhook.StoredIncidentFreshnessTrigger{
					incidentFreshnessStoredTrigger("trigger-jira", webhook.ProviderJira, test.scopeID, now),
				},
			}
			service := Service{
				Config: Config{
					DeploymentMode:    deploymentModeActive,
					ClaimsEnabled:     true,
					ReconcileInterval: time.Hour,
				},
				Store:                     store,
				JiraPlanner:               planner,
				IncidentFreshnessTriggers: triggerStore,
				Clock:                     func() time.Time { return now },
			}

			if err := service.runIncidentFreshnessHandoff(context.Background()); err != nil {
				t.Fatalf("runIncidentFreshnessHandoff() error = %v, want nil", err)
			}
			if got := len(planner.requests); got != test.wantPlannerCalls {
				t.Fatalf("planner calls = %d, want %d", got, test.wantPlannerCalls)
			}
			if got := store.admissionCalls; got != test.wantAdmissionCalls {
				t.Fatalf("Store admission calls = %d, want %d", got, test.wantAdmissionCalls)
			}
			if got := triggerStore.failedCall(test.wantFailureClass); test.wantFailureClass != "" && !reflect.DeepEqual(got, []string{"trigger-jira"}) {
				t.Fatalf("failed %s = %#v, want trigger-jira", test.wantFailureClass, got)
			}
			if test.wantFailureClass == "" && len(triggerStore.failed) != 0 {
				t.Fatalf("failed = %#v, want none", triggerStore.failed)
			}
			if !reflect.DeepEqual(triggerStore.handedOff, test.wantHandedOffTriggers) {
				t.Fatalf("handedOff = %#v, want %#v", triggerStore.handedOff, test.wantHandedOffTriggers)
			}
		})
	}
}

func jiraFreshnessPlannedWork(
	now time.Time,
	instance workflow.CollectorInstance,
	scopeID string,
) (workflow.Run, workflow.WorkItem) {
	run := workflow.Run{
		RunID:              "jira-freshness-run",
		TriggerKind:        workflow.TriggerKindWebhook,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorJira),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "jira-freshness-item",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorJira,
		CollectorInstanceID: instance.InstanceID,
		SourceSystem:        string(scope.CollectorJira),
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "jira:freshness-generation",
		GenerationID:        "jira:freshness-generation",
		FairnessKey:         "jira:jira-primary:example.atlassian.net",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	return run, item
}
