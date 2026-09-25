// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestReducerContentionGatePreActivationDeferralExits covers every way a
// #6686 deferral ends other than its own generation activating (which
// TestReducerContentionGatePreActivationGenerationRunsAfterActivation covers),
// plus the batch claim path. The deferral's failure class is non-counting, so
// no attempt budget ends the wait; it must instead end when the projector
// lifecycle settles the pending generation. Each subtest starts from the same
// seed (gen-1 active, gen-2 pending with its projector row running) and first
// defers a gen-2 reducer intent through production code.
func TestReducerContentionGatePreActivationDeferralExits(t *testing.T) {
	dsn := reducerDomainFairnessDSN()
	if dsn == "" {
		t.Skip("set ESHU_REDUCER_FAIRNESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the contention gate")
	}

	// The projector dead-letters gen-2: failProjectorWorkQuery marks the
	// generation failed in the same statement, so the next attempt of the
	// deferred intent is terminal supersession and the handler never runs.
	t.Run("projector_dead_letter_ends_the_wait_superseded", func(t *testing.T) {
		ctx := context.Background()
		db, env, calls := setupPreActivationDeferral(t, ctx, dsn)

		projectorQueue := NewProjectorQueue(SQLDB{DB: db}, preActivationProofLeaseOwner, time.Minute)
		if err := projectorQueue.Fail(ctx, preActivationProjectorWork("gen-2"), errors.New("projection failed permanently")); err != nil {
			t.Fatalf("ProjectorQueue.Fail(gen-2) error = %v", err)
		}
		if got := readPreActivationGenerationStatus(t, ctx, db, "gen-2"); got != "failed" {
			t.Fatalf("gen-2 generation status = %q, want failed after the projector dead-letter", got)
		}

		env.advance(time.Minute)
		if claimed := env.claimExecuteSettle(t, ctx); !claimed {
			t.Fatal("Claim() claimed = false, want the deferred gen-2 row back")
		}
		if status, _, _ := readPreActivationWorkItem(t, ctx, db, "gen-2"); status != "succeeded" {
			t.Fatalf("status = %q, want succeeded via the superseded ack (terminal)", status)
		}
		assertPreActivationDrained(t, ctx, env, calls)
	})

	// A newer gen-3 activates first: the scope's active pointer moves past
	// gen-2, so the reducer claim's superseded_stale_reducer_generations CTE
	// retires the deferred gen-2 row (retrying is in its status list) before
	// any worker can run it.
	t.Run("newer_generation_activation_ends_the_wait_superseded", func(t *testing.T) {
		ctx := context.Background()
		db, env, calls := setupPreActivationDeferral(t, ctx, dsn)

		seedPreActivationGeneration(t, ctx, db, "gen-3", time.Now().UTC().Add(-5*time.Minute))
		projectorQueue := NewProjectorQueue(SQLDB{DB: db}, preActivationProofLeaseOwner, time.Minute)
		if err := projectorQueue.Ack(ctx, preActivationProjectorWork("gen-3"), runtime.Result{}); err != nil {
			t.Fatalf("ProjectorQueue.Ack(gen-3) error = %v", err)
		}

		env.advance(time.Minute)
		if claimed := env.claimExecuteSettle(t, ctx); claimed {
			t.Fatal("Claim() claimed = true, want the claim-time supersession to retire the gen-2 row")
		}
		if status, _, _ := readPreActivationWorkItem(t, ctx, db, "gen-2"); status != "superseded" {
			t.Fatalf("status = %q, want superseded once a newer generation is active", status)
		}
		assertPreActivationDrained(t, ctx, env, calls)
	})

	// The batch claim path defers identically: ClaimBatch renders the same
	// non-counting attempt-count CASE, and the service fails each batch item
	// through WorkSink.Fail. After activation the row runs exactly once.
	t.Run("batch_claim_defers_then_runs_once", func(t *testing.T) {
		ctx := context.Background()
		db, env, calls := setupPreActivationDeferral(t, ctx, dsn)

		for cycle := 0; cycle < 3; cycle++ {
			env.advance(time.Minute)
			if n := env.claimBatchExecuteSettle(t, ctx); n != 1 {
				t.Fatalf("defer cycle %d: ClaimBatch() claimed %d, want the deferred row back", cycle, n)
			}
			status, class, attempts := readPreActivationWorkItem(t, ctx, db, "gen-2")
			if status != "retrying" || class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != 1 || *calls != 0 {
				t.Fatalf("defer cycle %d: status=%q class=%q attempt_count=%d calls=%d, want retrying/%s/1/0",
					cycle, status, class, attempts, *calls, reducercontract.GenerationActivationNotReadyFailureClass)
			}
		}

		projectorQueue := NewProjectorQueue(SQLDB{DB: db}, preActivationProofLeaseOwner, time.Minute)
		if err := projectorQueue.Ack(ctx, preActivationProjectorWork("gen-2"), runtime.Result{}); err != nil {
			t.Fatalf("ProjectorQueue.Ack(gen-2) error = %v", err)
		}
		env.advance(time.Minute)
		if n := env.claimBatchExecuteSettle(t, ctx); n != 1 {
			t.Fatalf("post-activation ClaimBatch() claimed %d, want 1", n)
		}
		if status, _, _ := readPreActivationWorkItem(t, ctx, db, "gen-2"); status != "succeeded" || *calls != 1 {
			t.Fatalf("post-activation status = %q calls = %d, want succeeded/1", status, *calls)
		}
		env.advance(time.Minute)
		if n := env.claimBatchExecuteSettle(t, ctx); n != 0 || *calls != 1 {
			t.Fatalf("drained ClaimBatch() claimed %d calls = %d, want 0/1", n, *calls)
		}
	})
}

