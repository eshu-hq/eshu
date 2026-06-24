// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestWorkflowControlStoreCreateRunExecutesInsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:       "run-1",
		TriggerKind: workflow.TriggerKindBootstrap,
		Status:      workflow.RunStatusCollectionPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("CreateRun() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO workflow_runs") {
		t.Fatalf("query missing workflow_runs insert: %s", db.execs[0].query)
	}
	if strings.Contains(db.execs[0].query, "status = EXCLUDED.status") {
		t.Fatalf("query regresses existing workflow status on conflict: %s", db.execs[0].query)
	}
}

func TestWorkflowControlStoreEnqueueWorkItemsExecutesBatchInsert(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.April, 20, 14, 0, 0, 0, time.UTC)
	items := []workflow.WorkItem{
		{
			WorkItemID:          "item-1",
			RunID:               "run-1",
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			SourceSystem:        "git",
			ScopeID:             "scope-1",
			AcceptanceUnitID:    "repository:scope-1",
			SourceRunID:         "source-run-1",
			GenerationID:        "generation-1",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
		{
			WorkItemID:          "item-2",
			RunID:               "run-1",
			CollectorKind:       scope.CollectorGit,
			CollectorInstanceID: "collector-git-default",
			SourceSystem:        "git",
			ScopeID:             "scope-2",
			AcceptanceUnitID:    "repository:scope-2",
			SourceRunID:         "source-run-2",
			GenerationID:        "generation-2",
			Status:              workflow.WorkItemStatusPending,
			CreatedAt:           now,
			UpdatedAt:           now,
		},
	}

	if err := store.EnqueueWorkItems(context.Background(), items); err != nil {
		t.Fatalf("EnqueueWorkItems() error = %v, want nil", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if !strings.Contains(db.execs[0].query, "INSERT INTO workflow_work_items") {
		t.Fatalf("query missing workflow_work_items insert: %s", db.execs[0].query)
	}
	if !strings.Contains(db.execs[0].query, "ON CONFLICT DO NOTHING") {
		t.Fatalf("query must ignore all workflow work item uniqueness conflicts: %s", db.execs[0].query)
	}
	for _, want := range []string{"source_system", "acceptance_unit_id", "source_run_id"} {
		if !strings.Contains(db.execs[0].query, want) {
			t.Fatalf("query missing workflow identity column %q: %s", want, db.execs[0].query)
		}
	}
}

func TestWorkflowControlStoreGuardedRunSkipsOpenScheduledTarget(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{false}}},
				{rows: nil},
			},
		},
	}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "aws:collector-aws:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "aws:collector-aws:gen-2",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: "collector-aws",
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             "aws:123456789012:us-east-1:lambda",
		AcceptanceUnitID:    `{"account_id":"123456789012","region":"us-east-1","service_kind":"lambda"}`,
		SourceRunID:         "gen-2",
		GenerationID:        "gen-2",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	inserted, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, []workflow.WorkItem{item})
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := inserted, 0; got != want {
		t.Fatalf("inserted = %d, want %d", got, want)
	}
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO workflow_runs") || strings.Contains(exec.query, "INSERT INTO workflow_work_items") {
			t.Fatalf("guarded schedule inserted while target was open: %s", exec.query)
		}
	}
	if !db.committed {
		t.Fatal("guarded schedule did not commit skipped transaction")
	}
}

func TestWorkflowControlStoreGuardedRunCreatesEligibleScheduledTarget(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{false}}},
				{rows: [][]any{{0}}},
			},
		},
	}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "aws:collector-aws:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "aws:collector-aws:gen-2",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: "collector-aws",
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             "aws:123456789012:us-east-1:lambda",
		AcceptanceUnitID:    `{"account_id":"123456789012","region":"us-east-1","service_kind":"lambda"}`,
		SourceRunID:         "gen-2",
		GenerationID:        "gen-2",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	inserted, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, []workflow.WorkItem{item})
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := inserted, 1; got != want {
		t.Fatalf("inserted = %d, want %d", got, want)
	}
	var insertedRun bool
	var insertedItem bool
	for _, exec := range db.execs {
		insertedRun = insertedRun || strings.Contains(exec.query, "INSERT INTO workflow_runs")
		insertedItem = insertedItem || strings.Contains(exec.query, "INSERT INTO workflow_work_items")
	}
	if !insertedRun {
		t.Fatal("guarded schedule did not insert workflow run")
	}
	if !insertedItem {
		t.Fatal("guarded schedule did not insert workflow work item")
	}
	if !db.committed {
		t.Fatal("guarded schedule did not commit transaction")
	}
}

