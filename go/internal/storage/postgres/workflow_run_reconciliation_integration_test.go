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

func TestWorkflowControlStoreIntegrationReconcileWorkflowRunsRequiresMatchingScopeAndGeneration(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.April, 20, 20, 0, 0, 0, time.UTC)
	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:integration-workflow-mismatch",
		SourceSystem:  "github",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "integration-workflow-mismatch",
		Metadata: map[string]string{
			"repo_id":    "integration-repo-mismatch",
			"source_key": "integration-repo-mismatch",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-integration-workflow-mismatch",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusCompleted,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mustUpsertScopeBoundary(t, db, scopeValue, generation)
	wrongGeneration := scope.ScopeGeneration{
		GenerationID: generation.GenerationID + "-other",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now.Add(time.Minute),
		IngestedAt:   now.Add(time.Minute),
		Status:       scope.GenerationStatusCompleted,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	if err := upsertScopeGeneration(ctx, SQLDB{DB: db}, wrongGeneration); err != nil {
		t.Fatalf("upsertScopeGeneration() wrong generation error = %v, want nil", err)
	}

	run := workflow.Run{
		RunID:       "integration-run-mismatch",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusReducerConverging,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-mismatch",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		ScopeID:             scopeValue.ScopeID,
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
				AcceptanceUnitID: "integration-repo-mismatch",
				SourceRunID:      wrongGeneration.GenerationID,
				GenerationID:     wrongGeneration.GenerationID,
				Keyspace:         gpphase.KeyspaceCodeEntitiesUID,
			},
			Phase:       gpphase.PhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-mismatch",
				SourceRunID:      wrongGeneration.GenerationID,
				GenerationID:     wrongGeneration.GenerationID,
				Keyspace:         gpphase.KeyspaceCodeEntitiesUID,
			},
			Phase:       gpphase.PhaseSemanticNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-mismatch",
				SourceRunID:      wrongGeneration.GenerationID,
				GenerationID:     wrongGeneration.GenerationID,
				Keyspace:         gpphase.KeyspaceDeployableUnitUID,
			},
			Phase:       gpphase.PhaseDeployableUnitCorrelation,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "workload:integration-repo-mismatch",
				SourceRunID:      wrongGeneration.GenerationID,
				GenerationID:     wrongGeneration.GenerationID,
				Keyspace:         gpphase.KeyspaceServiceUID,
			},
			Phase:       gpphase.PhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "workload:integration-repo-mismatch",
				SourceRunID:      wrongGeneration.GenerationID,
				GenerationID:     wrongGeneration.GenerationID,
				Keyspace:         gpphase.KeyspaceServiceUID,
			},
			Phase:       gpphase.PhaseDeploymentMapping,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: gpphase.PhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "workload:integration-repo-mismatch",
				SourceRunID:      wrongGeneration.GenerationID,
				GenerationID:     wrongGeneration.GenerationID,
				Keyspace:         gpphase.KeyspaceServiceUID,
			},
			Phase:       gpphase.PhaseWorkloadMaterialization,
			CommittedAt: now,
			UpdatedAt:   now,
		},
	}); err != nil {
		t.Fatalf("phaseStore.Upsert() with wrong generation error = %v, want nil", err)
	}

	reconciled, err := store.ReconcileWorkflowRuns(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	mustWorkflowRunStatus(t, db, run.RunID, workflow.RunStatusReducerConverging)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceCodeEntitiesUID),
		string(gpphase.PhaseCanonicalNodesCommitted),
		workflow.CompletenessStatusPending,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceCodeEntitiesUID),
		string(gpphase.PhaseSemanticNodesCommitted),
		workflow.CompletenessStatusPending,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceDeployableUnitUID),
		string(gpphase.PhaseDeployableUnitCorrelation),
		workflow.CompletenessStatusPending,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceServiceUID),
		string(gpphase.PhaseCanonicalNodesCommitted),
		workflow.CompletenessStatusPending,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceServiceUID),
		string(gpphase.PhaseDeploymentMapping),
		workflow.CompletenessStatusPending,
	)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(gpphase.KeyspaceServiceUID),
		string(gpphase.PhaseWorkloadMaterialization),
		workflow.CompletenessStatusPending,
	)
}
