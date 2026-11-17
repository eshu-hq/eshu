// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10820 disposable PostgreSQL 18 deadlock probes for the census 2e purge statements: each probe parks an in-flight writer ahead of the purge in that writer's own lock order and proves the purge waits without a 40P01 cycle (the real relationship generation claim activation statements, infra and config-artifact derive advisory locks, service materialization)
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/infra/inventory"
)

// runPurgeBehindWriter parks a writer transaction in the middle of its lock
// order, starts the purge, waits until a backend is blocked behind the writer,
// then lets the writer take the rest of its locks in its own order. A purge
// that takes any of those locks in the opposite order deadlocks here (40P01
// surfaces on the writer or the purge). The purge must finish after the writer
// commits.
func runPurgeBehindWriter(
	t *testing.T,
	prefix string,
	holdFirst func(ctx context.Context, tx *sql.Tx, partition string, scopeIDs []string) error,
	thenTake func(ctx context.Context, tx *sql.Tx, partition string, scopeIDs []string) error,
) (*sql.DB, context.Context, []string) {
	t.Helper()
	database, ctx := openIsolatedLiveDB(t, prefix, "set ESHU_POSTGRES_DSN to run the census lock-order Postgres proof")
	partition := "PARTITION:purge-live"
	scopeIDs := seedPurgeLiveScope(t, ctx, database, partition)
	seedCensus2eRows(t, ctx, database, partition, scopeIDs)
	store := NewRetirementStore(SQLDB{DB: database})
	if _, err := store.InsertPending(ctx, RetirementRequest{
		RetirementID: "rr_" + prefix, RepoID: partition, ScopeID: scopeIDs[0],
		ReasonCode: RetirementReasonOperatorRetired, ActorClass: "operator",
		IdempotencyKeyHash: prefix + "-key",
	}); err != nil {
		t.Fatalf("InsertPending: %v", err)
	}

	writer, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin writer: %v", err)
	}
	defer func() { _ = writer.Rollback() }()
	var writerPID int64
	if err := writer.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID); err != nil {
		t.Fatalf("read writer backend pid: %v", err)
	}
	if err := holdFirst(ctx, writer, partition, scopeIDs); err != nil {
		t.Fatalf("writer first lock: %v", err)
	}

	type outcome struct {
		result PurgeResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		purger := NewRetirementPurger(SQLDB{DB: database}, SQLDB{DB: database})
		result, err := purger.PurgeRetired(ctx, PurgeRequest{RepoIDs: []string{partition}})
		done <- outcome{result: result, err: err}
	}()

	deadline := time.Now().Add(15 * time.Second)
	for {
		blocked := liveCount(t, ctx, database, `SELECT count(*) FROM pg_stat_activity
			WHERE $1 = ANY(pg_blocking_pids(pid))`, writerPID)
		if blocked > 0 {
			break
		}
		select {
		case early := <-done:
			t.Fatalf("purge finished without waiting on the writer: %+v, %v", early.result, early.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("purge never blocked on the writer's lock")
		}
		time.Sleep(50 * time.Millisecond)
	}

	if err := thenTake(ctx, writer, partition, scopeIDs); err != nil {
		t.Fatalf("writer second lock: %v (purge and writer took the locks in opposite orders)", err)
	}
	if err := writer.Commit(); err != nil {
		t.Fatalf("writer commit: %v", err)
	}
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("PurgeRetired: %v", got.err)
		}
		if len(got.result.Completed) != 1 || len(got.result.Blocked) != 0 {
			t.Fatalf("result = %+v, want one completion and no blocks", got.result)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("purge did not finish after the writer committed")
	}
	return database, ctx, scopeIDs
}