func TestWorkflowControlStoreGuardedRunRetriesDeadlockTransaction(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "aws:collector-aws:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := awsScheduledWorkItem(run.RunID, "lambda", now)
	firstTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{false}}},
			{rows: [][]any{{0}}},
		},
		execErrors: map[int]error{
			1: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"},
		},
	}
	secondTx := &fakeTx{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{false}}},
			{rows: [][]any{{0}}},
		},
	}
	db := &fakeTransactionalDB{txs: []*fakeTx{firstTx, secondTx}}
	store := NewWorkflowControlStore(db)

	inserted, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, []workflow.WorkItem{item})
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want retry success", err)
	}
	if got, want := inserted, 1; got != want {
		t.Fatalf("inserted = %d, want %d", got, want)
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

func TestWorkflowControlStoreGuardedRunRetryLimitUsesExactAttemptCount(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "aws:collector-aws:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := awsScheduledWorkItem(run.RunID, "lambda", now)
	txs := make([]*fakeTx, 0, workflowGuardedRunCreateMaxAttempts)
	for range workflowGuardedRunCreateMaxAttempts {
		txs = append(txs, &fakeTx{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{false}}},
				{rows: [][]any{{0}}},
			},
			execErrors: map[int]error{
				1: &pgconn.PgError{Code: "40P01", Message: "deadlock detected"},
			},
		})
	}
	db := &fakeTransactionalDB{txs: txs}
	store := NewWorkflowControlStore(db)

	inserted, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, []workflow.WorkItem{item})
	if err == nil {
		t.Fatal("CreateRunWithWorkItemsIfNoOpenTargets() error = nil, want final deadlock")
	}
	if got, want := inserted, 0; got != want {
		t.Fatalf("inserted = %d, want %d", got, want)
	}
	if got, want := db.beginCalls, workflowGuardedRunCreateMaxAttempts; got != want {
		t.Fatalf("begin calls = %d, want exact max attempts %d", got, want)
	}
}

func TestWorkflowControlStoreGuardedRunComputesEligibleTargetsInOneQuery(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{false}}},
				{rows: [][]any{{0}, {1}}},
			},
		},
	}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "aws:collector-aws:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	items := []workflow.WorkItem{
		awsScheduledWorkItem(run.RunID, "lambda", now),
		awsScheduledWorkItem(run.RunID, "s3", now),
	}

	inserted, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, items)
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := inserted, 2; got != want {
		t.Fatalf("inserted = %d, want %d", got, want)
	}

	var eligibilityQueries int
	for _, query := range db.queries {
		if strings.Contains(query.query, "planned_targets") {
			eligibilityQueries++
			if strings.Count(query.query, "NOT EXISTS") != 2 {
				t.Fatalf("eligibility query must check open target and same-run target in one set:\n%s", query.query)
			}
			if strings.Count(query.query, "VALUES") != 1 {
				t.Fatalf("eligibility query must use one VALUES relation, got:\n%s", query.query)
			}
		}
	}
	if got, want := eligibilityQueries, 1; got != want {
		t.Fatalf("eligibility query count = %d, want %d; queries = %#v", got, want, db.queries)
	}
	if got, want := len(db.queries), 2; got != want {
		t.Fatalf("query count = %d, want terminal-run + set eligibility only", got)
	}
}

func TestWorkflowControlStoreGuardedRunLocksCollectorInstanceOnceForTargetBatch(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{false}}},
				{rows: [][]any{{0}, {1}}},
			},
		},
	}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "aws:collector-aws:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorAWS),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	items := []workflow.WorkItem{
		awsScheduledWorkItem(run.RunID, "lambda", now),
		awsScheduledWorkItem(run.RunID, "s3", now),
	}

	if _, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, items); err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}

	var lockExecs int
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "pg_advisory_xact_lock") {
			lockExecs++
		}
	}
	if got, want := lockExecs, 1; got != want {
		t.Fatalf("advisory lock exec count = %d, want one collector-instance planning lock", got)
	}
}

func TestWorkflowControlStoreGuardedRunSkipsSameRunTargetReplay(t *testing.T) {
	t.Parallel()

	db := &fakeBeginnerExecQueryer{
		fakeExecQueryer: fakeExecQueryer{
			queryResponses: []queueFakeRows{
				{rows: [][]any{{false}}},
				{rows: nil},
			},
		},
	}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.May, 21, 12, 0, 0, 0, time.UTC)
	run := workflow.Run{
		RunID:              "terraform_state:remote-e2e-terraform-state:schedule:continuous-20260521T120000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorTerraformState),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "terraform-state:remote-e2e-terraform-state:gen-2",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "remote-e2e-terraform-state",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             "terraform_state:s3:bucket/key.tfstate",
		AcceptanceUnitID:    `{"backend":"s3","bucket":"bucket","key":"key.tfstate"}`,
		SourceRunID:         "gen-2",
		GenerationID:        "gen-2",
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	inserted, err := store.CreateRunWithWorkItemsIfNoOpenTargets(context.Background(), run, []workflow.WorkItem{item})
	if err != nil {
		t.Fatalf("CreateRunWithWorkItemsIfNoOpenTargets() error = %v, want nil", err)
	}
	if got, want := inserted, 0; got != want {
		t.Fatalf("inserted = %d, want %d", got, want)
	}
	for _, exec := range db.execs {
		if strings.Contains(exec.query, "INSERT INTO workflow_runs") || strings.Contains(exec.query, "INSERT INTO workflow_work_items") {
			t.Fatalf("guarded schedule inserted duplicate target for same run: %s", exec.query)
		}
	}
	if !db.committed {
		t.Fatal("guarded schedule did not commit skipped transaction")
	}
}
