// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package grafanaplanner

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesOrderingIdentityAndPrivacyContract(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("EDT", -4*60*60)
	observedAt := time.Date(2026, time.June, 5, 18, 30, 0, 0, location)
	instance := workflow.CollectorInstance{
		InstanceID:    "grafana-primary",
		CollectorKind: scope.CollectorGrafana,
		Mode:          workflow.CollectorModeContinuous,
		Enabled:       true,
		ClaimsEnabled: true,
		Bootstrap:     true,
		Configuration: `{
			"targets": [{
				"provider": "grafana-cloud",
				"scope_id": "grafana:instance:zeta",
				"instance_id": "zeta-instance",
				"base_url": "https://zeta.grafana.example.com",
				"token_env": "GRAFANA_ZETA_TOKEN",
				"resource_limit": 100,
				"stale_after": "24h",
				"enabled": true
			}, {
				"provider": "grafana-cloud",
				"scope_id": "grafana:instance:alpha",
				"instance_id": "   ",
				"base_url": "https://alpha.grafana.example.com",
				"token_env": "GRAFANA_ALPHA_TOKEN",
				"resource_limit": 200,
				"stale_after": "12h",
				"enabled": true
			}]
		}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	planKey := "replay-20260605T183000-0400"

	run, items, err := (WorkPlanner{}).PlanGrafanaWork(t.Context(), PlanRequest{
		Instance:    instance,
		ObservedAt:  observedAt,
		PlanKey:     planKey,
		TriggerKind: workflow.TriggerKindReplay,
		ScopeIDs:    []string{"", " grafana:instance:alpha ", "  grafana:instance:zeta  ", "grafana:instance:alpha "},
	})
	if err != nil {
		t.Fatalf("PlanGrafanaWork() error = %v, want nil", err)
	}

	if got, want := run.RunID, "grafana:grafana-primary:replay:"+planKey; got != want {
		t.Fatalf("RunID = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindReplay; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := run.Status, workflow.RunStatusCollectionPending; got != want {
		t.Fatalf("Status = %q, want %q", got, want)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorGrafana); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.CreatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("CreatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
	if got, want := run.UpdatedAt, observedAt.UTC(); !got.Equal(want) || got.Location() != time.UTC {
		t.Fatalf("UpdatedAt = %v (%v), want %v (UTC)", got, got.Location(), want)
	}
	if got, want := run.RequestedScopeSet, `{"collector_instance_id":"grafana-primary","targets":[{"scope_id":"grafana:instance:alpha","provider":"grafana-cloud","instance_id":""},{"scope_id":"grafana:instance:zeta","provider":"grafana-cloud","instance_id":"zeta-instance"}]}`; got != want {
		t.Fatalf("RequestedScopeSet = %q, want %q", got, want)
	}
	for _, forbidden := range []string{
		"zeta.grafana.example.com",
		"alpha.grafana.example.com",
		"GRAFANA_ZETA_TOKEN",
		"GRAFANA_ALPHA_TOKEN",
		"resource_limit",
		"stale_after",
	} {
		if strings.Contains(run.RequestedScopeSet, forbidden) {
			t.Fatalf("RequestedScopeSet = %q, must not contain %q", run.RequestedScopeSet, forbidden)
		}
	}

	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}
	wantScopes := []string{"grafana:instance:zeta", "grafana:instance:alpha"}
	wantFairness := []string{
		"grafana:grafana-primary:zeta-instance",
		"grafana:grafana-primary:grafana:instance:alpha",
	}
	wantGenerationIDs := []string{
		"grafana:2b1478055317188ed4369574e73181b415ebdb294fb13c2137dcab4bde57b9ef",
		"grafana:adb9e614fe384a0ace25a921068ddc833ba7421685ee335580cf246d384bc2ee",
	}
	for i, wantScopeID := range wantScopes {
		item := items[i]
		expectedGenerationID := wantGenerationIDs[i]
		if got, want := item.WorkItemID, "grafana:grafana-primary:"+expectedGenerationID; got != want {
			t.Fatalf("items[%d].WorkItemID = %q, want %q", i, got, want)
		}
		if got, want := item.RunID, run.RunID; got != want {
			t.Fatalf("items[%d].RunID = %q, want %q", i, got, want)
		}
		if got, want := item.CollectorKind, scope.CollectorGrafana; got != want {
			t.Fatalf("items[%d].CollectorKind = %q, want %q", i, got, want)
		}
		if got, want := item.CollectorInstanceID, instance.InstanceID; got != want {
			t.Fatalf("items[%d].CollectorInstanceID = %q, want %q", i, got, want)
		}
		if got, want := item.SourceSystem, string(scope.CollectorGrafana); got != want {
			t.Fatalf("items[%d].SourceSystem = %q, want %q", i, got, want)
		}
		if got, want := item.ScopeID, wantScopeID; got != want {
			t.Fatalf("items[%d].ScopeID = %q, want %q", i, got, want)
		}
		if got, want := item.AcceptanceUnitID, wantScopeID; got != want {
			t.Fatalf("items[%d].AcceptanceUnitID = %q, want %q", i, got, want)
		}
		if got, want := item.SourceRunID, expectedGenerationID; got != want {
			t.Fatalf("items[%d].SourceRunID = %q, want %q", i, got, want)
		}
		if got, want := item.GenerationID, expectedGenerationID; got != want {
			t.Fatalf("items[%d].GenerationID = %q, want %q", i, got, want)
		}
		if got, want := item.FairnessKey, wantFairness[i]; got != want {
			t.Fatalf("items[%d].FairnessKey = %q, want %q", i, got, want)
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

func TestGrafanaTargetConflictKeyTrimsInstanceIDBeforeScopeFallback(t *testing.T) {
	t.Parallel()

	target := grafanaTargetConfiguration{
		ScopeID:    " grafana:instance:alpha ",
		InstanceID: "   ",
	}
	if got, want := grafanaTargetConflictKey(target), "grafana:instance:alpha"; got != want {
		t.Fatalf("grafanaTargetConflictKey() = %q, want %q", got, want)
	}
}
