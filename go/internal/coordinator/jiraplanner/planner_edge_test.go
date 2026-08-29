// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jiraplanner

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPlansOneClaimPerTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
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

	run, items, err := (WorkPlanner{}).PlanJiraWork(t.Context(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260531T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanJiraWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorJira); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.CollectorKind, scope.CollectorJira; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "jira:site:example"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if !strings.Contains(run.RequestedScopeSet, `"provider":"jira_cloud"`) ||
		!strings.Contains(run.RequestedScopeSet, `"site_id":"example.atlassian.net"`) {
		t.Fatalf("RequestedScopeSet = %q, want provider and site metadata", run.RequestedScopeSet)
	}
	if strings.Contains(run.RequestedScopeSet, "JIRA_API_TOKEN") || strings.Contains(run.RequestedScopeSet, "JIRA_EMAIL") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}
}

func TestWorkPlannerPlansWebhookScopeSubset(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "jira-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testJiraConfigWithTwoTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (WorkPlanner{}).PlanJiraWork(t.Context(), PlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     "freshness-20260531T150000Z",
		TriggerKind: workflow.TriggerKindWebhook,
		ScopeIDs:    []string{"jira:site:service-desk"},
	})
	if err != nil {
		t.Fatalf("PlanJiraWork() error = %v, want nil", err)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindWebhook; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "jira:site:service-desk"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if strings.Contains(run.RequestedScopeSet, "jira:site:example") {
		t.Fatalf("RequestedScopeSet = %q, must not include untriggered targets", run.RequestedScopeSet)
	}
}

func TestWorkPlannerScheduledPollingCoversAllTargetsAfterMissedWebhook(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 16, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "jira-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testJiraConfigWithTwoTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (WorkPlanner{}).PlanJiraWork(t.Context(), PlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260531T160000Z",
	})
	if err != nil {
		t.Fatalf("PlanJiraWork() error = %v, want nil", err)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	for _, want := range []string{"jira:site:example", "jira:site:service-desk"} {
		if !strings.Contains(run.RequestedScopeSet, want) {
			t.Fatalf("RequestedScopeSet = %q, want polling target %q", run.RequestedScopeSet, want)
		}
	}
}

func TestWorkPlannerDerivesScheduleAndBootstrapTriggers(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 16, 30, 0, 0, time.UTC)
	const planKey = "recovery-20260531T163000Z"
	tests := []struct {
		name        string
		bootstrap   bool
		wantTrigger workflow.TriggerKind
		wantRunID   string
	}{
		{
			name:        "schedule fallback",
			wantTrigger: workflow.TriggerKindSchedule,
			wantRunID:   "jira:jira-primary:schedule:" + planKey,
		},
		{
			name:        "bootstrap fallback",
			bootstrap:   true,
			wantTrigger: workflow.TriggerKindBootstrap,
			wantRunID:   "jira:jira-primary:bootstrap:" + planKey,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			instance := validJiraInstance(observedAt, testJiraConfig())
			instance.Bootstrap = test.bootstrap
			run, _, err := (WorkPlanner{}).PlanJiraWork(t.Context(), PlanRequest{
				Instance:   instance,
				ObservedAt: observedAt,
				PlanKey:    planKey,
			})
			if err != nil {
				t.Fatalf("PlanJiraWork() error = %v, want nil", err)
			}
			if got := run.TriggerKind; got != test.wantTrigger {
				t.Fatalf("TriggerKind = %q, want %q", got, test.wantTrigger)
			}
			if got := run.RunID; got != test.wantRunID {
				t.Fatalf("RunID = %q, want %q", got, test.wantRunID)
			}
		})
	}
}

