// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// factWorkItemFixture seeds one reducer-stage fact_work_items row for the
// #4459 dead-letter bridge tests. Status is caller-controlled so positive
// (dead_letter) and negative (retrying) cases exercise the exact same
// insertion path.
type factWorkItemFixture struct {
	WorkItemID   string
	ScopeID      string
	GenerationID string
	Stage        string
	Domain       string
	Status       string
	CreatedAt    time.Time
}

func mustInsertFactWorkItem(t *testing.T, db *sql.DB, fixture factWorkItemFixture) {
	t.Helper()
	if _, err := db.ExecContext(
		context.Background(), `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status, payload, created_at, updated_at
) VALUES (
    $1, $2, $3, $4, $5, $6, '{}'::jsonb, $7, $7
)
`,
		fixture.WorkItemID,
		fixture.ScopeID,
		fixture.GenerationID,
		fixture.Stage,
		fixture.Domain,
		fixture.Status,
		fixture.CreatedAt,
	); err != nil {
		t.Fatalf("insert fact_work_items fixture %q error = %v, want nil", fixture.WorkItemID, err)
	}
}

// openDeadLetterBridgeIntegrationStore opens the shared WorkflowControlStore
// integration harness and additionally provisions the reducer/ingestion
// schema (fact_work_items, ingestion_scopes, scope_generations,
// graph_projection_phase_state) that these #4459 tests read across store
// boundaries. ApplyBootstrap is idempotent (CREATE TABLE/INDEX IF NOT
// EXISTS), so this is safe to call even when an external bootstrap already
// ran. Kept local to this file rather than added to the shared
// openWorkflowControlIntegrationStore helper so
// workflow_control_integration_test.go stays untouched by this fix.
func openDeadLetterBridgeIntegrationStore(t *testing.T) (*sql.DB, *WorkflowControlStore) {
	t.Helper()
	db, store := openWorkflowControlIntegrationStore(t)
	ctx := context.Background()
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap() error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx, `
TRUNCATE fact_work_items, scope_generations, ingestion_scopes, graph_projection_phase_state
RESTART IDENTITY CASCADE
`); err != nil {
		t.Fatalf("TRUNCATE reducer/ingestion tables error = %v, want nil", err)
	}
	return db, store
}

func TestWorkflowControlStoreIntegrationReconcileWorkflowRunsTerminalDeadLetterBlocksConvergence(t *testing.T) {
	// Positive proof for #4459: a genuinely terminal fact_work_items
	// dead-letter on the required workload_materialization phase must not
	// leave the run wedged in reducer_converging forever — it must
	// terminate as blocked/failed with the dead-letter reason recorded.
	db, store := openDeadLetterBridgeIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.July, 1, 20, 0, 0, 0, time.UTC)
	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:integration-dlq-wedge",
		SourceSystem:  "github",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "integration-dlq-wedge",
		Metadata: map[string]string{
			"repo_id":    "integration-repo-dlq",
			"source_key": "integration-repo-dlq",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-integration-dlq-wedge",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusCompleted,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mustUpsertScopeBoundary(t, db, scopeValue, generation)

	run := workflow.Run{
		RunID:       "integration-run-dlq-wedge",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusReducerConverging,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-dlq-wedge",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		SourceSystem:        scopeValue.SourceSystem,
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "integration-repo-dlq",
		SourceRunID:         generation.GenerationID,
		GenerationID:        generation.GenerationID,
		Status:              workflow.WorkItemStatusCompleted,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	// Publish every required phase except workload_materialization: its
	// reducer intent dead-lettered terminally and will never publish.
	phaseStore := NewGraphProjectionPhaseStateStore(SQLDB{DB: db})
	if err := phaseStore.Upsert(ctx, []reducer.GraphProjectionPhaseState{
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-dlq",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			},
			Phase:       reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-dlq",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceCodeEntitiesUID,
			},
			Phase:       reducer.GraphProjectionPhaseSemanticNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-dlq",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceDeployableUnitUID,
			},
			Phase:       reducer.GraphProjectionPhaseDeployableUnitCorrelation,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-dlq",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
			},
			Phase:       reducer.GraphProjectionPhaseCanonicalNodesCommitted,
			CommittedAt: now,
			UpdatedAt:   now,
		},
		{
			Key: reducer.GraphProjectionPhaseKey{
				ScopeID:          scopeValue.ScopeID,
				AcceptanceUnitID: "integration-repo-dlq",
				SourceRunID:      generation.GenerationID,
				GenerationID:     generation.GenerationID,
				Keyspace:         reducer.GraphProjectionKeyspaceServiceUID,
			},
			Phase:       reducer.GraphProjectionPhaseDeploymentMapping,
			CommittedAt: now,
			UpdatedAt:   now,
		},
	}); err != nil {
		t.Fatalf("phaseStore.Upsert() error = %v, want nil", err)
	}

	mustInsertFactWorkItem(t, db, factWorkItemFixture{
		WorkItemID:   "reducer-workload-materialization-dead-letter",
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Stage:        "reducer",
		Domain:       string(reducer.DomainWorkloadMaterialization),
		Status:       "dead_letter",
		CreatedAt:    now,
	})

	reconciled, err := store.ReconcileWorkflowRuns(ctx, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	mustWorkflowRunStatus(t, db, run.RunID, workflow.RunStatusFailed)
	mustCompletenessStatus(
		t,
		db,
		run.RunID,
		string(scope.CollectorGit),
		string(reducer.GraphProjectionKeyspaceServiceUID),
		string(reducer.GraphProjectionPhaseWorkloadMaterialization),
		workflow.CompletenessStatusBlocked,
	)
}

