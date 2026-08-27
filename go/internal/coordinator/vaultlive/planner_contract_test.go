// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultlive

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	vaultcollector "github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkPlannerPreservesVaultLivePlanningContract(t *testing.T) {
	t.Parallel()

	request := testPlanRequest()
	run, items, err := (WorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanVaultLiveWork() error = %v, want nil", err)
	}
	if got, want := run.RequestedCollector, string(scope.CollectorVaultLive); got != want {
		t.Fatalf("RequestedCollector = %q, want %q", got, want)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindSchedule; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if got, want := len(items), 2; got != want {
		t.Fatalf("len(items) = %d, want %d", got, want)
	}

	wantScopes := []string{
		mustVaultScopeID(t, "vault-z", "team-z"),
		mustVaultScopeID(t, "vault-a", "team-a"),
	}
	for index, item := range items {
		if got, want := item.ScopeID, wantScopes[index]; got != want {
			t.Fatalf("items[%d].ScopeID = %q, want %q", index, got, want)
		}
		if got, want := item.AcceptanceUnitID, wantScopes[index]; got != want {
			t.Fatalf("items[%d].AcceptanceUnitID = %q, want %q", index, got, want)
		}
		if item.SourceSystem != vaultcollector.CollectorKind {
			t.Fatalf("items[%d].SourceSystem = %q, want %q", index, item.SourceSystem, vaultcollector.CollectorKind)
		}
		if !strings.HasPrefix(item.GenerationID, "vault_live:") || item.GenerationID != item.SourceRunID {
			t.Fatalf("items[%d] generation/source_run = %q/%q", index, item.GenerationID, item.SourceRunID)
		}
	}

	var requested struct {
		CollectorInstanceID string `json:"collector_instance_id"`
		Targets             []struct {
			ScopeID     string `json:"scope_id"`
			Environment string `json:"environment"`
		} `json:"targets"`
	}
	if err := json.Unmarshal([]byte(run.RequestedScopeSet), &requested); err != nil {
		t.Fatalf("decode RequestedScopeSet: %v", err)
	}
	if got, want := requested.CollectorInstanceID, request.Instance.InstanceID; got != want {
		t.Fatalf("CollectorInstanceID = %q, want %q", got, want)
	}
	if got, want := len(requested.Targets), 2; got != want {
		t.Fatalf("requested targets = %d, want %d", got, want)
	}
	if requested.Targets[0].ScopeID >= requested.Targets[1].ScopeID {
		t.Fatalf("requested targets are not sorted by scope_id: %+v", requested.Targets)
	}
	gotRequestedScopes := []string{requested.Targets[0].ScopeID, requested.Targets[1].ScopeID}
	wantRequestedScopes := append([]string(nil), wantScopes...)
	sort.Strings(wantRequestedScopes)
	if !reflect.DeepEqual(gotRequestedScopes, wantRequestedScopes) {
		t.Fatalf("requested target scopes = %v, want %v", gotRequestedScopes, wantRequestedScopes)
	}
	wantEnvironments := map[string]string{
		mustVaultScopeID(t, "vault-z", "team-z"): "prod",
		mustVaultScopeID(t, "vault-a", "team-a"): "dev",
	}
	for _, target := range requested.Targets {
		if got, want := target.Environment, wantEnvironments[target.ScopeID]; got != want {
			t.Fatalf("environment for scope %q = %q, want %q", target.ScopeID, got, want)
		}
	}
}

func TestWorkPlannerIsDeterministicAndDoesNotPersistVaultConnectionMaterial(t *testing.T) {
	t.Parallel()

	request := testPlanRequest()
	firstRun, firstItems, err := (WorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err != nil {
		t.Fatalf("first PlanVaultLiveWork() error = %v", err)
	}
	secondRun, secondItems, err := (WorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err != nil {
		t.Fatalf("second PlanVaultLiveWork() error = %v", err)
	}
	if !reflect.DeepEqual(firstRun, secondRun) || !reflect.DeepEqual(firstItems, secondItems) {
		t.Fatalf("planner output changed for the same request")
	}

	persisted := firstRun.RunID + firstRun.RequestedScopeSet + firstRun.RequestedCollector
	for _, item := range firstItems {
		persisted += item.WorkItemID + item.CollectorInstanceID + item.SourceSystem + item.ScopeID +
			item.AcceptanceUnitID + item.SourceRunID + item.GenerationID + item.FairnessKey
	}
	for _, secret := range []string{
		"VAULT_Z_TOKEN",
		"VAULT_A_TOKEN",
		"vault-z.internal.example",
		"vault-a.internal.example",
		"vault-z",
		"vault-a",
		"team-z",
		"team-a",
	} {
		if strings.Contains(persisted, secret) {
			t.Fatalf("planner output leaked %q: %s", secret, persisted)
		}
	}
}

func TestWorkPlannerUsesBootstrapTrigger(t *testing.T) {
	t.Parallel()

	request := testPlanRequest()
	request.Instance.Bootstrap = true
	request.PlanKey = "bootstrap"

	run, _, err := (WorkPlanner{}).PlanVaultLiveWork(t.Context(), request)
	if err != nil {
		t.Fatalf("PlanVaultLiveWork() error = %v, want nil", err)
	}
	if got, want := run.TriggerKind, workflow.TriggerKindBootstrap; got != want {
		t.Fatalf("TriggerKind = %q, want %q", got, want)
	}
	if !strings.Contains(run.RunID, ":bootstrap:bootstrap") {
		t.Fatalf("RunID = %q, want bootstrap trigger and plan key", run.RunID)
	}
}

func testPlanRequest() PlanRequest {
	observedAt := time.Date(2026, time.June, 6, 12, 0, 0, 123, time.FixedZone("test-offset", -5*60*60))
	return PlanRequest{
		Instance: workflow.CollectorInstance{
			InstanceID:    "vault-live-primary",
			CollectorKind: scope.CollectorVaultLive,
			Mode:          workflow.CollectorModeContinuous,
			Enabled:       true,
			ClaimsEnabled: true,
			Configuration: `{"targets":[` +
				`{"vault_cluster_id":"vault-z","namespace":"team-z","address":"https://vault-z.internal.example","token_env":"VAULT_Z_TOKEN","environment":"prod"},` +
				`{"vault_cluster_id":"vault-a","namespace":"team-a","address":"https://vault-a.internal.example","token_env":"VAULT_A_TOKEN","environment":"dev"}` +
				`]}`,
			LastObservedAt: observedAt,
			CreatedAt:      observedAt,
			UpdatedAt:      observedAt,
		},
		ObservedAt: observedAt,
		PlanKey:    "continuous-20260606T170000Z",
	}
}

func mustVaultScopeID(t *testing.T, clusterID, namespace string) string {
	t.Helper()
	scopeID, err := vaultcollector.VaultScopeID(clusterID, namespace)
	if err != nil {
		t.Fatalf("VaultScopeID(%q, %q) error = %v", clusterID, namespace, err)
	}
	return scopeID
}