// TestRetirementPurgeRelationshipGenerationsLockOrderLive: the claim variant of
// the relationship generation activation locks its fact_work_items row, then
// writes relationship_generations, then locks the in-flight own-gate consumer
// rows and wakes the dead-lettered ones (relationship_store_resolved.go). The
// writer runs those real statements. The purge deletes the work rows through
// the scope cascade and relationship_generations after it, so it holds no
// generation row while it waits on the work row the writer holds, and the
// generation the writer activated is still deleted afterwards.
func TestRetirementPurgeRelationshipGenerationsLockOrderLive(t *testing.T) {
	const workItem = "purge-live-w-purge-live-main-gen-old"
	claimedAt := time.Now().UTC().Truncate(time.Microsecond)
	database, ctx, scopeIDs := runPurgeBehindWriter(t, "census_lock_rel",
		func(ctx context.Context, tx *sql.Tx, _ string, _ []string) error {
			// Claim the work row: the UPDATE takes its row lock, the first
			// lock of the activation transaction.
			_, err := tx.ExecContext(ctx, `UPDATE fact_work_items
				SET status = 'running', domain = 'deployment_mapping', last_attempt_at = $2,
				    claim_until = NOW() + interval '1 hour', lease_owner = 'census-writer'
				WHERE work_item_id = $1`, workItem, claimedAt)
			return err
		},
		func(ctx context.Context, tx *sql.Tx, _ string, scopeIDs []string) error {
			generationID := scopeIDs[0] + "-gen-old"
			now := time.Now().UTC()
			result, err := tx.ExecContext(ctx, activateResolutionGenerationForClaimSQL,
				generationID, scopeIDs[0], now, now, workItem, claimedAt)
			if err != nil {
				return err
			}
			if n, _ := result.RowsAffected(); n == 0 {
				return fmt.Errorf("the real activation statement matched no claimant row")
			}
			rows, err := tx.QueryContext(ctx, lockInflightOwnGateConsumersSQL, scopeIDs[0], generationID)
			if err != nil {
				return err
			}
			for rows.Next() {
			}
			if err := rows.Err(); err != nil {
				_ = rows.Close()
				return err
			}
			_ = rows.Close()
			_, err = tx.ExecContext(ctx, wakeProducerTerminalConsumersSQL, now, scopeIDs[0], generationID)
			return err
		})
	// The generation the writer activated is deleted with the rest.
	if got := liveCount(t, ctx, database, `SELECT count(*) FROM relationship_generations WHERE scope = ANY($1)`, scopeIDs); got != 0 {
		t.Fatalf("relationship_generations rows = %d, want 0 after purge", got)
	}
}

// TestRetirementPurgeInfraAdvisoryLockOrderLive: the infra derive holds the
// repository advisory lock for its whole write, reads and rewrites
// content_entities and the mirror under it. The purge takes the same lock
// before it deletes any content row, so it never holds a content row while
// waiting on a lock a derive holds.
func TestRetirementPurgeInfraAdvisoryLockOrderLive(t *testing.T) {
	runPurgeBehindWriter(t, "census_lock_infra",
		func(ctx context.Context, tx *sql.Tx, partition string, _ []string) error {
			return inventory.LockRepository(ctx, tx, partition)
		},
		func(ctx context.Context, tx *sql.Tx, partition string, _ []string) error {
			if _, err := tx.ExecContext(ctx, `UPDATE content_entities SET indexed_at = NOW() WHERE repo_id = $1`, partition); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `UPDATE infra_resource_entities SET updated_at = NOW() WHERE repo_id = $1`, partition)
			return err
		})
}

// TestRetirementPurgeConfigArtifactAdvisoryLockOrderLive: the config-artifact
// derive takes its repository advisory lock first and then rewrites
// config_artifact_paths.
func TestRetirementPurgeConfigArtifactAdvisoryLockOrderLive(t *testing.T) {
	runPurgeBehindWriter(t, "census_lock_cfg",
		func(ctx context.Context, tx *sql.Tx, partition string, _ []string) error {
			return artifacts.LockRepository(ctx, tx, partition)
		},
		func(ctx context.Context, tx *sql.Tx, partition string, _ []string) error {
			_, err := tx.ExecContext(ctx, `UPDATE config_artifact_paths SET name_scoped = true WHERE repo_id = $1`, partition)
			return err
		})
}

// TestRetirementPurgeServiceMaterializationLockOrderLive: the service
// materialization writer updates the generation row and then its evidence
// snapshot rows in one transaction. The purge deletes the generation rows (the
// snapshots cascade) in the same order.
func TestRetirementPurgeServiceMaterializationLockOrderLive(t *testing.T) {
	runPurgeBehindWriter(t, "census_lock_svc",
		func(ctx context.Context, tx *sql.Tx, _ string, scopeIDs []string) error {
			_, err := tx.ExecContext(ctx, `UPDATE service_materialization_generations SET status = 'superseded'
				WHERE scope_id = $1`, scopeIDs[0])
			return err
		},
		func(ctx context.Context, tx *sql.Tx, _ string, scopeIDs []string) error {
			_, err := tx.ExecContext(ctx, `UPDATE service_evidence_snapshots SET payload_hash = 'h2'
				WHERE generation_id = $1 || '-svc'`, scopeIDs[0])
			return err
		})
}
