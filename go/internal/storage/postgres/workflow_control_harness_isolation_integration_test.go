// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestWorkflowControlHarnessLeavesSharedSchemaUntouched is the #7489 regression.
// The workflow-control and dead-letter integration harnesses used to empty the
// shared reducer tables with TRUNCATE ... CASCADE, which also deleted the
// eshu:global scope, generation and work item migration 115 seeds and the phase
// row migration 116 adds. The migration ledger marks both applied, so nothing
// re-seeds them and every later proof that expects the standing global anchor
// failed on the second run against one database.
//
// The harness must run on a schema of its own: opening it, writing through it
// and closing it leave the shared schema's scope and phase-state rows exactly as
// they were. The check reads the shared rows before and after rather than
// asserting a seed count, so it holds on a database another proof already
// touched.
func TestWorkflowControlHarnessLeavesSharedSchemaUntouched(t *testing.T) {
	dsn := os.Getenv(workflowControlIntegrationDSNEnv)
	if dsn == "" {
		t.Skip(workflowControlIntegrationDSNEnv + " is not set; skipping Postgres integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open shared database: %v", err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	if err := ApplyBootstrap(ctx, SQLDB{DB: shared}); err != nil {
		t.Fatalf("apply bootstrap to the shared schema: %v", err)
	}
	before := sharedReducerRowCounts(ctx, t, shared)
	if before.globalScope == 0 {
		t.Fatalf("shared schema has no eshu:global scope row to protect; an earlier run already wiped the seed migrations 115 and 116 add, so recreate the database")
	}

	db, _ := openDeadLetterBridgeIntegrationStore(t)
	now := time.Now().UTC()
	mustUpsertScopeBoundary(t, db, scope.IngestionScope{
		ScopeID:       "harness-isolation-scope",
		SourceSystem:  "github",
		ScopeKind:     scope.KindRepository,
		CollectorKind: scope.CollectorGit,
		PartitionKey:  "harness-isolation",
	}, scope.ScopeGeneration{
		GenerationID: "harness-isolation-generation",
		ScopeID:      "harness-isolation-scope",
		ObservedAt:   now,
		IngestedAt:   now,
		Status:       scope.GenerationStatusCompleted,
		TriggerKind:  scope.TriggerKindSnapshot,
	})
	mustInsertFactWorkItem(t, db, factWorkItemFixture{
		WorkItemID:   "harness-isolation-probe",
		ScopeID:      "harness-isolation-scope",
		GenerationID: "harness-isolation-generation",
		Stage:        "reducer",
		Domain:       "workload_materialization",
		Status:       "pending",
		CreatedAt:    now,
	})

	if after := sharedReducerRowCounts(ctx, t, shared); after != before {
		t.Fatalf("shared schema changed while the harness ran: before=%+v after=%+v, want identical", before, after)
	}
	var schema string
	if err := db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("read harness schema: %v", err)
	}
	if schema == "public" {
		t.Fatalf("harness runs in schema %q, want a schema of its own", schema)
	}
}

// sharedReducerCounts holds the rows of the shared schema this regression
// guards: the standing eshu:global scope (migration 115), its phase rows
// (migration 116), and the probe work item the harness writes. Only these are
// counted so a parallel test that writes other scopes into the shared schema
// cannot make the check fail.
type sharedReducerCounts struct {
	globalScope int
	phaseRows   int
	workItems   int
}

func sharedReducerRowCounts(ctx context.Context, t *testing.T, shared *sql.DB) sharedReducerCounts {
	t.Helper()
	var counts sharedReducerCounts
	if err := shared.QueryRowContext(ctx, `
SELECT
    (SELECT count(*) FROM ingestion_scopes WHERE scope_id = 'eshu:global'),
    (SELECT count(*) FROM graph_projection_phase_state WHERE scope_id = 'eshu:global'),
    (SELECT count(*) FROM fact_work_items WHERE work_item_id = 'harness-isolation-probe')
`).Scan(&counts.globalScope, &counts.phaseRows, &counts.workItems); err != nil {
		t.Fatalf("count shared reducer rows: %v", err)
	}
	return counts
}
