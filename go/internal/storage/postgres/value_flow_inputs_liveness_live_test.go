// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestValueFlowInputsLivenessFenceSharesSnapshot proves the #6923 value-flow
// refresh fence against a live Postgres: an open runs_in shared-projection
// intent on the ACTIVE generation refuses; completing it clears the fence; an
// open intent on a SUPERSEDED generation does not hold; a dead_letter
// iam_can_perform_materialization work item does not hold the fence on a
// SUPERSEDED generation (that generation will never run) nor on the ACTIVE
// one (a dead-lettered producer only contributes again when an operator
// replays it, and that replay re-triggers the refresh); the same row set to
// retrying on the ACTIVE generation refuses.
//
// Skipped by default; set ESHU_VALUE_FLOW_REFRESH_LIVE=1 and
// ESHU_POSTGRES_DSN. Every seeded id is uniquely prefixed per run so
// parallel databases never collide.
func TestValueFlowInputsLivenessFenceSharesSnapshot(t *testing.T) {
	if strings.TrimSpace(os.Getenv("ESHU_VALUE_FLOW_REFRESH_LIVE")) == "" {
		t.Skip("set ESHU_VALUE_FLOW_REFRESH_LIVE=1 and ESHU_POSTGRES_DSN to run the value-flow inputs liveness proof")
	}
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open postgres: %v", err)
	}
	defer func() { _ = database.Close() }()

	prefix := fmt.Sprintf("vfinputs-live-%d", time.Now().UnixNano())
	now := time.Now().UTC().Truncate(time.Millisecond)

	scope := prefix + "-scope"
	genPrior, genActive := prefix+"-gen1", prefix+"-gen2"
	if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes
  (scope_id, scope_kind, source_system, source_key, collector_kind,
   partition_key, observed_at, ingested_at, status, active_generation_id, payload)
VALUES ($1, 'aws', 'aws', $1, 'aws', $1, $2, $2, 'active', $3, '{}'::jsonb)`,
		scope, now, genActive); err != nil {
		t.Fatalf("seed scope: %v", err)
	}
	for gen, status := range map[string]string{genPrior: "superseded", genActive: "active"} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations
  (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'manual', $3, $3, $4, $3)`, gen, scope, now, status); err != nil {
			t.Fatalf("seed generation %s: %v", gen, err)
		}
	}
	defer func() {
		_, _ = database.ExecContext(ctx, `DELETE FROM ingestion_scopes WHERE scope_id = $1`, scope)
	}()

	probe := func() ([]string, error) {
		t.Helper()
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		return ValueFlowInputsLivenessStore{DB: SQLTx{Tx: tx}}.PendingValueFlowInputs(ctx)
	}

	// Baseline: nothing seeded yet, fence must be clear.
	if pending, err := probe(); err != nil || len(pending) != 0 {
		t.Fatalf("baseline probe = %v, %v; want empty, nil", pending, err)
	}

	// An open runs_in shared-projection intent refuses.
	intentID := prefix + "-runs-in-intent"
	if _, err := database.ExecContext(ctx, `
INSERT INTO shared_projection_intents
  (intent_id, projection_domain, partition_key, scope_id, repository_id, source_run_id,
   generation_id, payload, created_at, completed_at)
VALUES ($1, 'runs_in', $2, $3, 'repo-1', 'run-1', $4, '{}'::jsonb, $5, NULL)`,
		intentID, prefix+"-partition", scope, genActive, now); err != nil {
		t.Fatalf("seed runs_in intent: %v", err)
	}
	pending, err := probe()
	if err != nil {
		t.Fatalf("probe after open runs_in intent: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("fence must refuse while a runs_in intent is open")
	}

	// Completing it clears the fence.
	if _, err := database.ExecContext(ctx,
		`UPDATE shared_projection_intents SET completed_at = $2 WHERE intent_id = $1`, intentID, now); err != nil {
		t.Fatalf("complete runs_in intent: %v", err)
	}
	if pending, err := probe(); err != nil || len(pending) != 0 {
		t.Fatalf("probe after completing runs_in intent = %v, %v; want empty, nil", pending, err)
	}

	// An open runs_in intent left on a SUPERSEDED generation does not hold
	// the fence: the shared half joins the scope's active generation too.
	staleIntentID := prefix + "-runs-in-intent-superseded"
	if _, err := database.ExecContext(ctx, `
INSERT INTO shared_projection_intents
  (intent_id, projection_domain, partition_key, scope_id, repository_id, source_run_id,
   generation_id, payload, created_at, completed_at)
VALUES ($1, 'runs_in', $2, $3, 'repo-1', 'run-1', $4, '{}'::jsonb, $5, NULL)`,
		staleIntentID, prefix+"-partition-stale", scope, genPrior, now); err != nil {
		t.Fatalf("seed superseded runs_in intent: %v", err)
	}
	if pending, err := probe(); err != nil || len(pending) != 0 {
		t.Fatalf("probe with open runs_in intent on a superseded generation = %v, %v; want empty, nil", pending, err)
	}

	// A dead_letter iam_can_perform_materialization row on the SUPERSEDED
	// generation must not hold the fence.
	workID := prefix + "-iam-work"
	seedWork := func(gen, status string) {
		t.Helper()
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, created_at, updated_at)
VALUES ($1, $2, $3, 'reducer', $4, $5, $6, $6)
ON CONFLICT (work_item_id) DO UPDATE SET generation_id = EXCLUDED.generation_id, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			workID, scope, gen, reducercontract.DomainIAMCanPerformMaterialization, status, now); err != nil {
			t.Fatalf("seed work item gen=%s status=%s: %v", gen, status, err)
		}
	}
	seedWork(genPrior, "dead_letter")
	if pending, err := probe(); err != nil || len(pending) != 0 {
		t.Fatalf("probe with dead_letter on superseded generation = %v, %v; want empty, nil (not the active generation)", pending, err)
	}

	// The same row on the ACTIVE generation does not hold the fence either:
	// failed and dead_letter are outside valueFlowInputsFenceStatusList.
	seedWork(genActive, "dead_letter")
	if pending, err := probe(); err != nil || len(pending) != 0 {
		t.Fatalf("probe with dead_letter on active generation = %v, %v; want empty, nil (dead-lettered producers do not hold the fence)", pending, err)
	}
	// A retrying row on the ACTIVE generation refuses.
	seedWork(genActive, "retrying")
	pending, err = probe()
	if err != nil {
		t.Fatalf("probe with retrying row on active generation: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("fence must refuse while a retrying row sits on the active generation")
	}
}
