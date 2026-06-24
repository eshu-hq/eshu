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

func TestPagerDutyWorkPlannerPlansOneClaimPerTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 17, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "pagerduty-primary",
		CollectorKind:  scope.CollectorPagerDuty,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPagerDutyConfig(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (PagerDutyWorkPlanner{}).PlanPagerDutyWork(t.Context(), PagerDutyPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260531T170000Z",
	})
	if err != nil {
		t.Fatalf("PlanPagerDutyWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorPagerDuty); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.CollectorKind, scope.CollectorPagerDuty; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "pagerduty:account:example"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if !strings.Contains(run.RequestedScopeSet, `"provider":"pagerduty"`) {
		t.Fatalf("RequestedScopeSet = %q, want provider metadata", run.RequestedScopeSet)
	}
	if strings.Contains(run.RequestedScopeSet, "PAGERDUTY_TOKEN") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}
}

func TestPagerDutyWorkPlannerPlansWebhookScopeSubset(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 31, 17, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "pagerduty-primary",
		CollectorKind:  scope.CollectorPagerDuty,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPagerDutyConfigWithTwoTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (PagerDutyWorkPlanner{}).PlanPagerDutyWork(t.Context(), PagerDutyPlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     "freshness-20260531T170000Z",
		TriggerKind: workflow.TriggerKindWebhook,
		ScopeIDs:    []string{"pagerduty:service:checkout"},
	})
	if err != nil {
		t.Fatalf("PlanPagerDutyWork() error = %v, want nil", err)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindWebhook; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "pagerduty:service:checkout"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if strings.Contains(run.RequestedScopeSet, "pagerduty:account:example") {
		t.Fatalf("RequestedScopeSet = %q, must not include untriggered targets", run.RequestedScopeSet)
	}
}

func testPagerDutyConfig() string {
	return `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:account:example",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"api_base_url": "https://api.pagerduty.com",
			"source_uri": "https://example.pagerduty.com/incidents",
			"incident_limit": 25,
			"incident_lookback": "6h",
			"log_entry_limit": 25,
			"change_event_limit": 25,
			"allowed_service_ids": ["PABC123"]
		}]
	}`
}

func testPagerDutyConfigWithTwoTargets() string {
	return `{
		"targets": [{
			"provider": "pagerduty",
			"scope_id": "pagerduty:account:example",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"incident_limit": 25,
			"log_entry_limit": 25,
			"change_event_limit": 25
		}, {
			"provider": "pagerduty",
			"scope_id": "pagerduty:service:checkout",
			"account_id": "example",
			"token_env": "PAGERDUTY_TOKEN",
			"incident_limit": 25,
			"log_entry_limit": 25,
			"change_event_limit": 25,
			"allowed_service_ids": ["PABC123"]
		}]
	}`
}
