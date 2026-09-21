// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestProjectorQueueRejectsReclaimedSameOwnerAttempt proves that a worker from
// an earlier claim cannot alter a row reclaimed by the same process identity.
func TestProjectorQueueRejectsReclaimedSameOwnerAttempt(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	operations := []struct {
		name string
		run  func(ProjectorQueue, context.Context, projector.ScopeGenerationWork) error
	}{
		{name: "heartbeat", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			return queue.Heartbeat(ctx, work)
		}},
		{name: "ack", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			return queue.Ack(ctx, work, runtime.Result{})
		}},
		{name: "fail", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			return queue.Fail(ctx, work, errors.New("stale attempt failure"))
		}},
		{name: "retry_fail", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			queue.MaxAttempts = 3
			return queue.Fail(ctx, work, &retryableTestError{message: "stale attempt retry"})
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			database := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, database, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-attempt', 'repository', 'github', 'proof/attempt', 'git',
          'proof/attempt', now(), now(), 'active', 'gen-old');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES
    ('gen-old', 'scope-attempt', 'push', now() - interval '2 hours',
        now() - interval '2 hours', 'active', now() - interval '2 hours'),
    ('gen-new', 'scope-attempt', 'push', now() - interval '1 hour',
        now() - interval '1 hour', 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('work-old', 'scope-attempt', 'gen-old', 'projector',
          'source_local', 'running', 2, 'proof-worker',
          now() + interval '2 minutes', now(), '{}'::jsonb, now(), now());
`)
			ctx := context.Background()
			queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
			work := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-attempt"},
				Generation:   scope.ScopeGeneration{GenerationID: "gen-old"},
				AttemptCount: 1,
			}
			if err := operation.run(queue, ctx, work); !errors.Is(err, ErrProjectorClaimRejected) {
				t.Fatalf("%s error = %v; want stale claim rejection", operation.name, err)
			}
			var generationStatus, workStatus, leaseOwner string
			var activePointer sql.NullString
			var attemptCount int
			if err := database.QueryRowContext(ctx, `
SELECT generation.status, scope.active_generation_id,
       work.status, work.attempt_count, work.lease_owner
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation ON generation.generation_id = 'gen-old'
JOIN fact_work_items AS work ON work.work_item_id = 'work-old'
WHERE scope.scope_id = 'scope-attempt'
`).Scan(&generationStatus, &activePointer, &workStatus, &attemptCount, &leaseOwner); err != nil {
				t.Fatalf("read state after stale %s: %v", operation.name, err)
			}
			if generationStatus != "active" || !activePointer.Valid ||
				activePointer.String != "gen-old" || workStatus != "running" ||
				attemptCount != 2 || leaseOwner != "proof-worker" {
				t.Fatalf("%s changed successor-owned state: generation=%q pointer=%v work=%q attempt=%d owner=%q",
					operation.name, generationStatus, activePointer, workStatus, attemptCount, leaseOwner)
			}
		})
	}
}
