// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestJiraWorkPlannerPlansOneClaimPerTarget(t *testing.T) {
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

	run, items, err := (JiraWorkPlanner{}).PlanJiraWork(t.Context(), JiraPlanRequest{
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

func TestJiraWorkPlannerPlansWebhookScopeSubset(t *testing.T) {
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

	run, items, err := (JiraWorkPlanner{}).PlanJiraWork(t.Context(), JiraPlanRequest{
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

func TestJiraWorkPlannerScheduledPollingCoversAllTargetsAfterMissedWebhook(t *testing.T) {
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

	run, items, err := (JiraWorkPlanner{}).PlanJiraWork(t.Context(), JiraPlanRequest{
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
