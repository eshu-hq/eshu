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