func TestWorkflowControlStoreIntegrationReconcileWorkflowRunsRetryingDeadLetterDoesNotBlockConvergence(t *testing.T) {
	// Negative/false-fail guard for #4459: a still-retrying (non-terminal)
	// fact_work_items row for the same domain must NOT flip the run to
	// blocked/failed — only a confirmed status = 'dead_letter' row may
	// terminate convergence. The run must remain in reducer_converging,
	// exactly as it would without this change, proving the bridge never
	// false-fails on ordinary in-flight work.
	db, store := openDeadLetterBridgeIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.July, 1, 20, 30, 0, 0, time.UTC)
	scopeValue := scope.IngestionScope{
		ScopeID:       "git-repository-scope:integration-retrying-not-blocked",
		SourceSystem:  "github",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "integration-retrying-not-blocked",
		Metadata: map[string]string{
			"repo_id":    "integration-repo-retrying",
			"source_key": "integration-repo-retrying",
		},
	}
	generation := scope.ScopeGeneration{
		GenerationID: "generation-integration-retrying-not-blocked",
		ScopeID:      scopeValue.ScopeID,
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusCompleted,
		TriggerKind:  scope.TriggerKindSnapshot,
	}
	mustUpsertScopeBoundary(t, db, scopeValue, generation)

	run := workflow.Run{
		RunID:       "integration-run-retrying-not-blocked",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusReducerConverging,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-retrying-not-blocked",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorGit,
		CollectorInstanceID: "collector-git-default",
		SourceSystem:        scopeValue.SourceSystem,
		ScopeID:             scopeValue.ScopeID,
		AcceptanceUnitID:    "integration-repo-retrying",
		SourceRunID:         generation.GenerationID,
		GenerationID:        generation.GenerationID,
		Status:              workflow.WorkItemStatusCompleted,
		CreatedAt:           now,
		UpdatedAt:           now,
	})

	// No graph_projection_phase_state rows at all: every required phase is
	// still legitimately pending. The only reducer signal is a 'retrying'
	// fact_work_items row for workload_materialization — non-terminal, so it
	// must never be attributed as a block.
	mustInsertFactWorkItem(t, db, factWorkItemFixture{
		WorkItemID:   "reducer-workload-materialization-retrying",
		ScopeID:      scopeValue.ScopeID,
		GenerationID: generation.GenerationID,
		Stage:        "reducer",
		Domain:       string(reducer.DomainWorkloadMaterialization),
		Status:       "retrying",
		CreatedAt:    now,
	})

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
		string(reducer.GraphProjectionKeyspaceServiceUID),
		string(reducer.GraphProjectionPhaseWorkloadMaterialization),
		workflow.CompletenessStatusPending,
	)
}
