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

func TestGrafanaWorkPlannerPlansOneClaimPerEnabledTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorGrafana,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGrafanaConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), GrafanaPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorGrafana); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	for _, item := range items {
		if got, want := item.CollectorKind, scope.CollectorGrafana; got != want {
			t.Fatalf("CollectorKind = %q, want %q", got, want)
		}
		if got, want := item.SourceSystem, string(scope.CollectorGrafana); got != want {
			t.Fatalf("SourceSystem = %q, want %q", got, want)
		}
		if item.ScopeID == "" {
			t.Fatalf("ScopeID must not be blank for item %+v", item)
		}
	}
	for _, want := range []string{"grafana:instance:platform-prod", "grafana:instance:platform-stage"} {
		if !strings.Contains(run.RequestedScopeSet, want) {
			t.Fatalf("RequestedScopeSet = %q, want target %q", run.RequestedScopeSet, want)
		}
	}
	if strings.Contains(run.RequestedScopeSet, "GRAFANA_TOKEN") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}
}

func TestGrafanaWorkPlannerDistinctFairnessKeyPerTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorGrafana,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGrafanaConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), GrafanaPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].FairnessKey == items[1].FairnessKey {
		t.Fatalf("FairnessKey must differ per target, got %q for both", items[0].FairnessKey)
	}
	for _, item := range items {
		if !strings.HasPrefix(item.FairnessKey, string(scope.CollectorGrafana)+":grafana-primary:") {
			t.Fatalf("FairnessKey = %q, want per-target conflict key prefixed by collector and instance", item.FairnessKey)
		}
	}
}

func TestGrafanaWorkPlannerSkipsDisabledTargets(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorGrafana,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGrafanaConfigWithDisabledTarget(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), GrafanaPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d (disabled target skipped)", got, want)
	}
	if got, want := items[0].ScopeID, "grafana:instance:platform-prod"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if strings.Contains(run.RequestedScopeSet, "grafana:instance:platform-stage") {
		t.Fatalf("RequestedScopeSet = %q, must not include disabled target", run.RequestedScopeSet)
	}
}

func TestGrafanaWorkPlannerFiltersByScopeIDs(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorGrafana,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGrafanaConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), GrafanaPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
		ScopeIDs:   []string{"grafana:instance:platform-stage"},
	})
	if err != nil {
		t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
	}
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "grafana:instance:platform-stage"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
}

func TestGrafanaWorkPlannerPlanKeyIsDeterministic(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorGrafana,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGrafanaConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	request := GrafanaPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	}

	runA, itemsA, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), request)
	if err != nil {
		t.Fatalf("first PlanGrafanaWork() error = %v", err)
	}
	runB, itemsB, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), request)
	if err != nil {
		t.Fatalf("second PlanGrafanaWork() error = %v", err)
	}
	if runA.RunID != runB.RunID {
		t.Fatalf("RunID not deterministic: %q vs %q", runA.RunID, runB.RunID)
	}
	if len(itemsA) != len(itemsB) {
		t.Fatalf("item count not deterministic: %d vs %d", len(itemsA), len(itemsB))
	}
	for i := range itemsA {
		if itemsA[i].WorkItemID != itemsB[i].WorkItemID {
			t.Fatalf("WorkItemID not deterministic at %d: %q vs %q", i, itemsA[i].WorkItemID, itemsB[i].WorkItemID)
		}
		if itemsA[i].GenerationID != itemsB[i].GenerationID {
			t.Fatalf("GenerationID not deterministic at %d: %q vs %q", i, itemsA[i].GenerationID, itemsB[i].GenerationID)
		}
	}
}

func TestGrafanaWorkPlannerRejectsWrongCollectorKind(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testGrafanaConfigWithTwoEnabledTargets(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if _, _, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), GrafanaPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	}); err == nil {
		t.Fatalf("PlanGrafanaWork() error = nil, want collector_kind rejection")
	}
}

func TestGrafanaWorkPlannerEmptyConfigPlansNoWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "grafana-primary",
		CollectorKind:  scope.CollectorGrafana,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := (GrafanaWorkPlanner{}).PlanGrafanaWork(t.Context(), GrafanaPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0 for empty config", len(items))
	}
}

func testGrafanaConfigWithTwoEnabledTargets() string {
	return `{
		"targets": [{
			"provider": "grafana",
			"scope_id": "grafana:instance:platform-prod",
			"instance_id": "platform-prod",
			"base_url": "https://platform-prod.grafana.net",
			"token_env": "GRAFANA_TOKEN",
			"resource_limit": 200,
			"stale_after": "24h",
			"enabled": true
		}, {
			"provider": "grafana",
			"scope_id": "grafana:instance:platform-stage",
			"instance_id": "platform-stage",
			"base_url": "https://platform-stage.grafana.net",
			"token_env": "GRAFANA_TOKEN",
			"resource_limit": 200,
			"stale_after": "24h",
			"enabled": true
		}]
	}`
}

func testGrafanaConfigWithDisabledTarget() string {
	return `{
		"targets": [{
			"provider": "grafana",
			"scope_id": "grafana:instance:platform-prod",
			"instance_id": "platform-prod",
			"base_url": "https://platform-prod.grafana.net",
			"token_env": "GRAFANA_TOKEN",
			"enabled": true
		}, {
			"provider": "grafana",
			"scope_id": "grafana:instance:platform-stage",
			"instance_id": "platform-stage",
			"base_url": "https://platform-stage.grafana.net",
			"token_env": "GRAFANA_TOKEN",
			"enabled": false
		}]
	}`
}
