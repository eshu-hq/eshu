// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestProjectorAckRefusesSupersededGeneration is the #7130 regression. A
// sibling projector row on a generation that a newer Ack already superseded
// was replayed and claimed. Acking it must not re-activate the superseded
// generation, retire the published one, or repoint the scope. The work ends
// superseded and Ack reports ErrWorkSuperseded.
func TestProjectorAckRefusesSupersededGeneration(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	database := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, database, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-7130', 'repository', 'github', 'proof/7130', 'git',
          'proof/7130', now(), now(), 'active', 'gen-new');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, superseded_at
) VALUES ('gen-old', 'scope-7130', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'superseded', now() - interval '1 hour', now()),
         ('gen-new', 'scope-7130', 'push', now(), now(), 'active', now(), NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('refinalize_scope-7130_gen-old', 'scope-7130', 'gen-old',
          'projector', 'source_local', 'running', 2, 'proof-worker',
          now() + interval '1 minute', now(), '{}'::jsonb, now(), now());
`)

	queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-7130"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-old"},
		AttemptCount: 2,
	}
	err := queue.Ack(context.Background(), work, runtime.Result{})
	if !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack of superseded-generation work = %v, want ErrWorkSuperseded", err)
	}
	assertSupersededAckState(t, database, "scope-7130", "refinalize_scope-7130_gen-old",
		supersededAckWant{oldGeneration: "superseded", newGeneration: "active", pointer: "gen-new"})

	// A repeated Ack from the same stale attempt must not move anything either.
	if err := queue.Ack(context.Background(), work, runtime.Result{}); !errors.Is(err, ErrProjectorClaimRejected) {
		t.Fatalf("repeated Ack = %v, want ErrProjectorClaimRejected", err)
	}
	assertSupersededAckState(t, database, "scope-7130", "refinalize_scope-7130_gen-old",
		supersededAckWant{oldGeneration: "superseded", newGeneration: "active", pointer: "gen-new"})
}

// TestProjectorAckRefusesGenerationSupersededAfterClaim covers the TOCTOU:
// the claimed generation is superseded by a transaction that commits while
// Ack is in flight, after Ack took the scope and work locks. The claim path's
// stale-generation supersede locks only the generation row, not the scope, so
// Ack must re-check the generation under the generation row lock it already
// takes. EvalPlanQual then sees the committed supersede.
func TestProjectorAckRefusesGenerationSupersededAfterClaim(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}

	projectorDB := openLivenessProofDB(t, dsn)
	provisionLivenessSchema(t, projectorDB, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-race', 'repository', 'github', 'proof/race', 'git',
          'proof/race', now(), now(), 'active', 'gen-live');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at
) VALUES ('gen-live', 'scope-race', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'active', now() - interval '1 hour'),
         ('gen-claimed', 'scope-race', 'push', now(), now(), 'pending', NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('projector_scope-race_gen-claimed', 'scope-race', 'gen-claimed',
          'projector', 'source_local', 'running', 1, 'proof-worker',
          now() + interval '1 minute', now(), '{}'::jsonb, now(), now());
`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var searchPath string
	if err := projectorDB.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPath); err != nil {
		t.Fatalf("read proof search_path: %v", err)
	}
	supersedeDB := openLivenessProofDB(t, dsn)
	if _, err := supersedeDB.ExecContext(ctx, "SET search_path TO "+searchPath); err != nil {
		t.Fatalf("set supersede search_path: %v", err)
	}
	supersedeTx, err := supersedeDB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin supersede tx: %v", err)
	}
	defer func() { _ = supersedeTx.Rollback() }()
	// The claim statement's stale-generation branch: generation row only.
	if _, err := supersedeTx.ExecContext(ctx, `
UPDATE scope_generations SET status = 'superseded', superseded_at = now()
WHERE generation_id = 'gen-claimed' AND status IN ('pending', 'failed')`); err != nil {
		t.Fatalf("supersede claimed generation: %v", err)
	}
	var supersedeXID string
	if err := supersedeTx.QueryRowContext(ctx, "SELECT pg_current_xact_id()::text").Scan(&supersedeXID); err != nil {
		t.Fatalf("read supersede xid: %v", err)
	}

	queue := NewProjectorQueue(SQLDB{DB: projectorDB}, "proof-worker", time.Minute)
	work := projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: "scope-race"},
		Generation:   scope.ScopeGeneration{GenerationID: "gen-claimed"},
		AttemptCount: 1,
	}
	ackErr := make(chan error, 1)
	go func() { ackErr <- queue.Ack(ctx, work, runtime.Result{}) }()

	waitForUngrantedLock(ctx, t, openLivenessProofDB(t, dsn), supersedeXID)
	if err := supersedeTx.Commit(); err != nil {
		t.Fatalf("commit supersede tx: %v", err)
	}
	if err := <-ackErr; !errors.Is(err, failure.ErrWorkSuperseded) {
		t.Fatalf("Ack after concurrent supersede = %v, want ErrWorkSuperseded", err)
	}
	assertSupersededAckState(t, projectorDB, "scope-race", "projector_scope-race_gen-claimed",
		supersededAckWant{
			oldGeneration: "superseded", oldGenerationID: "gen-claimed",
			newGeneration: "active", newGenerationID: "gen-live", pointer: "gen-live",
		})
}

