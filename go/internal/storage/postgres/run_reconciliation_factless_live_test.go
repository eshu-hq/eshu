// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9257 disposable PostgreSQL proof that reconcile converges a run whose completed claim committed no facts and holds a run whose committed facts are not yet published; the companion hermetic shape test pins the index prefixes; enrolled in the live-postgres-readiness runner; sub-second local proof on a throwaway database
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Real-Postgres regression test for #9257: a completed claim that committed
// no facts must converge its run instead of wedging in reducer_converging
// forever. On the QA environment one terraform-state claim completed collection
// with an empty merged result, so ingestion skipped the generation entirely:
// no fact_records rows, no fact_work_items rows. The run sat in
// reducer_converging for 8+ hours while every 30-minute schedule tick
// skipped as target_already_planned, blocking all future schedules.
//
// Run with a throwaway Postgres:
//
//	ESHU_POSTGRES_DSN='postgres://eshu:change-me@localhost:<port>/eshu?sslmode=disable' \
//	  go test ./internal/storage/postgres -run 'FactlessClaim' -count=1 -v
func TestWorkflowControlStoreIntegrationReconcileConvergesFactlessClaim(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.October, 27, 13, 0, 0, 0, time.UTC)

	run := workflow.Run{
		RunID:       "integration-run-factless",
		TriggerKind: workflow.TriggerKindSchedule,
		Status:      workflow.RunStatusReducerConverging,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	mustCreateRun(t, store, ctx, run)
	// The completed item's generation committed nothing: no fact_records,
	// no fact_work_items, no scope_generations row. Only the work-item row
	// exists, exactly like the wedged QA run.
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-factless",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "terraform-state-cx-integration",
		ScopeID:             "scope-factless-integration",
		GenerationID:        "component-generation:integration-factless",
		Status:              workflow.WorkItemStatusCompleted,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now.Add(-time.Hour),
	})

	reconciled, err := store.ReconcileWorkflowRuns(ctx, now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	mustWorkflowRunStatus(t, db, run.RunID, workflow.RunStatusComplete)
	for _, keyspace := range []gpphase.Keyspace{
		gpphase.KeyspaceTerraformResourceUID,
		gpphase.KeyspaceTerraformModuleUID,
	} {
		mustCompletenessStatus(
			t,
			db,
			run.RunID,
			string(scope.CollectorTerraformState),
			string(keyspace),
			string(gpphase.PhaseCanonicalNodesCommitted),
			workflow.CompletenessStatusReady,
		)
	}
	var detail string
	if err := db.QueryRowContext(ctx, `
SELECT detail
FROM workflow_run_completeness
WHERE run_id = $1
LIMIT 1
`, run.RunID).Scan(&detail); err != nil {
		t.Fatalf("query completeness detail error = %v, want nil", err)
	}
	if !strings.Contains(detail, "factless") {
		t.Fatalf("completeness detail = %q, want it to name the factless convergence", detail)
	}
}

// The fail-closed companion: a completed item whose generation DID commit
// facts must stay in reducer_converging until the reducer publishes them,
// even when no queue rows are visible yet. Converging that case would hide
// data loss behind a green run.
func TestWorkflowControlStoreIntegrationReconcileHoldsFactCommittedClaim(t *testing.T) {
	db, store := openWorkflowControlIntegrationStore(t)

	ctx := context.Background()
	now := time.Date(2026, time.October, 27, 13, 0, 0, 0, time.UTC)

	run := workflow.Run{
		RunID:       "integration-run-facts-pending",
		TriggerKind: workflow.TriggerKindSchedule,
		Status:      workflow.RunStatusReducerConverging,
		CreatedAt:   now.Add(-time.Hour),
		UpdatedAt:   now.Add(-time.Hour),
	}
	mustCreateRun(t, store, ctx, run)
	mustEnqueueWorkItem(t, store, ctx, workflow.WorkItem{
		WorkItemID:          "integration-item-facts-pending",
		RunID:               run.RunID,
		CollectorKind:       scope.CollectorTerraformState,
		CollectorInstanceID: "terraform-state-cx-integration",
		ScopeID:             "scope-factless-integration-facts",
		GenerationID:        "component-generation:integration-facts-pending",
		Status:              workflow.WorkItemStatusCompleted,
		CreatedAt:           now.Add(-time.Hour),
		UpdatedAt:           now.Add(-time.Hour),
	})
	// One committed fact, no queue row, no phase row: projection is still
	// legitimately in flight (or lost upstream), so the run must hold.
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ('scope-factless-integration-facts', 'terraform_state', 'terraform_state', 'scope-factless-integration-facts', 'terraform_state', '', $1, $1, 'active', 'component-generation:integration-facts-pending', '{}'::jsonb)
`, now); err != nil {
		t.Fatalf("insert scope error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, observed_at, ingested_at, status, trigger_kind, freshness_hint)
VALUES ('component-generation:integration-facts-pending', 'scope-factless-integration-facts', $1, $1, 'accepted', 'scheduled', '')
`, now); err != nil {
		t.Fatalf("insert generation error = %v, want nil", err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, fencing_token, source_confidence,
    source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload
) VALUES (
    'fact-integration-facts-pending', 'scope-factless-integration-facts',
    'component-generation:integration-facts-pending', 'terraform_state_resource',
    'record:aws_vpc.default', '1.0.0', 'terraform_state', 0, 'high',
    'terraform_state', 'aws_vpc.default', $1, $1, FALSE, '{}'
)`, now); err != nil {
		t.Fatalf("insert committed fact error = %v, want nil", err)
	}

	reconciled, err := store.ReconcileWorkflowRuns(ctx, now)
	if err != nil {
		t.Fatalf("ReconcileWorkflowRuns() error = %v, want nil", err)
	}
	if got, want := reconciled, 1; got != want {
		t.Fatalf("reconciled = %d, want %d", got, want)
	}
	mustWorkflowRunStatus(t, db, run.RunID, workflow.RunStatusReducerConverging)
}
