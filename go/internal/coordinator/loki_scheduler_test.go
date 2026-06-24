package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestLokiWorkPlannerPlansOneClaimPerEnabledTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "loki-primary",
		CollectorKind:  scope.CollectorLoki,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testLokiConfigTwoEnabledOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (LokiWorkPlanner{}).PlanLokiWork(t.Context(), LokiPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanLokiWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorLoki); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	// Only the two enabled targets are planned; the disabled target is skipped.
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	for _, want := range []string{"loki:source:platform-prod", "loki:source:example-qa"} {
		if !strings.Contains(run.RequestedScopeSet, want) {
			t.Fatalf("RequestedScopeSet = %q, want enabled target %q", run.RequestedScopeSet, want)
		}
	}
	if strings.Contains(run.RequestedScopeSet, "loki:source:disabled") {
		t.Fatalf("RequestedScopeSet = %q, must not include disabled target", run.RequestedScopeSet)
	}
	if strings.Contains(run.RequestedScopeSet, "LOKI_TOKEN") {
		t.Fatalf("RequestedScopeSet = %q, must not expose credential env names", run.RequestedScopeSet)
	}

	seenFairness := make(map[string]struct{}, len(items))
	for _, item := range items {
		if got, want := item.CollectorKind, scope.CollectorLoki; got != want {
			t.Fatalf("CollectorKind = %q, want %q", got, want)
		}
		if item.ScopeID == "" {
			t.Fatalf("ScopeID must not be blank")
		}
		if got, want := item.AcceptanceUnitID, item.ScopeID; got != want {
			t.Fatalf("AcceptanceUnitID = %q, want %q", got, want)
		}
		if _, dup := seenFairness[item.FairnessKey]; dup {
			t.Fatalf("FairnessKey %q reused; targets must partition by distinct conflict key", item.FairnessKey)
		}
		seenFairness[item.FairnessKey] = struct{}{}
		if err := item.Validate(); err != nil {
			t.Fatalf("WorkItem.Validate() error = %v", err)
		}
	}
}

func TestLokiWorkPlannerPlanKeyIsIdempotent(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 16, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "loki-primary",
		CollectorKind:  scope.CollectorLoki,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testLokiConfigTwoEnabledOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	request := LokiPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T160000Z",
	}

	runA, itemsA, err := (LokiWorkPlanner{}).PlanLokiWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanLokiWork() first call error = %v", err)
	}
	runB, itemsB, err := (LokiWorkPlanner{}).PlanLokiWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanLokiWork() second call error = %v", err)
	}
	if runA.RunID != runB.RunID {
		t.Fatalf("RunID not stable: %q vs %q", runA.RunID, runB.RunID)
	}
	if len(itemsA) != len(itemsB) {
		t.Fatalf("item count not stable: %d vs %d", len(itemsA), len(itemsB))
	}
	for i := range itemsA {
		if itemsA[i].WorkItemID != itemsB[i].WorkItemID {
			t.Fatalf("WorkItemID[%d] not stable: %q vs %q", i, itemsA[i].WorkItemID, itemsB[i].WorkItemID)
		}
		if itemsA[i].GenerationID != itemsB[i].GenerationID {
			t.Fatalf("GenerationID[%d] not stable: %q vs %q", i, itemsA[i].GenerationID, itemsB[i].GenerationID)
		}
	}
}

func TestLokiWorkPlannerRejectsWrongCollectorKind(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "loki-primary",
		CollectorKind:  scope.CollectorJira,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testLokiConfigTwoEnabledOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	if _, _, err := (LokiWorkPlanner{}).PlanLokiWork(t.Context(), LokiPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	}); err == nil {
		t.Fatal("PlanLokiWork() error = nil, want wrong collector_kind rejection")
	}
}

func TestLokiWorkPlannerEmptyConfigPlansNoWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "loki-primary",
		CollectorKind:  scope.CollectorLoki,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (LokiWorkPlanner{}).PlanLokiWork(t.Context(), LokiPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanLokiWork() error = %v, want nil", err)
	}
	if got := len(items); got != 0 {
		t.Fatalf("len(items) = %d, want 0", got)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorLoki); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
}

func TestLokiWorkPlannerAllDisabledPlansNoWork(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "loki-primary",
		CollectorKind:  scope.CollectorLoki,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"scope_id":"loki:source:disabled","base_url":"https://loki.example","enabled":false}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	_, items, err := (LokiWorkPlanner{}).PlanLokiWork(t.Context(), LokiPlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "schedule-20260605T150000Z",
	})
	if err != nil {
		t.Fatalf("PlanLokiWork() error = %v, want nil", err)
	}
	if got := len(items); got != 0 {
		t.Fatalf("len(items) = %d, want 0 for all-disabled targets", got)
	}
}

func TestLokiWorkPlannerScopeIDsFilterAndTriggerOverride(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 15, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "loki-primary",
		CollectorKind:  scope.CollectorLoki,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  testLokiConfigTwoEnabledOneDisabled(),
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (LokiWorkPlanner{}).PlanLokiWork(t.Context(), LokiPlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     "schedule-20260605T150000Z",
		TriggerKind: workflow.TriggerKindBootstrap,
		ScopeIDs:    []string{"loki:source:example-qa"},
	})
	if err != nil {
		t.Fatalf("PlanLokiWork() error = %v, want nil", err)
	}
	// The explicit trigger override wins over the derived schedule trigger.
	if got, want := run.TriggerKind, workflow.TriggerKindBootstrap; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	// Only the requested (enabled) scope is planned.
	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	if got, want := items[0].ScopeID, "loki:source:example-qa"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
}

func testLokiConfigTwoEnabledOneDisabled() string {
	return `{
		"targets": [{
			"scope_id": "loki:source:platform-prod",
			"instance_id": "platform-prod",
			"base_url": "https://loki.platform-prod.example",
			"token_env": "LOKI_TOKEN",
			"enabled": true
		}, {
			"scope_id": "loki:source:example-qa",
			"instance_id": "example-qa",
			"base_url": "https://loki.example-qa.example",
			"token_env": "LOKI_TOKEN",
			"enabled": true
		}, {
			"scope_id": "loki:source:disabled",
			"instance_id": "disabled",
			"base_url": "https://loki.disabled.example",
			"token_env": "LOKI_TOKEN",
			"enabled": false
		}]
	}`
}
