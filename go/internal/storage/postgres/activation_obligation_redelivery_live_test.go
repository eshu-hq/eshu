// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/recovery"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// TestActivationObligationRedeliveryAndCatchUpRacesLive (#7584 D3 step 4,
// item 6): a duplicate Ack redelivery released together with the first Ack,
// a re-activating Ack (the re-owe path) released together with catch-up, and
// two catch-up replicas re-owing the same obsolete row of the active
// generation. Each ends with exactly one open obligation and no unique
// violation surfacing to any caller.
func TestActivationObligationRedeliveryAndCatchUpRacesLive(t *testing.T) {
	for i := 0; i < 5; i++ {
		t.Run(fmt.Sprintf("duplicate_ack_%d", i), func(t *testing.T) {
			ctx, database, _, _, targetWork := setupActivationConsumer(t, "act_dup_ack")
			pq := NewProjectorQueue(SQLDB{DB: database}, "7584-consumer-projector", time.Minute)
			ack := func() error { return pq.Ack(ctx, targetWork, projectorruntime.Result{}) }
			errs := releaseTogether(ack, ack)
			accepted, rejected := 0, 0
			for _, err := range errs {
				switch {
				case err == nil:
					accepted++
				case errors.Is(err, ErrProjectorClaimRejected):
					rejected++
				default:
					t.Fatalf("Ack redelivery: %v (sqlstate %q)", err, sqlState(err))
				}
			}
			if accepted != 1 || rejected != 1 {
				t.Fatalf("Acks accepted=%d rejected=%d, want 1 and 1", accepted, rejected)
			}
			assertExactActivationObligation(t, ctx, database, targetWork, "duplicate Ack obligation")
		})
	}
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("reactivating_ack_vs_catch_up_%d", i), func(t *testing.T) {
			runReactivationRace(t)
		})
	}
	for i := 0; i < 3; i++ {
		t.Run(fmt.Sprintf("two_catch_ups_reowe_one_row_%d", i), func(t *testing.T) {
			f := newActivationMatrix(t, "act_catchup_race", true)
			obsoleteActiveObligation(t, f)
			var inserted [2]int
			catchUp := func(n int) func() error {
				return func() error {
					page, err := f.oblig.CatchUp(f.ctx, "", 500)
					inserted[n] = page.Inserted
					return err
				}
			}
			for n, err := range releaseTogether(catchUp(0), catchUp(1)) {
				if err != nil {
					t.Fatalf("catch-up %d: %v (sqlstate %q)", n, err, sqlState(err))
				}
			}
			if inserted[0]+inserted[1] != 1 {
				t.Fatalf("catch-up re-owes = %v, want exactly one in all", inserted)
			}
			requireOneOpenObligation(t, f.ctx, f.db, f.scope, f.gen)
		})
	}
	// Deterministic overlap: the first catch-up's re-owe is held uncommitted
	// until the second is seen waiting on that row; the second must re-check
	// the row after the wait and re-owe nothing.
	t.Run("held_catch_up_then_second_rechecks", func(t *testing.T) {
		f := newActivationMatrix(t, "act_catchup_held", true)
		obsoleteActiveObligation(t, f)
		tx, err := SQLDB{DB: f.db}.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		first, err := activation.NewStore(txDatabase{Transaction: tx}).CatchUp(f.ctx, "", 500)
		if err != nil || first.Inserted != 1 {
			t.Fatalf("held catch-up = %+v err=%v, want one re-owe", first, err)
		}
		type pageResult struct {
			page activation.CatchUpPage
			err  error
		}
		second := make(chan pageResult, 1)
		go func() {
			page, err := f.oblig.CatchUp(f.ctx, "", 500)
			second <- pageResult{page, err}
		}()
		// pg_stat_activity keeps only the first track_activity_query_size bytes,
		// so match the catch-up statement by its opening page CTE.
		awaitLockWaiter(t, f.ctx, f.db, "%scope.active_generation_id AS generation_id%")
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		got := <-second
		if got.err != nil || got.page.Inserted != 0 {
			t.Fatalf("second catch-up after the wait = %+v err=%v (sqlstate %q), want no re-owe",
				got.page, got.err, sqlState(got.err))
		}
		requireOneOpenObligation(t, f.ctx, f.db, f.scope, f.gen)
	})
}

