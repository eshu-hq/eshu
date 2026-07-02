// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const workflowControlIntegrationDSNEnv = "ESHU_POSTGRES_DSN"

func openWorkflowControlIntegrationStore(t *testing.T) (*sql.DB, *WorkflowControlStore) {
	t.Helper()

	dsn := os.Getenv(workflowControlIntegrationDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping Postgres integration test", workflowControlIntegrationDSNEnv)
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("PingContext() error = %v, want nil", err)
	}

	store := NewWorkflowControlStore(SQLDB{DB: db})
	if err := store.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx, `
TRUNCATE workflow_claims, workflow_work_items, workflow_runs, collector_instances, workflow_run_completeness
RESTART IDENTITY CASCADE
`); err != nil {
		_ = db.Close()
		t.Fatalf("TRUNCATE workflow control tables error = %v, want nil", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db, store
}

func mustCreateRun(t *testing.T, store *WorkflowControlStore, ctx context.Context, run workflow.Run) {
	t.Helper()
	if err := store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun() error = %v, want nil", err)
	}
}

func mustUpsertScopeBoundary(t *testing.T, db *sql.DB, scopeValue scope.IngestionScope, generation scope.ScopeGeneration) {
	t.Helper()
	if err := upsertIngestionScope(context.Background(), SQLDB{DB: db}, scopeValue, generation); err != nil {
		t.Fatalf("upsertIngestionScope() error = %v, want nil", err)
	}
	if err := upsertScopeGeneration(context.Background(), SQLDB{DB: db}, generation); err != nil {
		t.Fatalf("upsertScopeGeneration() error = %v, want nil", err)
	}
}

func mustEnqueueWorkItem(t *testing.T, store *WorkflowControlStore, ctx context.Context, item workflow.WorkItem) {
	t.Helper()
	if item.SourceSystem == "" {
		item.SourceSystem = string(item.CollectorKind)
	}
	if item.GenerationID == "" {
		item.GenerationID = item.WorkItemID + "-generation"
	}
	if item.SourceRunID == "" {
		item.SourceRunID = item.GenerationID
	}
	if item.AcceptanceUnitID == "" {
		item.AcceptanceUnitID = item.ScopeID
	}
	if err := store.EnqueueWorkItems(ctx, []workflow.WorkItem{item}); err != nil {
		t.Fatalf("EnqueueWorkItems() error = %v, want nil", err)
	}
}

func mustHeartbeatClaim(t *testing.T, store *WorkflowControlStore, ctx context.Context, mutation workflow.ClaimMutation) {
	t.Helper()
	if err := store.HeartbeatClaim(ctx, mutation); err != nil {
		t.Fatalf("HeartbeatClaim() error = %v, want nil", err)
	}
}

func mustClaimState(t *testing.T, db *sql.DB, claimID string, wantStatus workflow.ClaimStatus, wantFence int64) {
	t.Helper()

	var gotStatus string
	var gotFence int64
	if err := db.QueryRowContext(context.Background(), `
SELECT status, fencing_token
FROM workflow_claims
WHERE claim_id = $1
`, claimID).Scan(&gotStatus, &gotFence); err != nil {
		t.Fatalf("query claim %q error = %v, want nil", claimID, err)
	}
	if gotStatus != string(wantStatus) {
		t.Fatalf("claim %q status = %q, want %q", claimID, gotStatus, wantStatus)
	}
	if gotFence != wantFence {
		t.Fatalf("claim %q fencing_token = %d, want %d", claimID, gotFence, wantFence)
	}
}

