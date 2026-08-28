// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lokiplanner

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesOrderingIdentityAndTriggerContract(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("EDT", -4*60*60)
	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, location)
	instance := workflow.CollectorInstance{
		InstanceID:    "loki-primary",
		CollectorKind: scope.CollectorLoki,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Bootstrap:     true,
		Configuration: `{
			"targets": [{
				"scope_id": "loki:source:zeta",
				"instance_id": "zeta",
				"base_url": "https://loki.zeta.example.com",
				"token_env": "LOKI_ZETA_TOKEN",
				"enabled": true
			}, {
				"scope_id": "loki:source:alpha",
				"instance_id": "alpha",
				"base_url": "https://loki.alpha.example.com",
				"token_env": "LOKI_ALPHA_TOKEN",
				"enabled": true
			}]
		}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	planKey := "replay-20260605T183000-0400"

	run, items, err := (WorkPlanner{}).PlanLokiWork(t.Context(), PlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     planKey,
		TriggerKind: workflow.TriggerKindReplay,
		ScopeIDs:    []string{" loki:source:alpha ", "loki:source:zeta"},
	})
	if err != nil {
		t.Fatalf("PlanLokiWork() error = %v, want nil", err)
	}

	if got, want := run.RunID, "loki:loki-primary:replay:"+planKey; got != want {
		t.Fatalf("RunID = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindReplay; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := run.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
	if got, want := run.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("UpdatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
	if got, want := run.RequestedScopeSet, `{"collector_instance_id":"loki-primary","targets":[{"scope_id":"loki:source:alpha","instance_id":"alpha","base_url":"https://loki.alpha.example.com"},{"scope_id":"loki:source:zeta","instance_id":"zeta","base_url":"https://loki.zeta.example.com"}]}`; got != want {
		t.Fatalf("RequestedScopeSet = %q, want %q", got, want)
	}

	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	for i, wantScopeID := range []string{"loki:source:zeta", "loki:source:alpha"} {
		item := items[i]
		expectedGenerationID := "loki:" + facts.StableID("LokiWorkflowGeneration", map[string]any{
			"instance_id": "loki-primary",
			"plan_key":    planKey,
			"scope_id":    wantScopeID,
		})
		if got, want := item.WorkItemID, "loki:loki-primary:"+expectedGenerationID; got != want {
			t.Fatalf("items[%d].WorkItemID = %q, want %q", i, got, want)
		}
		if got, want := item.GenerationID, expectedGenerationID; got != want {
			t.Fatalf("items[%d].GenerationID = %q, want %q", i, got, want)
		}
		if got, want := item.SourceRunID, expectedGenerationID; got != want {
			t.Fatalf("items[%d].SourceRunID = %q, want %q", i, got, want)
		}
		if got, want := item.FairnessKey, "loki:loki-primary:"+wantScopeID; got != want {
			t.Fatalf("items[%d].FairnessKey = %q, want %q", i, got, want)
		}
		if got := item.ScopeID; got != wantScopeID {
			t.Fatalf("items[%d].ScopeID = %q, want %q", i, got, wantScopeID)
		}
		if got, want := item.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
			t.Fatalf("items[%d].CreatedAt = %v (%v), want %v (UTC)", i, got, got.Location(), want)
		}
		if got, want := item.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
			t.Fatalf("items[%d].UpdatedAt = %v (%v), want %v (UTC)", i, got, got.Location(), want)
		}
	}
}