// obsoleteActiveObligation turns the active target generation's pending
// obligation into an obsolete row: the only same-row re-owe the catch-up
// statement can meet (an obsolete row of the active generation, no phase).
func obsoleteActiveObligation(t *testing.T, f activationMatrix) {
	t.Helper()
	if _, err := f.db.ExecContext(f.ctx, `UPDATE activation_obligations
SET state = 'obsolete', finished_at = clock_timestamp()
WHERE scope_id = $1 AND generation_id = $2`, f.scope, f.gen); err != nil {
		t.Fatal(err)
	}
}

// txDatabase runs activation store statements inside one open transaction.
type txDatabase struct{ db.Transaction }

// Begin implements db.Beginner; a held transaction never nests.
func (txDatabase) Begin(context.Context) (db.Transaction, error) {
	return nil, errors.New("txDatabase does not begin nested transactions")
}

// runReactivationRace drives the NULL-pointer path to an obsolete obligation,
// refinalizes the scope, and releases the re-activating Ack and a catch-up
// page together.
func runReactivationRace(t *testing.T) {
	t.Helper()
	f := newActivationMatrix(t, "act_reowe_race", true)
	refinalize := func() {
		t.Helper()
		if _, err := NewRecoveryStore(SQLDB{DB: f.db}).RefinalizeScopeProjections(f.ctx,
			recovery.RefinalizeFilter{ScopeIDs: []string{f.scope}}, time.Now().UTC()); err != nil {
			t.Fatalf("refinalize: %v", err)
		}
	}
	refinalize()
	pq := NewProjectorQueue(SQLDB{DB: f.db}, "7584-reowe-projector", time.Minute)
	redrive := claimActivationProjectorWork(t, f.ctx, pq, f.scope, f.gen)
	if err := epochPass(f.ctx, f.store); err != nil {
		t.Fatal(err)
	}
	if err := pq.Fail(f.ctx, redrive, errors.New("permanent projection failure")); err != nil {
		t.Fatalf("projector Fail: %v", err)
	}
	if _, err := newLiveActivationRunner(f.db, &countingActivationMaintainer{}, time.Minute).RunOnce(f.ctx); err != nil {
		t.Fatal(err)
	}
	if state := f.obligationState(t, f.gen); state != "obsolete" {
		t.Fatalf("obligation after the NULL pointer = %q, want obsolete", state)
	}
	refinalize()
	reactivate := claimActivationProjectorWork(t, f.ctx, pq, f.scope, f.gen)
	errs := releaseTogether(
		func() error { return pq.Ack(f.ctx, reactivate, projectorruntime.Result{}) },
		func() error { _, err := f.oblig.CatchUp(f.ctx, "", 500); return err },
	)
	for n, err := range errs {
		if err != nil {
			t.Fatalf("racer %d: %v (sqlstate %q)", n, err, sqlState(err))
		}
	}
	assertActivationActivePointer(t, f.ctx, f.db, f.scope, f.gen)
	requireOneOpenObligation(t, f.ctx, f.db, f.scope, f.gen)
}

// requireOneOpenObligation requires exactly one row for the generation,
// pending, unleased and unfinished.
func requireOneOpenObligation(t *testing.T, ctx context.Context, database *sql.DB, scopeID, generationID string) {
	t.Helper()
	var count int
	var state string
	var finished, leased bool
	if err := database.QueryRowContext(ctx, `SELECT count(*), min(state),
    bool_or(finished_at IS NOT NULL), bool_or(lease_owner IS NOT NULL OR lease_until IS NOT NULL)
FROM activation_obligations WHERE scope_id = $1 AND generation_id = $2`, scopeID, generationID).Scan(
		&count, &state, &finished, &leased); err != nil {
		t.Fatal(err)
	}
	if count != 1 || state != "pending" || finished || leased {
		t.Fatalf("obligations for %s/%s = count %d state %q finished %t leased %t, want one pending",
			scopeID, generationID, count, state, finished, leased)
	}
}

