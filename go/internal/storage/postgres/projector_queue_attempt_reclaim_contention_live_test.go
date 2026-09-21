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

// TestProjectorQueueRejectsAttemptReclaimedDuringLockWait proves that a stale
// worker cannot change work, generation, or scope after a same-owner reclaim
// commits while the worker is blocked on the work row.
func TestProjectorQueueRejectsAttemptReclaimedDuringLockWait(t *testing.T) {
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
		{name: "terminal_fail", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			return queue.Fail(ctx, work, errors.New("stale terminal failure"))
		}},
		{name: "retry_fail", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			queue.MaxAttempts = 4
			return queue.Fail(ctx, work, &retryableTestError{message: "stale retry"})
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			workerDB := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, workerDB, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-reclaim', 'repository', 'github', 'proof/reclaim', 'git',
          'proof/reclaim', now(), now(), 'active', 'gen-old');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES
    ('gen-old', 'scope-reclaim', 'push', now() - interval '2 hours',
        now() - interval '2 hours', 'active', now() - interval '2 hours'),
    ('gen-new', 'scope-reclaim', 'push', now() - interval '1 hour',
        now() - interval '1 hour', 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('work-old', 'scope-reclaim', 'gen-old', 'projector',
          'source_local', 'running', 2, 'proof-worker',
          now() + interval '2 minutes', now(), '{}'::jsonb, now(), now());
`)
			reclaimDB := openLivenessProofDB(t, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			var schemaName string
			if err := workerDB.QueryRowContext(ctx, "SHOW search_path").Scan(&schemaName); err != nil {
				t.Fatalf("read worker search_path: %v", err)
			}
			if _, err := reclaimDB.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
				t.Fatalf("set reclaim search_path: %v", err)
			}
			reclaimTx, err := reclaimDB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin reclaim tx: %v", err)
			}
			defer func() { _ = reclaimTx.Rollback() }()
			if _, err := reclaimTx.ExecContext(ctx, `
UPDATE fact_work_items SET attempt_count = 3
WHERE work_item_id = 'work-old' AND attempt_count = 2
`); err != nil {
				t.Fatalf("reclaim same-owner work: %v", err)
			}
			var workerPID int
			if err := workerDB.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&workerPID); err != nil {
				t.Fatalf("read worker backend pid: %v", err)
			}
			queue := NewProjectorQueue(SQLDB{DB: workerDB}, "proof-worker", time.Minute)
			work := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-reclaim"},
				Generation:   scope.ScopeGeneration{GenerationID: "gen-old"},
				AttemptCount: 2,
			}
			operationDone := make(chan error, 1)
			go func() { operationDone <- operation.run(queue, ctx, work) }()

			blocked := false
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case err := <-operationDone:
					t.Fatalf("%s returned before reclaim commit: %v", operation.name, err)
				default:
				}
				var waitType sql.NullString
				if err := reclaimTx.QueryRowContext(ctx,
					"SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1", workerPID,
				).Scan(&waitType); err != nil {
					t.Fatalf("inspect worker wait: %v", err)
				}
				if waitType.Valid && waitType.String == "Lock" {
					blocked = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatalf("%s did not wait on reclaimed work row", operation.name)
			}
			if err := reclaimTx.Commit(); err != nil {
				t.Fatalf("commit reclaim: %v", err)
			}
			select {
			case err := <-operationDone:
				if !errors.Is(err, ErrProjectorClaimRejected) {
					t.Fatalf("%s after reclaim = %v; want claim rejection", operation.name, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("%s did not complete after reclaim", operation.name)
			}
			var generationStatus, workStatus, leaseOwner string
			var activePointer sql.NullString
			var attemptCount int
			if err := workerDB.QueryRowContext(ctx, `
SELECT generation.status, scope.active_generation_id,
       work.status, work.attempt_count, work.lease_owner
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation ON generation.generation_id = 'gen-old'
JOIN fact_work_items AS work ON work.work_item_id = 'work-old'
WHERE scope.scope_id = 'scope-reclaim'
`).Scan(&generationStatus, &activePointer, &workStatus, &attemptCount, &leaseOwner); err != nil {
				t.Fatalf("read state after %s: %v", operation.name, err)
			}
			if generationStatus != "active" || !activePointer.Valid ||
				activePointer.String != "gen-old" || workStatus != "running" ||
				attemptCount != 3 || leaseOwner != "proof-worker" {
				t.Fatalf("%s changed successor-owned state: generation=%q pointer=%v work=%q attempt=%d owner=%q",
					operation.name, generationStatus, activePointer, workStatus, attemptCount, leaseOwner)
			}
		})
	}
}
