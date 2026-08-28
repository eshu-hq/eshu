// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prometheusmimir

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesOrderingIdentityAndPrivacyContract(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("EDT", -4*60*60)
	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, location)
	instance := workflow.CollectorInstance{
		InstanceID:    "prometheus-mimir-primary",
		CollectorKind: scope.CollectorPrometheusMimir,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Bootstrap:     true,
		Configuration: `{
			"targets": [{
				"provider": "prometheus",
				"scope_id": "prometheus:source:zeta",
				"instance_id": "zeta",
				"base_url": "https://prometheus.zeta.example.com",
				"token_env": "PROMETHEUS_ZETA_TOKEN",
				"resource_limit": 100,
				"enabled": true
			}, {
				"provider": "mimir",
				"scope_id": "mimir:source:alpha",
				"instance_id": "alpha",
				"base_url": "https://mimir.alpha.example.com",
				"token_env": "MIMIR_ALPHA_TOKEN",
				"tenant_id": "team-alpha",
				"tenant_id_env": "MIMIR_ALPHA_TENANT",
				"resource_limit": 200,
				"enabled": true
			}]
		}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	planKey := "replay-20260605T183000-0400"

	run, items, err := (WorkPlanner{}).PlanPrometheusMimirWork(t.Context(), PlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     planKey,
		TriggerKind: workflow.TriggerKindReplay,
		ScopeIDs:    []string{"", " mimir:source:alpha ", "prometheus:source:zeta", "  mimir:source:alpha  "},
	})
	if err != nil {
		t.Fatalf("PlanPrometheusMimirWork() error = %v, want nil", err)
	}

	if got, want := run.RunID, "prometheus_mimir:prometheus-mimir-primary:replay:"+planKey; got != want {
		t.Fatalf("RunID = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindReplay; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := run.Status, workflow.RunStatusCollectionPending; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorPrometheusMimir); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
	if got, want := run.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("UpdatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
	if got, want := run.RequestedScopeSet, `{"collector_instance_id":"prometheus-mimir-primary","targets":[{"scope_id":"mimir:source:alpha","provider":"mimir","instance_id":"alpha","tenant_id":"team-alpha"},{"scope_id":"prometheus:source:zeta","provider":"prometheus","instance_id":"zeta"}]}`; got != want {
		t.Fatalf("RequestedScopeSet = %q, want %q", got, want)
	}

	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	for i, wantScopeID := range []string{"prometheus:source:zeta", "mimir:source:alpha"} {
		item := items[i]
		expectedGenerationID := "prometheus_mimir:" + facts.StableID("PrometheusMimirWorkflowGeneration", map[string]any{
			"instance_id": "prometheus-mimir-primary",
			"plan_key":    planKey,
			"scope_id":    wantScopeID,
		})
		if got, want := item.WorkItemID, "prometheus_mimir:prometheus-mimir-primary:"+expectedGenerationID; got != want {
			t.Fatalf("items[%d].WorkItemID = %q, want %q", i, got, want)
		}
		if got, want := item.RunID, run.RunID; got != want {
			t.Fatalf("items[%d].RunID = %q, want %q", i, got, want)
		}
		if got, want := item.CollectorKind, scope.CollectorPrometheusMimir; got != want {
			t.Fatalf("items[%d].CollectorKind = %q, want %q", i, got, want)
		}
		if got, want := item.CollectorInstanceID, instance.InstanceID; got != want {
			t.Fatalf("items[%d].CollectorInstanceID = %q, want %q", i, got, want)
		}
		if got, want := item.SourceSystem, string(scope.CollectorPrometheusMimir); got != want {
			t.Fatalf("items[%d].SourceSystem = %q, want %q", i, got, want)
		}
		if got, want := item.GenerationID, expectedGenerationID; got != want {
			t.Fatalf("items[%d].GenerationID = %q, want %q", i, got, want)
		}
		if got, want := item.SourceRunID, expectedGenerationID; got != want {
			t.Fatalf("items[%d].SourceRunID = %q, want %q", i, got, want)
		}
		if got, want := item.FairnessKey, "prometheus_mimir:prometheus-mimir-primary:"+wantScopeID; got != want {
			t.Fatalf("items[%d].FairnessKey = %q, want %q", i, got, want)
		}
		if got := item.ScopeID; got != wantScopeID {
			t.Fatalf("items[%d].ScopeID = %q, want %q", i, got, wantScopeID)
		}
		if got, want := item.AcceptanceUnitID, wantScopeID; got != want {
			t.Fatalf("items[%d].AcceptanceUnitID = %q, want %q", i, got, want)
		}
		if got, want := item.Status, workflow.WorkItemStatusPending; got != want {
			t.Fatalf("items[%d].Status = %q, want %q", i, got, want)
		}
		if got, want := item.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
			t.Fatalf("items[%d].CreatedAt = %v (%v), want %v (UTC)", i, got, got.Location(), want)
		}
		if got, want := item.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
			t.Fatalf("items[%d].UpdatedAt = %v (%v), want %v (UTC)", i, got, got.Location(), want)
		}
	}
}

func TestWorkPlannerDerivesTriggerKindWhenRequestOmitsIt(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, time.UTC)
	tests := []struct {
		name        string
		bootstrap   bool
		planKey     string
		wantTrigger workflow.TriggerKind
		wantRunID   string
	}{
		{
			name:        "schedule",
			planKey:     "continuous-20260605T180000Z",
			wantTrigger: workflow.TriggerKindSchedule,
			wantRunID:   "prometheus_mimir:prometheus-mimir-primary:schedule:continuous-20260605T180000Z",
		},
		{
			name:        "bootstrap",
			bootstrap:   true,
			planKey:     "bootstrap",
			wantTrigger: workflow.TriggerKindBootstrap,
			wantRunID:   "prometheus_mimir:prometheus-mimir-primary:bootstrap:bootstrap",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := validEdgeRequest(observedAt, `{"targets":[]}`)
			request.Instance.Bootstrap = test.bootstrap
			request.PlanKey = test.planKey
			request.TriggerKind = ""

			run, _, err := (WorkPlanner{}).PlanPrometheusMimirWork(t.Context(), request)
			if err != nil {
				t.Fatalf("PlanPrometheusMimirWork() error = %v, want nil", err)
			}
			if got := run.TriggerKind; got != test.wantTrigger {
				t.Fatalf("TriggerKind = %q, want %q", got, test.wantTrigger)
			}
			if got := run.RunID; got != test.wantRunID {
				t.Fatalf("RunID = %q, want %q", got, test.wantRunID)
			}
		})
	}
}
