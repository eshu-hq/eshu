// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/securityalert"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeSecurityAlertPlanner struct {
	requests []securityalert.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
}

func (f *fakeSecurityAlertPlanner) PlanSecurityAlertWork(
	_ context.Context,
	request securityalert.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesSecurityAlertWork(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 18, 15, 30, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "security_alert:security-alert-primary:schedule:continuous-20260618T150000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorSecurityAlert),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "security-alert-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorSecurityAlert,
		CollectorInstanceID: "security-alert-primary",
		SourceSystem:        string(scope.CollectorSecurityAlert),
		ScopeID:             "security-alert:github:eshu-hq/eshu",
		AcceptanceUnitID:    "security-alert:github:eshu-hq/eshu",
		SourceRunID:         "security_alert:generation-1",
		GenerationID:        "security_alert:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeSecurityAlertPlanner{run: run, items: []workflow.WorkItem{item}}
	instance := testServiceSecurityAlertInstance(now)
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
				Configuration: instance.Configuration,
			}},
		},
		Store:                store,
		SecurityAlertPlanner: planner,
		Clock:                func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	if got, want := planner.requests[0].PlanKey, "continuous-20260618T150000Z"; got != want {
		t.Fatalf("planner PlanKey = %q, want %q", got, want)
	}
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func testServiceSecurityAlertInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "security-alert-primary",
		CollectorKind:  scope.CollectorSecurityAlert,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testServiceSecurityAlertConfiguration(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}

func testServiceSecurityAlertConfiguration() string {
	return `{
		"targets": [{
			"provider": "github_dependabot",
			"scope_id": "security-alert:github:eshu-hq/eshu",
			"repository": "eshu-hq/eshu",
			"token_env": "GITHUB_TOKEN",
			"allowed_repositories": ["eshu-hq/eshu"],
			"repository_alert_limit": 25,
			"max_pages": 2
		}]
	}`
}