func mustHeartbeatLeaseState(
	t *testing.T,
	db *sql.DB,
	claimID string,
	workItemID string,
	wantHeartbeatAt time.Time,
	wantLeaseExpiresAt time.Time,
) {
	t.Helper()

	var gotClaimHeartbeatAt time.Time
	var gotClaimLeaseExpiresAt time.Time
	if err := db.QueryRowContext(context.Background(), `
SELECT heartbeat_at, lease_expires_at
FROM workflow_claims
WHERE claim_id = $1
`, claimID).Scan(&gotClaimHeartbeatAt, &gotClaimLeaseExpiresAt); err != nil {
		t.Fatalf("query heartbeat claim %q error = %v, want nil", claimID, err)
	}
	if !gotClaimHeartbeatAt.Equal(wantHeartbeatAt) {
		t.Fatalf("claim %q heartbeat_at = %v, want %v", claimID, gotClaimHeartbeatAt, wantHeartbeatAt)
	}
	if !gotClaimLeaseExpiresAt.Equal(wantLeaseExpiresAt) {
		t.Fatalf("claim %q lease_expires_at = %v, want %v", claimID, gotClaimLeaseExpiresAt, wantLeaseExpiresAt)
	}

	var gotWorkItemLeaseExpiresAt time.Time
	if err := db.QueryRowContext(context.Background(), `
SELECT lease_expires_at
FROM workflow_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&gotWorkItemLeaseExpiresAt); err != nil {
		t.Fatalf("query work item %q lease error = %v, want nil", workItemID, err)
	}
	if !gotWorkItemLeaseExpiresAt.Equal(wantLeaseExpiresAt) {
		t.Fatalf("work item %q lease_expires_at = %v, want %v", workItemID, gotWorkItemLeaseExpiresAt, wantLeaseExpiresAt)
	}
}

func mustWorkflowRunStatus(t *testing.T, db *sql.DB, runID string, wantStatus workflow.RunStatus) {
	t.Helper()
	var gotStatus string
	if err := db.QueryRowContext(context.Background(), `
SELECT status
FROM workflow_runs
WHERE run_id = $1
`, runID).Scan(&gotStatus); err != nil {
		t.Fatalf("query workflow run %q error = %v, want nil", runID, err)
	}
	if gotStatus != string(wantStatus) {
		t.Fatalf("workflow run %q status = %q, want %q", runID, gotStatus, wantStatus)
	}
}

func mustCompletenessStatus(
	t *testing.T,
	db *sql.DB,
	runID string,
	collectorKind string,
	keyspace string,
	phaseName string,
	wantStatus string,
) {
	t.Helper()
	var gotStatus string
	if err := db.QueryRowContext(context.Background(), `
SELECT status
FROM workflow_run_completeness
WHERE run_id = $1
  AND collector_kind = $2
  AND keyspace = $3
  AND phase_name = $4
`, runID, collectorKind, keyspace, phaseName).Scan(&gotStatus); err != nil {
		t.Fatalf("query workflow completeness %q/%q/%q error = %v, want nil", collectorKind, keyspace, phaseName, err)
	}
	if gotStatus != wantStatus {
		t.Fatalf("workflow completeness %q/%q/%q status = %q, want %q", collectorKind, keyspace, phaseName, gotStatus, wantStatus)
	}
}

func mustWorkItemState(t *testing.T, db *sql.DB, workItemID string, wantStatus workflow.WorkItemStatus, wantClaimID string, wantFence int64) {
	t.Helper()

	var gotStatus, gotClaimID string
	var gotFence int64
	if err := db.QueryRowContext(context.Background(), `
SELECT status, COALESCE(current_claim_id, ''), current_fencing_token
FROM workflow_work_items
WHERE work_item_id = $1
`, workItemID).Scan(&gotStatus, &gotClaimID, &gotFence); err != nil {
		t.Fatalf("query work item %q error = %v, want nil", workItemID, err)
	}
	if gotStatus != string(wantStatus) {
		t.Fatalf("work item %q status = %q, want %q", workItemID, gotStatus, wantStatus)
	}
	if gotClaimID != wantClaimID {
		t.Fatalf("work item %q current_claim_id = %q, want %q", workItemID, gotClaimID, wantClaimID)
	}
	if gotFence != wantFence {
		t.Fatalf("work item %q current_fencing_token = %d, want %d", workItemID, gotFence, wantFence)
	}
}

func markClaimExpired(t *testing.T, db *sql.DB, claimID, workItemID, ownerID string, fencingToken int64, expiredAt time.Time) {
	t.Helper()

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `
UPDATE workflow_claims
SET lease_expires_at = $1,
    updated_at = $1
WHERE claim_id = $2
  AND work_item_id = $3
  AND owner_id = $4
  AND fencing_token = $5
`, expiredAt, claimID, workItemID, ownerID, fencingToken); err != nil {
		t.Fatalf("expire claim %q error = %v, want nil", claimID, err)
	}
	if _, err := db.ExecContext(ctx, `
UPDATE workflow_work_items
SET lease_expires_at = $1,
    updated_at = $1
WHERE work_item_id = $2
  AND current_claim_id = $3
  AND current_owner_id = $4
  AND current_fencing_token = $5
`, expiredAt, workItemID, claimID, ownerID, fencingToken); err != nil {
		t.Fatalf("expire work item %q error = %v, want nil", workItemID, err)
	}
}
