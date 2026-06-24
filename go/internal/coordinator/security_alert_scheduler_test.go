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

func TestSecurityAlertWorkPlannerPlansOneClaimPerAllowedRepository(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 25, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "security-alert-primary",
		CollectorKind:  scope.CollectorSecurityAlert,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testSecurityAlertConfig(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (SecurityAlertWorkPlanner{}).PlanSecurityAlertWork(t.Context(), SecurityAlertPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260525T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanSecurityAlertWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorSecurityAlert); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.CollectorKind, scope.CollectorSecurityAlert; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "security-alert:github:example-org/example-repo"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if !strings.Contains(run.RequestedScopeSet, `"provider":"github_dependabot"`) {
		t.Fatalf("RequestedScopeSet = %q, want provider metadata", run.RequestedScopeSet)
	}
	if strings.Contains(run.RequestedScopeSet, "GITHUB_TOKEN") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}
}

func testSecurityAlertConfig() string {
	return `{
		"targets": [{
			"provider": "github_dependabot",
			"scope_id": "security-alert:github:example-org/example-repo",
			"repository": "example-org/example-repo",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["example-org/example-repo"],
			"repository_alert_limit": 25,
			"max_pages": 2
		}]
	}`
}