// waitForUngrantedLock blocks until some backend waits on transaction xid,
// which here is Ack waiting on the generation row the supersede transaction
// holds. db must be a connection pool neither side of the race is using.
func waitForUngrantedLock(ctx context.Context, t *testing.T, db *sql.DB, xid string) {
	t.Helper()
	for {
		var waiting int
		if err := db.QueryRowContext(ctx,
			"SELECT count(*) FROM pg_locks WHERE NOT granted AND locktype = 'transactionid' AND transactionid::text = $1",
			xid,
		).Scan(&waiting); err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if waiting > 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Ack never waited on the superseding transaction: %v", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

type supersededAckWant struct {
	oldGeneration, oldGenerationID string
	newGeneration, newGenerationID string
	pointer                        string
}

func assertSupersededAckState(t *testing.T, db *sql.DB, scopeID, workItemID string, want supersededAckWant) {
	t.Helper()
	if want.oldGenerationID == "" {
		want.oldGenerationID = "gen-old"
	}
	if want.newGenerationID == "" {
		want.newGenerationID = "gen-new"
	}
	var workStatus, oldGen, newGen, pointer string
	var failureClass sql.NullString
	if err := db.QueryRowContext(context.Background(), `
SELECT
    (SELECT status FROM fact_work_items WHERE work_item_id = $2),
    (SELECT failure_class FROM fact_work_items WHERE work_item_id = $2),
    (SELECT status FROM scope_generations WHERE generation_id = $3),
    (SELECT status FROM scope_generations WHERE generation_id = $4),
    (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1)`,
		scopeID, workItemID, want.oldGenerationID, want.newGenerationID,
	).Scan(&workStatus, &failureClass, &oldGen, &newGen, &pointer); err != nil {
		t.Fatalf("read superseded ack state: %v", err)
	}
	if workStatus != "superseded" || failureClass.String != projectorAckGenerationSupersededClass ||
		oldGen != want.oldGeneration || newGen != want.newGeneration || pointer != want.pointer {
		t.Fatalf("work=%s class=%s %s=%s %s=%s pointer=%s; want work=superseded class=%s %s=%s %s=%s pointer=%s",
			workStatus, failureClass.String, want.oldGenerationID, oldGen, want.newGenerationID, newGen, pointer,
			projectorAckGenerationSupersededClass, want.oldGenerationID, want.oldGeneration,
			want.newGenerationID, want.newGeneration, want.pointer)
	}
}

// TestProjectorAckSupersededMarkKeepsOwnerAndAttemptFences pins the mark
// statement's claim fences (review F2). Between the Ack rollback and the mark,
// another worker may reclaim the row (new lease_owner) or the same worker may
// re-claim it (attempt_count + 1). Either way the mark must match no row, so
// refuseSupersededAck returns ErrProjectorClaimRejected, leaves the row as the
// new owner holds it, and counts nothing.
func TestProjectorAckSupersededMarkKeepsOwnerAndAttemptFences(t *testing.T) {
	dsn := os.Getenv("ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN")
	if dsn == "" {
		t.Skip("set ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN to a disposable Postgres database")
	}
	for _, tc := range []struct {
		name     string
		owner    string
		attempts int
	}{
		{name: "reclaimed_by_other_owner", owner: "other-worker", attempts: 2},
		{name: "same_owner_next_attempt", owner: "proof-worker", attempts: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := openLivenessProofDB(t, dsn)
			provisionLivenessSchema(t, database, `
INSERT INTO ingestion_scopes (
    scope_id, scope_kind, source_system, source_key, collector_kind,
    partition_key, observed_at, ingested_at, status, active_generation_id
) VALUES ('scope-f2', 'repository', 'github', 'proof/f2', 'git',
          'proof/f2', now(), now(), 'active', 'gen-new');
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at,
    status, activated_at, superseded_at
) VALUES ('gen-old', 'scope-f2', 'push', now() - interval '1 hour',
          now() - interval '1 hour', 'superseded', now() - interval '1 hour', now()),
         ('gen-new', 'scope-f2', 'push', now(), now(), 'active', now(), NULL);
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload,
    created_at, updated_at
) VALUES ('refinalize_scope-f2_gen-old', 'scope-f2', 'gen-old', 'projector',
          'source_local', 'running', `+strconv.Itoa(tc.attempts)+`, '`+tc.owner+`',
          now() + interval '1 minute', now(), '{}'::jsonb, now(), now());
`)
			before := readAckFenceRow(t, database)
			queue := NewProjectorQueue(SQLDB{DB: database}, "proof-worker", time.Minute)
			instruments, reader := newEnqueueInstruments(t)
			queue.Instruments = instruments
			work := projector.ScopeGenerationWork{
				Scope:        scope.IngestionScope{ScopeID: "scope-f2"},
				Generation:   scope.ScopeGeneration{GenerationID: "gen-old"},
				AttemptCount: 2,
			}

			tx, err := SQLDB{DB: database}.Begin(context.Background())
			if err != nil {
				t.Fatalf("begin ack tx: %v", err)
			}
			err = queue.refuseSupersededAck(context.Background(), tx, work, time.Now().UTC())
			if !errors.Is(err, ErrProjectorClaimRejected) || errors.Is(err, failure.ErrWorkSuperseded) {
				t.Fatalf("refuseSupersededAck() = %v, want only ErrProjectorClaimRejected", err)
			}
			if after := readAckFenceRow(t, database); after != before {
				t.Fatalf("row changed under a lost claim: before %+v, after %+v", before, after)
			}
			if got := heartbeatFenceCount(t, reader); got != 0 {
				t.Fatalf("superseded generation fence count = %d, want 0 for a lost claim", got)
			}
		})
	}
}

// ackFenceRow is the full mutable state of the F2 row.
type ackFenceRow struct {
	status, owner, failureClass, updatedAt, claimUntil string
	attempts                                           int
}

func readAckFenceRow(t *testing.T, database *sql.DB) ackFenceRow {
	t.Helper()
	var row ackFenceRow
	if err := database.QueryRowContext(context.Background(), `
SELECT status, COALESCE(lease_owner, ''), COALESCE(failure_class, ''),
       updated_at::text, COALESCE(claim_until::text, ''), attempt_count
FROM fact_work_items WHERE work_item_id = 'refinalize_scope-f2_gen-old'`).Scan(
		&row.status, &row.owner, &row.failureClass, &row.updatedAt, &row.claimUntil, &row.attempts,
	); err != nil {
		t.Fatalf("read F2 row: %v", err)
	}
	return row
}
