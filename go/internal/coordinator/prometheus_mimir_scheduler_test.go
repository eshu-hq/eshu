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

func TestPrometheusMimirWorkPlannerPlansOneClaimPerEnabledTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "prometheus-mimir-primary",
		CollectorKind:  scope.CollectorPrometheusMimir,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPrometheusMimirConfigWithTwoEnabledAndOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (PrometheusMimirWorkPlanner{}).PlanPrometheusMimirWork(t.Context(), PrometheusMimirPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanPrometheusMimirWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorPrometheusMimir); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d (disabled target must be skipped)", got, want)
	}

	seenScopes := map[string]struct{}{}
	seenFairness := map[string]struct{}{}
	for _, item := range items {
		if got, want := item.CollectorKind, scope.CollectorPrometheusMimir; got != want {
			t.Fatalf("CollectorKind = %q, want %q", got, want)
		}
		if got, want := item.CollectorInstanceID, instance.InstanceID; got != want {
			t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
		}
		if got, want := item.SourceSystem, string(scope.CollectorPrometheusMimir); got != want {
			t.Fatalf("SourceSystem = %q, want %q", got, want)
		}
		if got, want := item.AcceptanceUnitID, item.ScopeID; got != want {
			t.Fatalf("AcceptanceUnitID = %q, want scope_id %q", got, want)
		}
		seenScopes[item.ScopeID] = struct{}{}
		seenFairness[item.FairnessKey] = struct{}{}
	}
	for _, want := range []string{"prometheus:source:platform-prod", "mimir:source:example-qa"} {
		if _, ok := seenScopes[want]; !ok {
			t.Fatalf("planned scopes = %v, want target %q", seenScopes, want)
		}
	}
	if _, ok := seenScopes["prometheus:source:disabled"]; ok {
		t.Fatalf("planned scopes = %v, must not include disabled target", seenScopes)
	}
	if len(seenFairness) != 2 {
		t.Fatalf("fairness keys = %v, want one distinct key per target", seenFairness)
	}

	if strings.Contains(run.RequestedScopeSet, "PROM_TOKEN") || strings.Contains(run.RequestedScopeSet, "MIMIR_TOKEN") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}
	if !strings.Contains(run.RequestedScopeSet, "platform-prod") || !strings.Contains(run.RequestedScopeSet, "example-qa") {
		t.Fatalf("RequestedScopeSet = %q, want enabled target metadata", run.RequestedScopeSet)
	}
}

func TestPrometheusMimirWorkPlannerScopeIDFilter(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "prometheus-mimir-primary",
		CollectorKind:  scope.CollectorPrometheusMimir,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPrometheusMimirConfigWithTwoEnabledAndOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := (PrometheusMimirWorkPlanner{}).PlanPrometheusMimirWork(t.Context(), PrometheusMimirPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
		ScopeIDs:   []string{"mimir:source:example-qa"},
	})
	if err != nil {
		t.Fatalf("PlanPrometheusMimirWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "mimir:source:example-qa"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
}

func TestPrometheusMimirWorkPlannerEmptyConfigPlansNoWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	for name, config := range map[string]string{
		"empty document": "{}",
		"empty targets":  `{"targets":[]}`,
		"all disabled":   `{"targets":[{"provider":"prometheus","scope_id":"prometheus:source:off","instance_id":"off","base_url":"https://off.example","enabled":false}]}`,
		"blank string":   "",
	} {
		config := config
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			instance := workflow.CollectorInstance{
				InstanceID:     "prometheus-mimir-primary",
				CollectorKind:  scope.CollectorPrometheusMimir,
				Mode:           workflow.CollectorModeContinuous,
				Enabled:        true,
				ClaimsEnabled:  true,
				Configuration:  config,
				LastObservedAt: observedAt,
				CreatedAt:      observedAt,
				UpdatedAt:      observedAt,
			}
			_, items, err := (PrometheusMimirWorkPlanner{}).PlanPrometheusMimirWork(t.Context(), PrometheusMimirPlanRequest{
				Instance:   instance,
				ObservedAt: observedAt,
				PlanKey:    "schedule-20260605T150000Z",
			})
			if err != nil {
				t.Fatalf("PlanPrometheusMimirWork() error = %v, want nil", err)
			}
			if got, want := len(items), 0; got != want {
				t.Fatalf("len(items) = %d, want %d", got, want)
			}
		})
	}
}

func TestPrometheusMimirWorkPlannerRejectsWrongKind(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "prometheus-mimir-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPrometheusMimirConfigWithTwoEnabledAndOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	_, _, err := (PrometheusMimirWorkPlanner{}).PlanPrometheusMimirWork(t.Context(), PrometheusMimirPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err == nil {
		t.Fatal("PlanPrometheusMimirWork() error = nil, want wrong collector_kind error")
	}
}

func TestPrometheusMimirWorkPlannerDeterministicPlanKey(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "prometheus-mimir-primary",
		CollectorKind:  scope.CollectorPrometheusMimir,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testPrometheusMimirConfigWithTwoEnabledAndOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	request := PrometheusMimirPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	}

	firstRun, firstItems, err := (PrometheusMimirWorkPlanner{}).PlanPrometheusMimirWork(t.Context(), request)
	if err != nil {
		t.Fatalf("first PlanPrometheusMimirWork() error = %v, want nil", err)
	}
	secondRun, secondItems, err := (PrometheusMimirWorkPlanner{}).PlanPrometheusMimirWork(t.Context(), request)
	if err != nil {
		t.Fatalf("second PlanPrometheusMimirWork() error = %v, want nil", err)
	}
	if firstRun.RunID != secondRun.RunID {
		t.Fatalf("RunID = %q then %q, want deterministic for same (instance, plan_key)", firstRun.RunID, secondRun.RunID)
	}
	if len(firstItems) != len(secondItems) {
		t.Fatalf("len(items) = %d then %d, want stable", len(firstItems), len(secondItems))
	}
	for i := range firstItems {
		if firstItems[i].WorkItemID != secondItems[i].WorkItemID {
			t.Fatalf("WorkItemID[%d] = %q then %q, want deterministic", i, firstItems[i].WorkItemID, secondItems[i].WorkItemID)
		}
		if firstItems[i].GenerationID != secondItems[i].GenerationID {
			t.Fatalf("GenerationID[%d] = %q then %q, want deterministic", i, firstItems[i].GenerationID, secondItems[i].GenerationID)
		}
	}
}

func testPrometheusMimirConfigWithTwoEnabledAndOneDisabled() string {
	return `{
		"targets": [{
			"provider": "prometheus",
			"scope_id": "prometheus:source:platform-prod",
			"instance_id": "platform-prod",
			"base_url": "https://platform-prod.example",
			"token_env": "PROM_TOKEN",
			"resource_limit": 100,
			"enabled": true
		}, {
			"provider": "mimir",
			"scope_id": "mimir:source:example-qa",
			"instance_id": "example-qa",
			"base_url": "https://example-qa.example",
			"token_env": "MIMIR_TOKEN",
			"tenant_id": "team-a",
			"resource_limit": 100,
			"enabled": true
		}, {
			"provider": "prometheus",
			"scope_id": "prometheus:source:disabled",
			"instance_id": "disabled",
			"base_url": "https://disabled.example",
			"enabled": false
		}]
	}`
}