func TestHasConfiguredScopeUsesValidatedConfiguredTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		configuration string
		scopeID       string
		want          bool
	}{
		{name: "exact", configuration: testJiraConfig(), scopeID: "jira:site:example", want: true},
		{name: "trimmed", configuration: testJiraConfig(), scopeID: " jira:site:example ", want: true},
		{name: "unconfigured", configuration: testJiraConfig(), scopeID: "jira:site:missing"},
		{name: "blank", configuration: testJiraConfig(), scopeID: " \t "},
		{name: "malformed configuration", configuration: "{", scopeID: "jira:site:example"},
		{
			name:          "semantically invalid matching target",
			configuration: `{"targets":[{"provider":"jira_cloud","scope_id":"jira:site:example","site_id":"example.atlassian.net","jql":"project = OPS"}]}`,
			scopeID:       "jira:site:example",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := HasConfiguredScope(test.configuration, test.scopeID); got != test.want {
				t.Fatalf("HasConfiguredScope() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestWorkPlannerValidatesAllTargetsBeforeScopeFiltering(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 8, 18, 30, 0, 0, time.UTC)
	configuration := `{"targets":[
		{"provider":"jira_cloud","scope_id":"jira:site:selected","site_id":"selected.atlassian.example","token_env":"JIRA_TOKEN","jql":"project = SELECTED"},
		{"provider":"jira_cloud","scope_id":" jira:site:duplicate ","site_id":"duplicate.atlassian.example","token_env":"JIRA_TOKEN","jql":"project = DUPLICATE"},
		{"provider":"jira_cloud","scope_id":"jira:site:duplicate","site_id":"duplicate.atlassian.example","token_env":"JIRA_TOKEN","jql":"project = DUPLICATE"}
	]}`
	_, _, err := (WorkPlanner{}).PlanJiraWork(t.Context(), PlanRequest{
		Instance:   validJiraInstance(observedAt, configuration),
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260608T183000Z",
		ScopeIDs:   []string{"jira:site:selected"},
	})
	if err == nil || !strings.Contains(err.Error(), `duplicate jira target scope_id "jira:site:duplicate"`) {
		t.Fatalf("PlanJiraWork() error = %v, want duplicate unselected target rejection", err)
	}
}

func TestWorkPlannerRejectsInvalidRequestsAndUnselectedTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 8, 18, 30, 0, 0, time.UTC)
	validRequest := PlanRequest{
		Instance:   validJiraInstance(observedAt, testJiraConfig()),
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260608T183000Z",
	}
	tests := []struct {
		name    string
		mutate  func(*PlanRequest)
		wantErr string
	}{
		{name: "invalid instance", mutate: func(request *PlanRequest) { request.Instance.InstanceID = "" }, wantErr: "instance_id"},
		{name: "wrong collector kind", mutate: func(request *PlanRequest) {
			request.Instance.CollectorKind = scope.CollectorGit
			request.Instance.Configuration = "{}"
		}, wantErr: `requires collector_kind "jira"`},
		{name: "disabled instance", mutate: func(request *PlanRequest) { request.Instance.Enabled = false }, wantErr: "requires enabled collector instance"},
		{name: "claims disabled", mutate: func(request *PlanRequest) { request.Instance.ClaimsEnabled = false }, wantErr: "requires claim-enabled collector instance"},
		{name: "zero observation time", mutate: func(request *PlanRequest) { request.ObservedAt = time.Time{} }, wantErr: "observed_at must not be zero"},
		{name: "invalid trigger kind", mutate: func(request *PlanRequest) { request.TriggerKind = workflow.TriggerKind("invalid") }, wantErr: `unknown workflow trigger kind "invalid"`},
		{name: "unsafe plan key", mutate: func(request *PlanRequest) { request.PlanKey = "contains whitespace" }, wantErr: "plan_key"},
		{name: "malformed configuration", mutate: func(request *PlanRequest) { request.Instance.Configuration = "{" }, wantErr: "configuration must be valid JSON"},
		{name: "empty configuration", mutate: func(request *PlanRequest) { request.Instance.Configuration = "" }, wantErr: "requires targets"},
		{
			name: "invalid unselected target",
			mutate: func(request *PlanRequest) {
				request.Instance.Configuration = `{"targets":[
					{"provider":"jira_cloud","scope_id":"jira:site:selected","site_id":"selected.atlassian.example","token_env":"JIRA_TOKEN","jql":"project = SELECTED"},
					{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","jql":"project = INVALID"}
				]}`
				request.ScopeIDs = []string{"jira:site:selected"}
			},
			wantErr: "targets[1]: token_env is required",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validRequest
			test.mutate(&request)
			_, _, err := (WorkPlanner{}).PlanJiraWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanJiraWork() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerRejectsEveryInvalidUnselectedTargetField(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 8, 18, 30, 0, 0, time.UTC)
	const selectedTarget = `{"provider":"jira_cloud","scope_id":"jira:site:selected","site_id":"selected.atlassian.example","token_env":"JIRA_TOKEN","jql":"project = SELECTED"}`
	tests := []struct {
		name    string
		target  string
		wantErr string
	}{
		{name: "provider", target: `{"provider":"server","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID"}`, wantErr: `targets[1]: unsupported jira provider "server"`},
		{name: "scope", target: `{"provider":"jira_cloud","scope_id":" ","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID"}`, wantErr: "targets[1]: scope_id is required"},
		{name: "site", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":" ","token_env":"TOKEN","jql":"project = INVALID"}`, wantErr: "targets[1]: site_id is required"},
		{name: "token", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","jql":"project = INVALID"}`, wantErr: "targets[1]: token_env is required"},
		{name: "JQL", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN"}`, wantErr: "targets[1]: jql or jql_env is required"},
		{name: "two JQL sources", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID","jql_env":"JIRA_JQL"}`, wantErr: "targets[1]: only one of jql or jql_env may be set"},
		{name: "issue limit", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID","issue_limit":101}`, wantErr: "targets[1]: issue_limit must be between 0 and 100"},
		{name: "changelog limit", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID","changelog_limit":-1}`, wantErr: "targets[1]: changelog_limit must be between 0 and 100"},
		{name: "remote link limit", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID","remote_link_limit":101}`, wantErr: "targets[1]: remote_link_limit must be between 0 and 100"},
		{name: "base URL", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID","base_url":"http://invalid.atlassian.example"}`, wantErr: "targets[1]: base_url must use https"},
		{name: "lookback", target: `{"provider":"jira_cloud","scope_id":"jira:site:invalid","site_id":"invalid.atlassian.example","token_env":"TOKEN","jql":"project = INVALID","updated_lookback":"forever"}`, wantErr: "targets[1]: parse updated_lookback"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := PlanRequest{
				Instance:   validJiraInstance(observedAt, `{"targets":[`+selectedTarget+`,`+test.target+`]}`),
				ObservedAt: observedAt,
				PlanKey:    "schedule-20260608T183000Z",
				ScopeIDs:   []string{"jira:site:selected"},
			}
			_, _, err := (WorkPlanner{}).PlanJiraWork(t.Context(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("PlanJiraWork() error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestWorkPlannerReturnsPendingRunForEmptyScopeSelection(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 8, 18, 30, 0, 0, time.UTC)
	for _, scopeIDs := range [][]string{{"jira:site:missing"}, {" ", "\t"}} {
		run, items, err := (WorkPlanner{}).PlanJiraWork(t.Context(), PlanRequest{
			Instance:   validJiraInstance(observedAt, testJiraConfig()),
			ObservedAt: observedAt,
			PlanKey:    "schedule-20260608T183000Z",
			ScopeIDs:   scopeIDs,
		})
		if err != nil {
			t.Fatalf("PlanJiraWork() error = %v, want nil", err)
		}
		wantRun := workflow.Run{
			RunID:              "jira:jira-primary:schedule:schedule-20260608T183000Z",
			TriggerKind:        workflow.TriggerKindSchedule,
			Status:             workflow.RunStatusCollectionPending,
			RequestedScopeSet:  `{"collector_instance_id":"jira-primary","targets":[]}`,
			RequestedCollector: string(scope.CollectorJira),
			CreatedAt:          observedAt,
			UpdatedAt:          observedAt,
			FinishedAt:         time.Time{},
		}
		if !reflect.DeepEqual(run, wantRun) {
			t.Fatalf("run = %#v, want %#v", run, wantRun)
		}
		if len(items) != 0 {
			t.Fatalf("len(items) = %d, want 0", len(items))
		}
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

func testJiraConfigWithTwoTargets() string {
	return `{
		"targets": [{
			"provider": "jira_cloud",
			"scope_id": "jira:site:example",
			"site_id": "example.atlassian.net",
			"base_url": "https://example.atlassian.net",
			"token_env": "JIRA_API_TOKEN",
			"jql": "project = OPS ORDER BY updated ASC",
			"issue_limit": 25,
			"changelog_limit": 25,
			"remote_link_limit": 25
		}, {
			"provider": "jira_cloud",
			"scope_id": "jira:site:service-desk",
			"site_id": "service.atlassian.net",
			"base_url": "https://service.atlassian.net",
			"token_env": "JIRA_API_TOKEN",
			"jql": "project = SVC ORDER BY updated ASC",
			"issue_limit": 25,
			"changelog_limit": 25,
			"remote_link_limit": 25
		}]
	}`
}
