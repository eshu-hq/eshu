// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreIntegrationReconcileWorkflowRunsUsesReducerPhaseTruth(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 19, 0, 0, 0, time.UTC)
	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:integration-workflow-run",
		SourceSystem:  "github",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "integration-workflow-run",
		Metadata: map[string]string{
			"repo_id":    "integration-repo-1",
			"source_key": "integration-repo-1",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-integration-workflow-run",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusCompleted,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mustUpsertScopeBoundary(t, db, scopeValue, generation)

	run := workflow.Run{
		RunID:       "integration-run-complete",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusReducerConverging,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-complete",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		SourceSystem:        scopeValue.SourceSystem,
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "integration-repo-1",
		SourceRunID:         generation.GenerationID,
		GenerationID:        generation.GenerationID,
		Status:              workflow.WorkItemStatusCompleted,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	phaseStore := NewGraphProjectionPhaseStateStore(SQLDB{DB: db})
	if err := phaseStore.Upsert(ctx, []gpphase.PhaseState{
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         gpphase.KeyspaceCodeEntitiesUID,
			},
			Phase:       gpphase.PhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         gpphase.KeyspaceCodeEntitiesUID,
			},
			Phase:       gpphase.PhaseSemanticNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         gpphase.KeyspaceDeployableUnitUID,
			},
			Phase:       gpphase.PhaseDeployableUnitCorrelation,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         gpphase.KeyspaceServiceUID,
			},
			Phase:       gpphase.PhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         gpphase.KeyspaceServiceUID,
			},
			Phase:       gpphase.PhaseDeploymentMapping,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-1",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         gpphase.KeyspaceServiceUID,
			},
			Phase:       gpphase.PhaseWorkloadMaterialization,
			CommittedAt: now,
			UpdatedAt:   now,
		},
	}); err != nil {
		t.Fatalf("phaseStore.Upsert() error = %v, want nil", err)
	}

	reconciled, err := store.ReconcileWorkflowRuns(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	mustWorkflowRunStatus(t, db, run.RunID, workflow.RunStatusComplete)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceCodeEntitiesUID),
		"canonical_nodes_committed",
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceCodeEntitiesUID),
		"semantic_nodes_committed",
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceDeployableUnitUID),
		string(gpphase.PhaseDeployableUnitCorrelation),
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceServiceUID),
		string(gpphase.PhaseCanonicalNodesCommitted),
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceServiceUID),
		string(gpphase.PhaseDeploymentMapping),
		workflow.CompletenessStatusReady,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceServiceUID),
		string(gpphase.PhaseWorkloadMaterialization),
		workflow.CompletenessStatusReady,
	)
}

func TestWorkflowControlStoreIntegrationHeartbeatUpdatesClaimAndWorkItemLeaseTogether(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 18, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "integration-run-4",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-heartbeat-1",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             "scope-heartbeat-1",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	item, claim, found, err := store.ClaimNextEligible(ctx, workflow.ClaimSelector{
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		OwnerID:             "collector-pod-heartbeat",
		ClaimID:             "claim-heartbeat",
	}, now, time.Minute)
	if err != nil {
		t.Fatalf("ClaimNextEligible() error = %v, want nil", err)
	}
	if !found {
		t.Fatal("ClaimNextEligible() found = false, want true")
	}

	heartbeatAt := now.Add(15 * time.Second)
	leaseTTL := 90 * time.Second
	if err := store.HeartbeatClaim(ctx, workflow.ClaimMutation{
		WorkItemID:    item.WorkItemID,
		ClaimID:       claim.ClaimID,
		FencingToken:  claim.FencingToken,
		OwnerID:       claim.OwnerID,
		ObservedAt:    heartbeatAt,
		LeaseDuration: leaseTTL,
	}); err != nil {
		t.Fatalf("HeartbeatClaim() error = %v, want nil", err)
	}

	mustHeartbeatLeaseState(t, db, claim.ClaimID, item.WorkItemID, heartbeatAt, heartbeatAt.Add(leaseTTL))
}