// setupPreActivationDeferral opens a fresh schema, seeds the standard
// pre-activation scope, enqueues one gen-2 reducer intent, and defers it once
// through Claim -> Runtime.Execute -> Fail, asserting the deferred row shape.
func setupPreActivationDeferral(t *testing.T, ctx context.Context, dsn string) (*sql.DB, preActivationProofEnv, *int) {
	t.Helper()
	db := openCrossScopeReadinessProofDB(t, ctx, dsn)
	seedPreActivationScope(t, ctx, db, time.Now().UTC().Add(-10*time.Minute).Truncate(time.Millisecond))

	calls := new(int)
	env := newPreActivationProofEnv(t, db, calls)
	if _, err := env.queue.Enqueue(ctx, []runtime.ReducerIntent{{
		ScopeID:      preActivationProofScope,
		GenerationID: "gen-2",
		Domain:       reducer.DomainWorkloadMaterialization,
		EntityKey:    "workload:6686",
		Reason:       "pre-activation exit proof",
		FactID:       "fact-6686",
		SourceSystem: "git",
	}}); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if claimed := env.claimExecuteSettle(t, ctx); !claimed {
		t.Fatal("pre-activation Claim() claimed = false, want the gen-2 row")
	}
	status, class, attempts := readPreActivationWorkItem(t, ctx, db, "gen-2")
	if status != "retrying" || class != reducercontract.GenerationActivationNotReadyFailureClass || attempts != 1 || *calls != 0 {
		t.Fatalf("deferral: status=%q class=%q attempt_count=%d calls=%d, want retrying/%s/1/0",
			status, class, attempts, *calls, reducercontract.GenerationActivationNotReadyFailureClass)
	}
	return db, env, calls
}

// claimBatchExecuteSettle claims through ClaimBatch and settles each item as
// the batch service does: an Execute error goes to per-item Fail, results go
// to AckBatch. It returns how many intents were claimed.
func (e preActivationProofEnv) claimBatchExecuteSettle(t *testing.T, ctx context.Context) int {
	t.Helper()
	intents, err := e.queue.ClaimBatch(ctx, 5)
	if err != nil {
		t.Fatalf("ClaimBatch() error = %v", err)
	}
	var (
		acked   []reducer.Intent
		results []reducer.Result
	)
	for _, intent := range intents {
		result, execErr := e.runtime.Execute(ctx, intent)
		if execErr != nil {
			if err := e.queue.Fail(ctx, intent, execErr); err != nil {
				t.Fatalf("Fail() error = %v", err)
			}
			continue
		}
		acked = append(acked, intent)
		results = append(results, result)
	}
	if len(acked) > 0 {
		if err := e.queue.AckBatch(ctx, acked, results); err != nil {
			t.Fatalf("AckBatch() error = %v", err)
		}
	}
	return len(intents)
}

// assertPreActivationDrained proves nothing is left to claim and the handler
// never ran.
func assertPreActivationDrained(t *testing.T, ctx context.Context, env preActivationProofEnv, calls *int) {
	t.Helper()
	env.advance(time.Minute)
	if claimed := env.claimExecuteSettle(t, ctx); claimed {
		t.Fatal("drained queue Claim() claimed = true, want nothing left")
	}
	if *calls != 0 {
		t.Fatalf("handler calls = %d, want 0: a generation that never activated must not run", *calls)
	}
}

// preActivationProjectorWork is the claimed projector work identity for a
// generation seeded with a running projector row owned by the proof lease.
func preActivationProjectorWork(generationID string) projector.ScopeGenerationWork {
	return projector.ScopeGenerationWork{
		Scope:        scope.IngestionScope{ScopeID: preActivationProofScope},
		Generation:   scope.ScopeGeneration{GenerationID: generationID, ScopeID: preActivationProofScope},
		AttemptCount: 1,
	}
}

// seedPreActivationGeneration adds one more pending generation, ingested at
// the given time, with its running projector row.
func seedPreActivationGeneration(t *testing.T, ctx context.Context, db *sql.DB, generationID string, ingestedAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
    generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload
) VALUES ($1, $2, 'snapshot', $3, $3, 'pending', NULL, '{}'::jsonb)`,
		generationID, preActivationProofScope, ingestedAt); err != nil {
		t.Fatalf("seed generation %s: %v", generationID, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_work_items (
    work_item_id, scope_id, generation_id, stage, domain, status,
    attempt_count, lease_owner, claim_until, visible_at, payload, created_at, updated_at
) VALUES ($1, $2, $3, 'projector', 'source_local', 'running',
          1, $4, $5, $6, '{}'::jsonb, $6, $6)`,
		"projector-"+generationID, preActivationProofScope, generationID,
		preActivationProofLeaseOwner, ingestedAt.Add(time.Hour), ingestedAt); err != nil {
		t.Fatalf("seed projector work %s: %v", generationID, err)
	}
}

func readPreActivationGenerationStatus(t *testing.T, ctx context.Context, db *sql.DB, generationID string) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(ctx,
		`SELECT status FROM scope_generations WHERE generation_id = $1`, generationID).Scan(&status); err != nil {
		t.Fatalf("read generation %s: %v", generationID, err)
	}
	return status
}
