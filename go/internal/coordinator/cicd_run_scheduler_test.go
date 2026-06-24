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

func TestCICDRunWorkPlannerPlansOneClaimPerConfiguredRepository(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 7, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "ci-cd-primary",
		CollectorKind:  scope.CollectorCICDRun,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testCICDRunPlannerConfig(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (CICDRunWorkPlanner{}).PlanCICDRunWork(t.Context(), CICDRunPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260607T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanCICDRunWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorCICDRun); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	item := items[0]
	if got, want := item.CollectorKind, scope.CollectorCICDRun; got != want {
		t.Fatalf("CollectorKind = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "ci-cd:github-actions:example/repo"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if !strings.Contains(run.RequestedScopeSet, `"provider":"github_actions"`) {
		t.Fatalf("RequestedScopeSet = %q, want provider metadata", run.RequestedScopeSet)
	}
	if strings.Contains(run.RequestedScopeSet, "GITHUB_TOKEN") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}
}

func testCICDRunPlannerConfig() string {
	return `{
		"targets": [{
			"provider": "github_actions",
			"scope_id": "ci-cd:github-actions:example/repo",
			"repository": "example/repo",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["example/repo"],
			"max_runs": 1,
			"max_jobs": 100,
			"max_artifacts": 100
		}]
	}`
}
