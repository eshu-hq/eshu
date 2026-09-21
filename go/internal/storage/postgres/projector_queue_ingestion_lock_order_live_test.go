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

// TestProjectorCompletionDoesNotDeadlockSameGenerationCommit proves that a
// same-generation ingestion commit and projector completion can finish while
// competing for the scope, generation, and deterministic projector work row.
// Heartbeat never waits on the scope row, so its non-blocking contract is
// proven by TestProjectorHeartbeatRenewsLeaseWhileIngestionHoldsScope.
func TestProjectorCompletionDoesNotDeadlockSameGenerationCommit(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	operations := []struct {
		name string
		run  func(ProjectorQueue, context.Context, projector.ScopeGenerationWork) error
	}{
		{name: "ack", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			return queue.Ack(ctx, work, runtime.Result{})
		}},
		{name: "terminal_fail", run: func(queue ProjectorQueue, ctx context.Context, work projector.ScopeGenerationWork) error {
			return queue.Fail(ctx, work, errors.New("terminal proof failure"))
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			projectorDB := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, projectorDB, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-lock', 'repository', 'github', 'proof/lock', 'git',
          'proof/lock', now(), now(), 'active', 'gen-lock');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-lock', 'scope-lock', 'push', now(), now(), 'active', now());
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-lock_gen-lock', 'scope-lock', 'gen-lock',
          'projector', 'source_local', 'running', 1, 'proof-worker',
          now() + interval '2 minutes', now(), '{}'::jsonb, now(), now());
`)
			ingestDB := openLivenessProofDB(t, dsn)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			var schemaName string
			if err := projectorDB.QueryRowContext(ctx, "SHOW search_path").Scan(&schemaName); err != nil {
				t.Fatalf("read proof search_path: %v", err)
			}
			if _, err := ingestDB.ExecContext(ctx, "SET search_path TO "+schemaName); err != nil {
				t.Fatalf("set ingest search_path: %v", err)
			}

			ingestTx, err := ingestDB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatalf("begin ingest tx: %v", err)
			}
			defer func() { _ = ingestTx.Rollback() }()
			for _, query := range []string{
				"UPDATE ingestion_scopes SET observed_at = observed_at WHERE scope_id = 'scope-lock'",
				"UPDATE scope_generations SET observed_at = observed_at WHERE generation_id = 'gen-lock'",
			} {
				if _, err := ingestTx.ExecContext(ctx, query); err != nil {
					t.Fatalf("lock ingest row: %v", err)
				}
			}

			var projectorPID int
			if err := projectorDB.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&projectorPID); err != nil {
				t.Fatalf("read projector backend pid: %v", err)
			}
			queue := NewProjectorQueue(SQLDB{DB: projectorDB}, "proof-worker", time.Minute)
			work := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-lock"},
				Generation:   scope.ScopeGeneration{GenerationID: "gen-lock"},
				AttemptCount: 1,
			}
			completionDone := make(chan error, 1)
			go func() { completionDone <- operation.run(queue, ctx, work) }()

			// Wait until completion is blocked on a row held by ingest. This
			// establishes the lock ordering before ingest reaches Enqueue.
			blocked := false
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case err := <-completionDone:
					t.Fatalf("completion returned before ingest released rows: %v", err)
				default:
				}
				var waitType sql.NullString
				if err := ingestTx.QueryRowContext(ctx,
					"SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1", projectorPID,
				).Scan(&waitType); err != nil {
					t.Fatalf("inspect projector wait: %v", err)
				}
				if waitType.Valid && waitType.String == "Lock" {
					blocked = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatal("projector completion did not wait on the ingest transaction")
			}

			// The same-generation work ID is a unique conflict. Ingest must
			// finish this enqueue without deadlocking with projector completion.
			ingestQueue := NewProjectorQueue(SQLTx{Tx: ingestTx}, "proof-worker", time.Minute)
			enqueueErr := ingestQueue.Enqueue(ctx, "scope-lock", "gen-lock")
			if enqueueErr != nil {
				_ = ingestTx.Rollback()
				select {
				case <-completionDone:
				case <-time.After(time.Second):
				}
				t.Fatalf("same-generation ingest enqueue deadlocked: %v", enqueueErr)
			}
			if err := ingestTx.Commit(); err != nil {
				t.Fatalf("commit ingest tx: %v", err)
			}
			select {
			case err := <-completionDone:
				if err != nil {
					t.Fatalf("projector completion after ingest commit: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("projector completion did not finish after ingest commit")
			}
		})
	}
}
