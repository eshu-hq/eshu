// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tempoplanner

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesFilteringTriggerAndOrderContract(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("EDT", -4*60*60)
	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, location)
	instance := workflow.CollectorInstance{
		InstanceID:    "tempo-primary",
		CollectorKind: scope.CollectorTempo,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Bootstrap:     true,
		Configuration: `{
			"targets": [{
				"scope_id": "tempo:source:zeta",
				"instance_id": "zeta",
				"base_url": "https://tempo.zeta.example.com",
				"token_env": "TEMPO_ZETA_TOKEN",
				"enabled": true
			}, {
				"scope_id": "tempo:source:alpha",
				"instance_id": "alpha",
				"base_url": "https://tempo.alpha.example.com",
				"token_env": "TEMPO_ALPHA_TOKEN",
				"enabled": true
			}]
		}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	planKey := "replay-20260605T183000-0400"

	run, items, err := (WorkPlanner{}).PlanTempoWork(t.Context(), PlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     planKey,
		TriggerKind: workflow.TriggerKindReplay,
		ScopeIDs:    []string{" tempo:source:alpha "},
	})
	if err != nil {
		t.Fatalf("PlanTempoWork() error = %v, want nil", err)
	}

	if got, want := run.RunID, "tempo:tempo-primary:replay:"+planKey; got != want {
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
	if got, want := run.RequestedScopeSet, `{"collector_instance_id":"tempo-primary","targets":[{"scope_id":"tempo:source:alpha","instance_id":"alpha","base_url":"https://tempo.alpha.example.com"}]}`; got != want {
		t.Fatalf("RequestedScopeSet = %q, want %q", got, want)
	}

	if got, want := len(items), 1; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	expectedGenerationID := "tempo:" + facts.StableID("TempoWorkflowGeneration", map[string]any{
		"instance_id": "tempo-primary",
		"plan_key":    planKey,
		"scope_id":    "tempo:source:alpha",
	})
	item := items[0]
	if got, want := item.WorkItemID, "tempo:tempo-primary:"+expectedGenerationID; got != want {
		t.Fatalf("WorkItemID = %q, want %q", got, want)
	}
	if got, want := item.GenerationID, expectedGenerationID; got != want {
		t.Fatalf("GenerationID = %q, want %q", got, want)
	}
	if got, want := item.SourceRunID, expectedGenerationID; got != want {
		t.Fatalf("SourceRunID = %q, want %q", got, want)
	}
	if got, want := item.FairnessKey, "tempo:tempo-primary:tempo:source:alpha"; got != want {
		t.Fatalf("FairnessKey = %q, want %q", got, want)
	}
	if got, want := item.ScopeID, "tempo:source:alpha"; got != want {
		t.Fatalf("ScopeID = %q, want %q", got, want)
	}
	if got, want := item.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
	if got, want := item.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("UpdatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
}
