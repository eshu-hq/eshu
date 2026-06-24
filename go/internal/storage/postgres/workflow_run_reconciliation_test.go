// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreReconcileWorkflowRunsUpdatesRunAndCompleteness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 21, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"run-1",
					string(workflow.TriggerKindBootstrap),
					string(workflow.RunStatusReducerConverging),
					"[]",
					sql.NullString{},
					now.Add(-time.Minute),
					now.Add(-time.Minute),
					sql.NullTime{},
				}},
			},
			{
				rows: [][]any{{
					string(scope.CollectorGit),
					1,
					0,
					0,
					1,
					0,
				}},
			},
			{
				rows: [][]any{
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
						string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
						string(reducer.GraphProjectionPhaseSemanticNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceDeployableUnitUID),
						string(reducer.GraphProjectionPhaseDeployableUnitCorrelation),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseDeploymentMapping),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseWorkloadMaterialization),
						1,
					},
				},
			},
		},
	}
	store := NewWorkflowControlStore(db)

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 7; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "UPDATE workflow_runs") {
		t.Fatalf("first exec missing workflow_runs update: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[1].query, "INSERT INTO workflow_run_completeness") {
		t.Fatalf("second exec missing workflow_run_completeness upsert: %s", db.execs[1].query)
	}
	if got, want := db.execs[1].args[2], string(reducer.GraphProjectionKeyspaceCodeEntitiesUID); got != want {
		t.Fatalf("first completeness keyspace arg = %v, want %q", got, want)
	}
}

func TestWorkflowControlStoreReconcileWorkflowRunsJoinsExactPhaseStateTuple(t *testing.T) {
	t.Parallel()

	query := listWorkflowCollectorPhaseCountsQuery
	for _, want := range []string{
		"phase.scope_id = item.scope_id",
		"phase.acceptance_unit_id = item.acceptance_unit_id",
		"phase.source_run_id = item.source_run_id",
		"phase.generation_id = item.generation_id",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("phase-count query missing exact identity predicate %q:\n%s", want, query)
		}
	}
}

func TestWorkflowControlStoreReconcileWorkflowRunsCompletesAWSWithoutPhaseRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 21, 13, 35, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"aws-run",
					string(workflow.TriggerKindSchedule),
					string(workflow.RunStatusReducerConverging),
					"[]",
					sql.NullString{String: string(scope.CollectorAWS), Valid: true},
					now.Add(-time.Minute),
					now.Add(-time.Minute),
					sql.NullTime{},
				}},
			},
			{
				rows: [][]any{{
					string(scope.CollectorAWS),
					19,
					0,
					0,
					19,
					0,
				}},
			},
			{
				rows: nil,
			},
		},
	}
	store := NewWorkflowControlStore(db)

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want only workflow_runs update", got)
	}
	if got, want := db.execs[0].args[1], string(workflow.RunStatusComplete); got != want {
		t.Fatalf("updated status arg = %v, want %q", got, want)
	}
	if got, want := db.execs[0].args[3], true; got != want {
		t.Fatalf("finished flag arg = %v, want %v", got, want)
	}
}

func TestWorkflowControlStoreReconcileWorkflowRunsReturnsZeroWhenNoRunsNeedWork(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: nil}},
	}
	store := NewWorkflowControlStore(db)

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), time.Date(2026, time.April, 20, 21, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 0; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
}

func TestWorkflowControlStoreReconcileWorkflowRunsUsesTransactionWhenAvailable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 20, 21, 10, 0, 0, time.UTC)
	tx := &fakeTx{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					string(scope.CollectorGit),
					1,
					0,
					0,
					1,
					0,
				}},
			},
			{
				rows: [][]any{
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
						string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
						string(reducer.GraphProjectionPhaseSemanticNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceDeployableUnitUID),
						string(reducer.GraphProjectionPhaseDeployableUnitCorrelation),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseDeploymentMapping),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseWorkloadMaterialization),
						1,
					},
				},
			},
		},
	}
	db := &fakeTransactionalDB{
		tx: tx,
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"run-transactional",
					string(workflow.TriggerKindBootstrap),
					string(workflow.RunStatusReducerConverging),
					"[]",
					sql.NullString{},
					now.Add(-time.Minute),
					now.Add(-time.Minute),
					sql.NullTime{},
				}},
			},
		},
	}
	store := NewWorkflowControlStore(db)

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	if got, want := db.beginCalls, 1; got != want {
		t.Fatalf("begin calls = %d, want %d", got, want)
	}
	if !tx.committed {
		t.Fatal("transaction committed = false, want true")
	}
	if tx.rolledBack {
		t.Fatal("transaction rolled back after successful commit, want false")
	}
	if got, want := len(tx.execs), 7; got != want {
		t.Fatalf("transaction exec count = %d, want %d", got, want)
	}
	if !strings.Contains(tx.execs[0].query, "UPDATE workflow_runs") {
		t.Fatalf("first tx exec missing workflow_runs update: %s", tx.execs[0].query)
	}
	if !strings.Contains(tx.execs[1].query, "INSERT INTO workflow_run_completeness") {
		t.Fatalf("second tx exec missing workflow_run_completeness upsert: %s", tx.execs[1].query)
	}
	if got, want := tx.execs[1].args[2], string(reducer.GraphProjectionKeyspaceCodeEntitiesUID); got != want {
		t.Fatalf("first transactional completeness keyspace arg = %v, want %q", got, want)
	}
}

func TestWorkflowControlStoreReconcileWorkflowRunsRetriesDeadlockTransaction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 18, 16, 30, 0, 0, time.UTC)
	firstTx := workflowRunReconciliationTx()
	firstTx.execErrors = map[int]error{0: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}}
	secondTx := workflowRunReconciliationTx()
	db := &fakeTransactionalDB{
		txs: []*fakeTx{firstTx, secondTx},
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					"run-deadlock",
					string(workflow.TriggerKindSchedule),
					string(workflow.RunStatusReducerConverging),
					"[]",
					sql.NullString{},
					now.Add(-time.Minute),
					now.Add(-time.Minute),
					sql.NullTime{},
				}},
			},
		},
	}
	store := NewWorkflowControlStore(db)

	reconciled, err := store.ReconcileWorkflowRuns(context.Background(), now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want retry success", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	if got, want := db.beginCalls, 2; got != want {
		t.Fatalf("begin calls = %d, want %d", got, want)
	}
	if !firstTx.rolledBack {
		t.Fatal("first transaction rolledBack = false, want true after deadlock")
	}
	if !secondTx.committed {
		t.Fatal("second transaction committed = false, want true")
	}
}

func workflowRunReconciliationTx() *fakeTx {
	return &fakeTx{
		queryResponses: []queueFakeRows{
			{
				rows: [][]any{{
					string(scope.CollectorGit),
					1,
					0,
					0,
					1,
					0,
				}},
			},
			{
				rows: [][]any{
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
						string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceCodeEntitiesUID),
						string(reducer.GraphProjectionPhaseSemanticNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceDeployableUnitUID),
						string(reducer.GraphProjectionPhaseDeployableUnitCorrelation),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseCanonicalNodesCommitted),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseDeploymentMapping),
						1,
					},
					{
						string(scope.CollectorGit),
						string(reducer.GraphProjectionKeyspaceServiceUID),
						string(reducer.GraphProjectionPhaseWorkloadMaterialization),
						1,
					},
				},
			},
		},
	}
}
