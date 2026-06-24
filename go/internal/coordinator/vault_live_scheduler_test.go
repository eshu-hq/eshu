// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestVaultLiveWorkPlannerPlansOneWorkItemPerTarget(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "vault-live-primary",
		CollectorKind:  scope.CollectorVaultLive,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"vault_cluster_id":"vault-a","namespace":"admin","address":"http://127.0.0.1:8200","token_env":"VAULT_TOKEN"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}

	run, items, err := (VaultLiveWorkPlanner{}).PlanVaultLiveWork(t.Context(), VaultLivePlanRequest{
		Instance:   instance,
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260606T120000Z",
	})
	if err != nil {
		t.Fatalf("PlanVaultLiveWork() error = %v, want nil", err)
	}
	if run.RequestedCollector != string(scope.CollectorVaultLive) || run.TriggerKind != workflow.TriggerKindSchedule {
		t.Fatalf("run = %+v", run)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	wantScopeID, err := vaultlive.VaultScopeID("vault-a", "admin")
	if err != nil {
		t.Fatalf("VaultScopeID() error = %v", err)
	}
	item := items[0]
	if item.CollectorKind != scope.CollectorVaultLive || item.SourceSystem != vaultlive.CollectorKind {
		t.Fatalf("item collector/source = %q/%q", item.CollectorKind, item.SourceSystem)
	}
	if item.ScopeID != wantScopeID || item.AcceptanceUnitID != wantScopeID {
		t.Fatalf("item scope = %q acceptance = %q, want %q", item.ScopeID, item.AcceptanceUnitID, wantScopeID)
	}
	if !strings.HasPrefix(item.GenerationID, "vault_live:") || item.GenerationID != item.SourceRunID {
		t.Fatalf("generation/source_run = %q/%q", item.GenerationID, item.SourceRunID)
	}
	for _, leak := range []string{"VAULT_TOKEN", "127.0.0.1", "vault-a", "admin"} {
		if strings.Contains(run.RequestedScopeSet, leak) {
			t.Fatalf("requested_scope_set leaked %q: %s", leak, run.RequestedScopeSet)
		}
	}
	if strings.Contains(item.FairnessKey, "vault-a") || strings.Contains(item.FairnessKey, "admin") {
		t.Fatalf("fairness key leaked raw Vault target identity: %s", item.FairnessKey)
	}
}

func TestVaultLiveWorkPlannerUsesPlanKeyForRecurringWorkIdentity(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 12, 0, 0, 0, time.UTC)
	instance := workflow.CollectorInstance{
		InstanceID:     "vault-live-primary",
		CollectorKind:  scope.CollectorVaultLive,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"vault_cluster_id":"vault-a","namespace":"admin","address":"http://127.0.0.1:8200","token_env":"VAULT_TOKEN"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
	request := VaultLivePlanRequest{Instance: instance, ObservedAt: observedAt, PlanKey: "continuous-20260606T120000Z"}

	firstRun, firstItems, err := (VaultLiveWorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err != nil {
		t.Fatalf("first PlanVaultLiveWork() error = %v", err)
	}
	secondRun, secondItems, err := (VaultLiveWorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err != nil {
		t.Fatalf("second PlanVaultLiveWork() error = %v", err)
	}
	if firstRun.RunID != secondRun.RunID || firstItems[0].WorkItemID != secondItems[0].WorkItemID {
		t.Fatalf("planner identities are not deterministic: first %q/%q second %q/%q",
			firstRun.RunID, firstItems[0].WorkItemID, secondRun.RunID, secondItems[0].WorkItemID)
	}
}

func TestVaultLiveWorkPlannerRejectsWrongCollectorKind(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.June, 6, 12, 0, 0, 0, time.UTC)
	_, _, err := (VaultLiveWorkPlanner{}).PlanVaultLiveWork(t.Context(), VaultLivePlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:     "grafana",
			CollectorKind:  scope.CollectorGrafana,
			Mode:           workflow.CollectorModeContinuous,
			Enabled:        true,
			ClaimsEnabled:  true,
			Configuration:  `{"targets":[]}`,
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260606T120000Z",
	})
	if err == nil {
		t.Fatal("PlanVaultLiveWork() error = nil, want collector_kind rejection")
	}
}

func TestVaultLiveWorkPlannerDuplicateTargetErrorDoesNotExposeRawIdentity(t *testing.T) {
	t.Parallel()

	err := validateUniqueVaultLiveTargets([]vaultLiveTargetConfiguration{
		{VaultClusterID: "vault-a", Namespace: "admin"},
		{VaultClusterID: "vault-a", Namespace: "admin"},
	})
	if err == nil {
		t.Fatal("validateUniqueVaultLiveTargets() error = nil, want duplicate-target error")
	}
	if strings.Contains(err.Error(), "vault-a") || strings.Contains(err.Error(), "admin") {
		t.Fatalf("duplicate-target error exposed raw target identity: %v", err)
	}
}

func TestVaultLiveWorkPlannerTargetKeyDoesNotCollideOnColon(t *testing.T) {
	t.Parallel()

	err := validateUniqueVaultLiveTargets([]vaultLiveTargetConfiguration{
		{VaultClusterID: "vault:a", Namespace: "admin"},
		{VaultClusterID: "vault", Namespace: "a:admin"},
	})
	if err != nil {
		t.Fatalf("validateUniqueVaultLiveTargets() error = %v, want nil for distinct targets", err)
	}
}
