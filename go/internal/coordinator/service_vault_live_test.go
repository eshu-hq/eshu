// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	coordinatorvaultlive "github.com/eshu-hq/eshu/go/internal/coordinator/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeVaultLivePlanner struct {
	requests []coordinatorvaultlive.PlanRequest
	run      workflow.Run
	items    []workflow.WorkItem
	err      error
}

func (f *fakeVaultLivePlanner) PlanVaultLiveWork(
	_ context.Context,
	request coordinatorvaultlive.PlanRequest,
) (workflow.Run, []workflow.WorkItem, error) {
	f.requests = append(f.requests, request)
	if f.err != nil {
		return workflow.Run{}, nil, f.err
	}
	return f.run, append([]workflow.WorkItem(nil), f.items...), nil
}

func TestServiceRunActiveModeSchedulesVaultLiveWorkThroughChildPlanner(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.June, 6, 14, 0, 0, 0, time.UTC)
	scopeID, err := vaultlive.VaultScopeID("vault-a", "admin")
	if err != nil {
		t.Fatalf("VaultScopeID() error = %v", err)
	}
	run := workflow.Run{
		RunID:              "vault_live:vault-live-primary:continuous-20260606T140000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "{}",
		RequestedCollector: string(scope.CollectorVaultLive),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "vault-live-item-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorVaultLive,
		CollectorInstanceID: "vault-live-primary",
		SourceSystem:        vaultlive.CollectorKind,
		ScopeID:             scopeID,
		AcceptanceUnitID:    scopeID,
		SourceRunID:         "vault_live:generation-1",
		GenerationID:        "vault_live:generation-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	planner := &fakeVaultLivePlanner{run: run, items: []workflow.WorkItem{item}}
	instance := testServiceVaultLiveInstance(now)
	service := Service{
		Config: Config{
			DeploymentMode:           deploymentModeActive,
			ClaimsEnabled:            true,
			ReconcileInterval:        time.Hour,
			ReapInterval:             time.Hour,
			ClaimLeaseTTL:            time.Minute,
			HeartbeatInterval:        20 * time.Second,
			ExpiredClaimLimit:        10,
			ExpiredClaimRequeueDelay: 5 * time.Second,
			CollectorInstances: []workflow.DesiredCollectorInstance{{
				InstanceID:    instance.InstanceID,
				CollectorKind: instance.CollectorKind,
				Mode:          instance.Mode,
				Enabled:       instance.Enabled,
				ClaimsEnabled: instance.ClaimsEnabled,
				Configuration: instance.Configuration,
			}},
		},
		Store:            &fakeStore{instances: []workflow.CollectorInstance{instance}},
		VaultLivePlanner: planner,
		Clock:            func() time.Time { return now },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got, want := len(planner.requests), 1; got != want {
		t.Fatalf("planner requests = %d, want %d", got, want)
	}
	wantRequest := coordinatorvaultlive.PlanRequest{
		Instance:   instance,
		ObservedAt: now,
		PlanKey:    "continuous-20260606T140000Z",
	}
	if got := planner.requests[0]; !reflect.DeepEqual(got, wantRequest) {
		t.Fatalf("planner request = %+v, want %+v", got, wantRequest)
	}
	store := service.Store.(*fakeStore)
	if got, want := len(store.createdRuns), 1; got != want {
		t.Fatalf("created runs = %d, want %d", got, want)
	}
	if got, want := len(store.enqueuedItems), 1; got != want {
		t.Fatalf("enqueued items = %d, want %d", got, want)
	}
}

func testServiceVaultLiveInstance(observedAt time.Time) workflow.CollectorInstance {
	return workflow.CollectorInstance{
		InstanceID:     "vault-live-primary",
		CollectorKind:  scope.CollectorVaultLive,
		Mode:           workflow.CollectorModeContinuous,
		Enabled:        true,
		ClaimsEnabled:  true,
		Configuration:  `{"targets":[{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://vault.example.com","token_env":"VAULT_TOKEN"}]}`,
		LastObservedAt: observedAt,
		CreatedAt:      observedAt,
		UpdatedAt:      observedAt,
	}
}
