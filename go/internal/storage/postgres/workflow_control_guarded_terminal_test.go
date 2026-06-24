// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func awsScheduledWorkItem(runID, serviceKind string, now time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "aws:collector-aws:" + serviceKind,
		RunID:               runID,
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: "collector-aws",
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             "aws:123456789012:us-east-1:" + serviceKind,
		AcceptanceUnitID:    `{"account_id":"123456789012","region":"us-east-1","service_kind":"` + serviceKind + `"}`,
		SourceRunID:         "gen-" + serviceKind,
		GenerationID:        "gen-" + serviceKind,
		Status:              workflow.WorkItemStatusPending,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func TestWorkflowControlStoreGuardedRunSkipsTerminalSameRunReplay(t *testing.T) {
	t.Parallel()

	db := &terminalRunReplayDB{}
	store := NewWorkflowControlStore(db)
	now := time.Date(2026, time.May, 21, 15, 38, 43, 0, time.UTC)
	run := workflow.Run{
		RunID:              "terraform_state:remote-e2e-terraform-state:schedule:continuous-20260521T150000Z",
		TriggerKind:        workflow.TriggerKindSchedule,
		Status:             workflow.RunStatusCollectionPending,
		RequestedScopeSet:  "[]",
		RequestedCollector: string(scope.CollectorTerraformState),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	item := workflow.WorkItem{
		WorkItemID:          "terraform-state:remote-e2e-terraform-state:gen-new",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "remote-e2e-terraform-state",
		SourceSystem:        string(scope.CollectorTerraformState),
		ScopeID:             "state_snapshot:s3:5b2644e084cde2b3010ecac110aaab176df3b4e4cac223b244841d6f103feb0c",
		AcceptanceUnitID:    "repository:r_bff45784",
		SourceRunID:         "terraform_state_candidate:s3:new",
		GenerationID:        "terraform_state_candidate:s3:new",
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
			t.Fatalf("guarded schedule inserted new target for terminal same run: %s", exec.query)
		}
	}
	if !db.committed {
		t.Fatal("guarded schedule did not commit skipped transaction")
	}
}

type fakeBeginnerExecQueryer struct {
	fakeExecQueryer
	committed  bool
	rolledBack bool
}

func (f *fakeBeginnerExecQueryer) Begin(context.Context) (Transaction, error) {
	return &fakeTransaction{owner: f}, nil
}

type fakeTransaction struct {
	owner *fakeBeginnerExecQueryer
}

func (tx *fakeTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.owner.ExecContext(ctx, query, args...)
}

func (tx *fakeTransaction) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.owner.QueryContext(ctx, query, args...)
}

func (tx *fakeTransaction) Commit() error {
	tx.owner.committed = true
	return nil
}

func (tx *fakeTransaction) Rollback() error {
	tx.owner.rolledBack = true
	return nil
}

type terminalRunReplayDB struct {
	fakeBeginnerExecQueryer
}

func (db *terminalRunReplayDB) Begin(context.Context) (Transaction, error) {
	return &terminalRunReplayTx{owner: db}, nil
}

func (db *terminalRunReplayDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.mu.Lock()
	db.queries = append(db.queries, fakeQueryCall{query: query, args: args})
	db.mu.Unlock()

	switch {
	case strings.Contains(query, "FROM workflow_runs") && strings.Contains(query, "status IN ('complete', 'failed')"):
		return &queueFakeRows{rows: [][]any{{true}}}, nil
	case strings.Contains(query, "JOIN workflow_runs") && strings.Contains(query, "run.status NOT IN"):
		return &queueFakeRows{rows: [][]any{{false}}}, nil
	case strings.Contains(query, "FROM workflow_work_items AS item") && strings.Contains(query, "item.run_id = $1"):
		return &queueFakeRows{rows: [][]any{{false}}}, nil
	default:
		return nil, sql.ErrNoRows
	}
}

type terminalRunReplayTx struct {
	owner *terminalRunReplayDB
}

func (tx *terminalRunReplayTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return tx.owner.ExecContext(ctx, query, args...)
}

func (tx *terminalRunReplayTx) QueryContext(ctx context.Context, query string, args ...any) (Rows, error) {
	return tx.owner.QueryContext(ctx, query, args...)
}

func (tx *terminalRunReplayTx) Commit() error {
	tx.owner.committed = true
	return nil
}

func (tx *terminalRunReplayTx) Rollback() error {
	tx.owner.rolledBack = true
	return nil
}
