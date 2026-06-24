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

func TestTempoWorkPlannerPlansOneClaimPerEnabledTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "tempo-primary",
		CollectorKind:  scope.CollectorTempo,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testTempoConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (TempoWorkPlanner{}).PlanTempoWork(t.Context(), TempoPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanTempoWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorTempo); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	for _, item := range items {
		if got, want := item.CollectorKind, scope.CollectorTempo; got != want {
			t.Fatalf("CollectorKind = %q, want %q", got, want)
		}
		if got, want := item.CollectorInstanceID, "tempo-primary"; got != want {
			t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
		}
	}
	// Distinct per-target fairness/conflict keys partition concurrent claims.
	if items[0].FairnessKey == items[1].FairnessKey {
		t.Fatalf("FairnessKey collision = %q, want distinct per-target keys", items[0].FairnessKey)
	}
	if items[0].ScopeID == items[1].ScopeID {
		t.Fatalf("ScopeID collision = %q, want distinct per-target scopes", items[0].ScopeID)
	}
	for _, want := range []string{"tempo:source:platform-prod", "tempo:source:example-qa"} {
		if !strings.Contains(run.RequestedScopeSet, want) {
			t.Fatalf("RequestedScopeSet = %q, want target %q", run.RequestedScopeSet, want)
		}
	}
	if strings.Contains(run.RequestedScopeSet, "TEMPO_API_TOKEN") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}
}

func TestTempoWorkPlannerSkipsDisabledTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "tempo-primary",
		CollectorKind:  scope.CollectorTempo,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testTempoConfigWithOneDisabledTarget(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (TempoWorkPlanner{}).PlanTempoWork(t.Context(), TempoPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanTempoWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "tempo:source:platform-prod"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if strings.Contains(run.RequestedScopeSet, "tempo:source:example-qa") {
		t.Fatalf("RequestedScopeSet = %q, must not include disabled targets", run.RequestedScopeSet)
	}
}

func TestTempoWorkPlannerEmptyConfigPlansNoWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "tempo-primary",
		CollectorKind:  scope.CollectorTempo,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := (TempoWorkPlanner{}).PlanTempoWork(t.Context(), TempoPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanTempoWork() error = %v, want nil", err)
	}
	if got, want := len(items), 0; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
}

func TestTempoWorkPlannerRejectsWrongCollectorKind(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "tempo-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testTempoConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if _, _, err := (TempoWorkPlanner{}).PlanTempoWork(t.Context(), TempoPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	}); err == nil {
		t.Fatal("PlanTempoWork() error = nil, want wrong collector_kind rejection")
	}
}

func TestTempoWorkPlannerPlanKeyDeterministic(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "tempo-primary",
		CollectorKind:  scope.CollectorTempo,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testTempoConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	request := TempoPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	}

	firstRun, firstItems, err := (TempoWorkPlanner{}).PlanTempoWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanTempoWork() error = %v, want nil", err)
	}
	secondRun, secondItems, err := (TempoWorkPlanner{}).PlanTempoWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanTempoWork() error = %v, want nil", err)
	}
	if firstRun.RunID != secondRun.RunID {
		t.Fatalf("RunID = %q and %q, want identical for repeated reconcile", firstRun.RunID, secondRun.RunID)
	}
	if len(firstItems) != len(secondItems) {
		t.Fatalf("len mismatch %d vs %d", len(firstItems), len(secondItems))
	}
	for i := range firstItems {
		if firstItems[i].WorkItemID != secondItems[i].WorkItemID {
			t.Fatalf("WorkItemID[%d] = %q and %q, want idempotent ID", i, firstItems[i].WorkItemID, secondItems[i].WorkItemID)
		}
		if firstItems[i].GenerationID != secondItems[i].GenerationID {
			t.Fatalf("GenerationID[%d] = %q and %q, want idempotent generation", i, firstItems[i].GenerationID, secondItems[i].GenerationID)
		}
	}
}

func testTempoConfigWithTwoEnabledTargets() string {
	return `{
		"targets": [{
			"scope_id": "tempo:source:platform-prod",
			"instance_id": "platform-prod",
			"base_url": "https://tempo.platform-prod.example.com",
			"token_env": "TEMPO_API_TOKEN",
			"enabled": true
		}, {
			"scope_id": "tempo:source:example-qa",
			"instance_id": "example-qa",
			"base_url": "https://tempo.example-qa.example.com",
			"token_env": "TEMPO_API_TOKEN",
			"enabled": true
		}]
	}`
}

func testTempoConfigWithOneDisabledTarget() string {
	return `{
		"targets": [{
			"scope_id": "tempo:source:platform-prod",
			"instance_id": "platform-prod",
			"base_url": "https://tempo.platform-prod.example.com",
			"token_env": "TEMPO_API_TOKEN",
			"enabled": true
		}, {
			"scope_id": "tempo:source:example-qa",
			"instance_id": "example-qa",
			"base_url": "https://tempo.example-qa.example.com",
			"token_env": "TEMPO_API_TOKEN",
			"enabled": false
		}]
	}`
}
