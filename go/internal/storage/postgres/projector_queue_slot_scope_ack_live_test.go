// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9432 queue-layer acceptance that two consecutive planning windows leave exactly one active generation per pinned slot scope: two slot scopes each carry a window-one active generation and a window-two pending generation, acking window two supersedes window one and repoints the scope; untagged and gated by the ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN env Skip; Postgres only; about 0.1s locally; enrolled in the live-postgres-readiness runner
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestProjectorAckLeavesOneActiveGenerationPerSlotScope is the #9432
// acceptance at the queue layer: two consecutive planning windows mint the
// same pinned slot-scope set for different target sets (proven planner-side
// by TestSlotScopesStableAcrossWindows), so acking the second window's
// generations through the shipped exact-scope supersede leaves exactly one
// active generation per slot scope.
func TestProjectorAckLeavesOneActiveGenerationPerSlotScope(t *testing.T) {
	database := openLivenessProofDB(t, supersessionProofDSN(t))
	provisionLivenessSchema(t, database, "")
	ctx := context.Background()
	oldTime := time.Date(2026, time.October, 29, 12, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Minute)

	slotScopes := []string{
		"vuln-intel://osv/npm/_slot/3/0",
		"vuln-intel://osv/npm/_slot/500/0",
	}
	// The queue keys generations by scope id string, so the slot scope ids
	// the planner mints flow through Ack verbatim; the planner-side
	// stability test proves both windows mint the same strings.
	for i, scopeID := range slotScopes {
		oldGen, newGen := fmt.Sprintf("gen-w1-%d", i), fmt.Sprintf("gen-w2-%d", i)
		if _, err := database.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ($1, 'vulnerability_intelligence', 'vuln-intel', $1, 'vulnerability_intelligence',
          $1, $2, $2, 'active', $3)`,
			scopeID, oldTime, oldGen); err != nil {
			t.Fatalf("insert scope: %v", err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES
    ($1, $4, 'schedule', $2, $2, 'active', $2),
    ($3, $4, 'schedule', $5, $5, 'pending', NULL)`,
			oldGen, oldTime, newGen, scopeID, newTime); err != nil {
			t.Fatalf("insert generations: %v", err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at
) VALUES
    ($1, $4, $5, 'projector', 'source_local',
        'completed', 1, 'proof-worker', $2, $3, '{}'::jsonb, $3, $3),
    ($6, $4, $7, 'projector', 'source_local',
        'running', 1, 'proof-worker', $2, $3, '{}'::jsonb, $3, $3)`,
			fmt.Sprintf("work-w1-%d", i), newTime.Add(time.Minute), oldTime,
			scopeID, oldGen, fmt.Sprintf("work-w2-%d", i), newGen); err != nil {
			t.Fatalf("insert work: %v", err)
		}
	}

	// Acking window two must supersede window one per slot scope.
	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	queue.Now = func() time.Time { return newTime }
	for i, scopeID := range slotScopes {
		work := projector.ScopeGenerationWork{
			Scope:        scope.IngestionScope{ScopeID: scopeID},
			Generation:   scope.ScopeGeneration{GenerationID: fmt.Sprintf("gen-w2-%d", i)},
			AttemptCount: 1,
		}
		if err := queue.Ack(ctx, work, runtime.Result{}); err != nil {
			t.Fatalf("Ack window-two generation for %s: %v", scopeID, err)
		}
	}

	for i, scopeID := range slotScopes {
		oldGen, newGen := fmt.Sprintf("gen-w1-%d", i), fmt.Sprintf("gen-w2-%d", i)
		var activeCount int
		var pointer string
		var w1Status, w2Status string
		if err := database.QueryRowContext(ctx, `
SELECT
    (SELECT COUNT(*) FROM scope_generations WHERE scope_id = $1 AND status = 'active'),
    (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1),
    (SELECT status FROM scope_generations WHERE scope_id = $1 AND generation_id = $2),
    (SELECT status FROM scope_generations WHERE scope_id = $1 AND generation_id = $3)`,
			scopeID, oldGen, newGen).Scan(&activeCount, &pointer, &w1Status, &w2Status); err != nil {
			t.Fatalf("read scope state: %v", err)
		}
		if activeCount != 1 || pointer != newGen || w1Status != "superseded" || w2Status != "active" {
			t.Errorf("scope %s: active=%d pointer=%q w1=%q w2=%q, want 1 gen-w2 superseded active",
				scopeID, activeCount, pointer, w1Status, w2Status)
		}
	}
}