// TestActivationObligationSupersessionBetweenClaimAndFinalizeLive (#7584 D3
// step 4, item 7): after the consumer claimed an owed generation and found
// its phase missing, a newer generation of the same scope is committed and
// activated through the real projector Claim and Ack. The production
// maintainer then reports not_active, Finalize retires the claimed
// obligation obsolete with no phase and no wake, and the successor's own
// obligation (written by that Ack) completes in the same cycle.
func TestActivationObligationSupersessionBetweenClaimAndFinalizeLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "act_supersede_real")
	store := NewIngestionStore(SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	queue := NewProjectorQueue(SQLDB{DB: database}, "7584-supersede-projector", time.Minute)
	activate := func(scopeID, generationID, repoID string, later time.Duration) {
		t.Helper()
		fact := activationRepositoryFact("fact-"+generationID, scopeID, generationID, repoID,
			"https://github.com/acme/"+repoID+".git")
		fact.ObservedAt = fact.ObservedAt.Add(later)
		commitActivationRepository(t, ctx, store, fact, repoID)
		work := claimActivationProjectorWork(t, ctx, queue, scopeID, generationID)
		if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
			t.Fatalf("Ack %s: %v", generationID, err)
		}
	}
	activate("git:sup-x", "sup-x-1", "repo-sup-x", 0)
	activate("git:sup-z", "sup-z-1", "repo-sup-z", 0)
	if err := epochPass(ctx, NewIngestionStore(SQLDB{DB: database})); err != nil {
		t.Fatal(err)
	}
	activate("git:sup-x", "sup-x-2", "repo-sup-x", time.Hour) // the owed quiet generation
	if _, err := database.ExecContext(ctx, `INSERT INTO fact_work_items
  (work_item_id, scope_id, generation_id, stage, domain, status, attempt_count,
   visible_at, next_attempt_at, failure_class, failure_message, payload, created_at, updated_at)
VALUES ($1, 'git:sup-x', 'sup-x-2', 'reducer', 'deployment_mapping', 'retrying', 1,
   clock_timestamp() + interval '1 hour', clock_timestamp() + interval '1 hour',
   'cross_repo_backward_evidence_not_ready', 'backward evidence not ready', '{}'::jsonb, now(), now())`,
		composedWaitingID("sup-x-2")); err != nil {
		t.Fatal(err)
	}
	consumer := newComposedConsumer(t, database, "7584-supersede-consumer", time.Minute, 0)
	consumer.port.before = func(_ context.Context, work maintenance.ActivationObligation) error {
		if work.GenerationID == "sup-x-2" {
			activate("git:sup-x", "sup-x-3", "repo-sup-x", 2*time.Hour)
		}
		return nil
	}
	if _, err := consumer.runner.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	consumer.requireNoFailures(t, ctx)
	for _, want := range []struct {
		gen, state string
		token      int64
		calls      int
	}{
		{"sup-x-1", "obsolete", 1, 0}, {"sup-x-2", "obsolete", 1, 1}, {"sup-x-3", "completed", 1, 1},
	} {
		assertObligationStateToken(t, ctx, database, "git:sup-x", want.gen, want.state, want.token)
		if got := consumer.port.callsFor(want.gen); got != want.calls {
			t.Fatalf("callbacks for %s = %d, want %d", want.gen, got, want.calls)
		}
	}
	assertObligationStateToken(t, ctx, database, "git:sup-z", "sup-z-1", "completed", 1)
	for gen, want := range map[string]bool{"sup-x-2": false, "sup-x-3": true} {
		var exists bool
		if err := database.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM graph_projection_phase_state
WHERE scope_id = 'git:sup-x' AND generation_id = $1 AND keyspace = 'cross_repo_evidence'
  AND phase = 'backward_evidence_committed')`, gen).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists != want {
			t.Fatalf("phase for %s exists=%t, want %t", gen, exists, want)
		}
	}
	assertActivationActivePointer(t, ctx, database, "git:sup-x", "sup-x-3")
	requireWoken(t, ctx, database, "sup-x-2", false)
	if got := targetedCounter(consumer.metrics(t, ctx), "eshu_dp_deferred_backfill_targeted_outcomes_total", "outcome", "not_active"); got != 1 {
		t.Fatalf("targeted not_active outcomes = %d, want 1 (the superseded claim)", got)
	}
}
