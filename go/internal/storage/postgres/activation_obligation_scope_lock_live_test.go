// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// finalizeScopeLockLike matches Finalize's scope lock in pg_stat_activity.
const finalizeScopeLockLike = "%FROM ingestion_scopes%FOR NO KEY UPDATE%"

// heldTgtCommit starts a real ingestion commit of a pending git:tgt
// generation and parks it inside its transaction, after the scope row lock,
// on its fact stream. release sends the repository fact and lets it commit.
type heldTgtCommit struct {
	facts chan facts.Envelope
	done  chan error
	gen   string
}

func holdTgtCommit(t *testing.T, ctx context.Context, database *sql.DB, generationID string) *heldTgtCommit {
	t.Helper()
	store := NewIngestionStore(SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	held := &heldTgtCommit{facts: make(chan facts.Envelope), done: make(chan error, 1), gen: generationID}
	go func() {
		held.done <- store.CommitScopeGeneration(ctx,
			scope.IngestionScope{
				ScopeID: "git:tgt", SourceSystem: "git", ScopeKind: scope.KindRepository,
				CollectorKind: scope.CollectorGit, PartitionKey: "git:tgt",
			},
			catalogTestGeneration("git:tgt", generationID, time.Now().UTC()), held.facts)
	}()
	awaitScopeRowLocked(t, ctx, database, "git:tgt")
	return held
}

func (h *heldTgtCommit) release(t *testing.T) {
	t.Helper()
	h.facts <- facts.Envelope{
		FactID: "repo-" + h.gen + "-repo-tgt", ScopeID: "git:tgt", GenerationID: h.gen,
		FactKind: "repository", StableFactKey: "repository:repo-tgt", ObservedAt: time.Now().UTC(),
		Payload:   map[string]any{"repo_id": "repo-tgt", "name": "payments-service"},
		SourceRef: facts.Ref{SourceSystem: "git", FactKey: "repo-" + h.gen},
	}
	close(h.facts)
	if err := <-h.done; err != nil {
		t.Fatalf("held ingestion commit: %v", err)
	}
}

// claimPublished claims tgt-2's obligation and publishes its phase with the
// production partition-scoped maintainer, so the next Finalize completes.
func claimPublished(t *testing.T, ctx context.Context, database *sql.DB, lease time.Duration) *activation.Obligation {
	t.Helper()
	store := activation.NewStore(SQLDB{DB: database})
	work := claimActivationObligation(t, ctx, store, "7584-scope-lock", lease, "git:tgt", "tgt-2")
	if result, err := store.Finalize(ctx, *work); err != nil || result.Outcome != activation.OutcomePhaseNotReady {
		t.Fatalf("first Finalize = %+v err=%v, want phase_not_ready", result, err)
	}
	if err := NewActivationMaintainer(NewIngestionStore(SQLDB{DB: database}), nil, nil).MaintainActivation(ctx,
		maintenance.ActivationObligation{ScopeID: work.ScopeID, GenerationID: work.GenerationID}); err != nil {
		t.Fatalf("partition-scoped maintenance: %v", err)
	}
	return work
}

// TestActivationObligationIngestionCommitRacesFinalizeLive (#7584 D3 step 4,
// item 3): an ingestion commit holding the scope row makes Finalize wait on
// its scope lock; Finalize then re-reads the active pointer. A commit that
// leaves the pointer alone lets Finalize complete; a projector Ack that moves
// it makes Finalize retire the obligation obsolete; a lock held past
// Finalize's lock_timeout surfaces as a retried attempt that changes nothing
// durable, and the next claimer after the lease completes it.
func TestActivationObligationIngestionCommitRacesFinalizeLive(t *testing.T) {
	t.Run("pending_commit_then_complete", func(t *testing.T) {
		ctx, database := openActivationObligationProofDB(t, "act_scope_commit")
		setupComposed(t, ctx, database, composedTgt2)
		work := claimPublished(t, ctx, database, time.Minute)
		held := holdTgtCommit(t, ctx, database, "tgt-3")
		finalized := make(chan error, 1)
		var result activation.FinalizeResult
		go func() {
			var err error
			result, err = activation.NewStore(SQLDB{DB: database}).Finalize(ctx, *work)
			finalized <- err
		}()
		awaitLockWaiter(t, ctx, database, finalizeScopeLockLike)
		held.release(t)
		if err := <-finalized; err != nil {
			t.Fatalf("Finalize after the commit: %v (sqlstate %q)", err, sqlState(err))
		}
		if result.Outcome != activation.OutcomeCompleted || result.Woken != 1 {
			t.Fatalf("Finalize = %+v, want completed with 1 woken", result)
		}
		assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "completed", 1)
		assertActivationActivePointer(t, ctx, database, "git:tgt", "tgt-2")
		if got := activationGenerationStatus(t, ctx, database, "tgt-3"); got != "pending" {
			t.Fatalf("committed generation status = %q, want pending", got)
		}
		requireWoken(t, ctx, database, "tgt-2", true)
	})

	t.Run("ack_moves_pointer_then_obsolete", func(t *testing.T) {
		ctx, database := openActivationObligationProofDB(t, "act_scope_ack")
		setupComposed(t, ctx, database, composedTgt2)
		work := claimPublished(t, ctx, database, time.Minute)
		held := holdTgtCommit(t, ctx, database, "tgt-3")
		held.release(t)
		parked, release := make(chan struct{}), make(chan struct{})
		gate := &lockWaitBeginner{inner: SQLDB{DB: database}, onExec: func(ctx context.Context, query string) error {
			if strings.Contains(query, "INSERT INTO activation_obligations") {
				close(parked)
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		}}
		pq := NewProjectorQueue(gatedSQLDB{SQLDB: SQLDB{DB: database}, beginner: gate}, "7584-scope-ack", time.Minute)
		next := claimActivationProjectorWork(t, ctx, pq, "git:tgt", "tgt-3")
		acked := make(chan error, 1)
		go func() { acked <- pq.Ack(ctx, next, projectorruntime.Result{}) }()
		<-parked // the Ack holds the scope row with the pointer moved, uncommitted
		finalized := make(chan error, 1)
		var result activation.FinalizeResult
		go func() {
			var err error
			result, err = activation.NewStore(SQLDB{DB: database}).Finalize(ctx, *work)
			finalized <- err
		}()
		awaitLockWaiter(t, ctx, database, finalizeScopeLockLike)
		close(release)
		if err := <-acked; err != nil {
			t.Fatalf("projector Ack of tgt-3: %v", err)
		}
		if err := <-finalized; err != nil {
			t.Fatalf("Finalize after the Ack: %v (sqlstate %q)", err, sqlState(err))
		}
		if result.Outcome != activation.OutcomeObsolete || result.Woken != 0 {
			t.Fatalf("Finalize = %+v, want obsolete with nothing woken", result)
		}
		assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "obsolete", 1)
		assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-3", "pending", 0)
		assertActivationActivePointer(t, ctx, database, "git:tgt", "tgt-3")
		// The superseded generation's waiting row stays a future retry: no
		// wake for an obsolete obligation.
		requireWoken(t, ctx, database, "tgt-2", false)
	})

	t.Run("lock_timeout_is_retried_not_failed_truth", func(t *testing.T) {
		ctx, database := openActivationObligationProofDB(t, "act_scope_timeout")
		setupComposed(t, ctx, database, composedTgt2)
		// The phase is published before any claim, so the consumer's first
		// Finalize would complete; the held commit makes it time out.
		if _, err := NewIngestionStore(SQLDB{DB: database}).RunDeferredRelationshipMaintenanceForPartitions(ctx, nil, nil,
			[]OwedPartition{{ScopeID: "git:tgt", GenerationID: "tgt-2"}}); err != nil {
			t.Fatal(err)
		}
		consumer := newComposedConsumer(t, database, "7584-scope-timeout", 2*time.Second, 0)
		held := holdTgtCommit(t, ctx, database, "tgt-3")
		before := composedState(t, ctx, database)
		if _, err := consumer.runner.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		// A real SQLSTATE 55P03 from Finalize is counted under its own
		// reason (ruling P2-F), never as a finalize failure.
		if got := consumer.failures(t, ctx, "finalize_lock_timeout"); got != 1 {
			t.Fatalf("finalize_lock_timeout failures = %d, want 1 (the lock timeout)", got)
		}
		if got := consumer.failures(t, ctx, "finalize"); got != 0 {
			t.Fatalf("finalize failures = %d, want 0 (a lock timeout is not one)", got)
		}
		after := composedState(t, ctx, database)
		want := replaceTuple(before, "obligation|git:tgt|tgt-2|pending", "obligation|git:tgt|tgt-2|leased")
		requireComposedState(t, "durable state after a Finalize lock timeout", after, want)
		held.release(t)
		awaitObligationLeaseExpiry(t, ctx, database, "git:tgt", "tgt-2")
		if _, err := consumer.runner.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		assertObligationStateToken(t, ctx, database, "git:tgt", "tgt-2", "completed", 2)
		if got := consumer.port.total(); got != 0 {
			t.Fatalf("maintenance callbacks = %d, want 0 (the phase was already published)", got)
		}
		requireWoken(t, ctx, database, "tgt-2", true)
	})
}

// gatedSQLDB is SQLDB whose transactions go through a statement gate.
type gatedSQLDB struct {
	SQLDB
	beginner *lockWaitBeginner
}

// Begin implements db.Beginner through the gate.
func (g gatedSQLDB) Begin(ctx context.Context) (db.Transaction, error) { return g.beginner.Begin(ctx) }

// replaceTuple returns state with old replaced by new, failing loudly via a
// mismatch later if old was absent.
func replaceTuple(state []string, old, new string) []string {
	out := make([]string, 0, len(state))
	for _, tuple := range state {
		if tuple == old {
			tuple = new
		}
		out = append(out, tuple)
	}
	return sortedTuples(out)
}

// awaitObligationLeaseExpiry waits on the database clock until the
// obligation's lease has expired.
func awaitObligationLeaseExpiry(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var expired bool
		err := database.QueryRowContext(ctx, `SELECT COALESCE(lease_until <= clock_timestamp(), TRUE)
FROM activation_obligations WHERE scope_id = $1 AND generation_id = $2`, scopeID, generationID).Scan(&expired)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		if expired {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lease of %s/%s did not expire", scopeID, generationID)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
