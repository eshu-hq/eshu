// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// TestActivationObligationClaimSkipsLockedRowsLive: Claim must never wait on
// an obligation row another transaction holds (for example a Finalize in
// flight). With the oldest obligation row-locked by an open transaction, a
// Claim under a short deadline returns the next obligation instead.
func TestActivationObligationClaimSkipsLockedRowsLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_skiplocked", true)
	holder, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	var heldScope, heldGeneration string
	if err := holder.QueryRowContext(f.ctx, `SELECT scope_id, generation_id FROM activation_obligations
WHERE state = 'pending' ORDER BY created_at, scope_id, generation_id LIMIT 1
FOR NO KEY UPDATE`).Scan(&heldScope, &heldGeneration); err != nil {
		t.Fatalf("hold the oldest obligation: %v", err)
	}
	claimCtx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	work, err := f.oblig.Claim(claimCtx, "skip-locked-owner", time.Minute)
	if err != nil || work == nil {
		t.Fatalf("Claim beside a held row = %+v err=%v, want the next obligation without waiting", work, err)
	}
	if work.ScopeID == heldScope && work.GenerationID == heldGeneration {
		t.Fatal("Claim returned the row another transaction holds")
	}
}

// TestActivationObligationClaimIgnoresFinishedRowsLive: a finished obligation
// older than every open one must neither be leased nor hide the open one.
func TestActivationObligationClaimIgnoresFinishedRowsLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_finished_claim")
	seedScope(t, ctx, database, "claim-finished")
	for _, row := range []struct{ generation, state, finished, created string }{
		{"claim-finished-completed", "completed", "clock_timestamp()", "clock_timestamp() - interval '2 hours'"},
		{"claim-finished-obsolete", "obsolete", "clock_timestamp()", "clock_timestamp() - interval '90 minutes'"},
		{"claim-finished-pending", "pending", "NULL", "clock_timestamp() - interval '1 hour'"},
	} {
		if _, err := database.ExecContext(ctx, `
INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, superseded_at)
VALUES ($1, 'claim-finished', 'snapshot', now(), now(), 'superseded', now())`, row.generation); err != nil {
			t.Fatal(err)
		}
		if _, err := database.ExecContext(ctx, `
INSERT INTO activation_obligations (scope_id, generation_id, work_item_id, state, finished_at, created_at)
VALUES ('claim-finished', $1, 'w', $2, `+row.finished+`, `+row.created+`)`, row.generation, row.state); err != nil {
			t.Fatal(err)
		}
	}
	store := activation.NewStore(postgres.SQLDB{DB: database})
	work, err := store.Claim(ctx, "finished-owner", time.Minute)
	if err != nil || work == nil || work.GenerationID != "claim-finished-pending" {
		t.Fatalf("Claim = %+v err=%v, want the pending obligation behind two older finished rows", work, err)
	}
	if again, err := store.Claim(ctx, "finished-owner", time.Minute); err != nil || again != nil {
		t.Fatalf("second Claim = %+v err=%v, want nothing claimable", again, err)
	}
}

// TestActivationObligationFinalizeLeaseExpiryRollsBackLive: when the lease
// expires inside Finalize after the wake ran, nothing commits. A
// statement-level pg_sleep trigger on fact_work_items (isolated schema only)
// stretches the wake past a short lease. With more waiting rows than one
// batch the partial-wake ownership recheck must refuse; with one waiting row
// the token-fenced completion must refuse.
func TestActivationObligationFinalizeLeaseExpiryRollsBackLive(t *testing.T) {
	for _, tc := range []struct {
		name    string
		waiting int
	}{
		{"partial_wake_recheck", activation.WakeBatchLimit + 1},
		{"completion_fence", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newActivationMatrix(t, "activation_expiry", true)
			for i := 0; i < tc.waiting; i++ {
				f.notReady(t, f.scope, f.gen, fmt.Sprintf("expiry-%02d", i))
			}
			f.maintenance(t)
			for _, ddl := range []string{
				`CREATE FUNCTION slow_activation_wake() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(1.2); RETURN NULL; END $$`,
				`CREATE TRIGGER slow_activation_wake BEFORE UPDATE ON fact_work_items FOR EACH STATEMENT EXECUTE FUNCTION slow_activation_wake()`,
			} {
				if _, err := f.db.ExecContext(f.ctx, ddl); err != nil {
					t.Fatal(err)
				}
			}
			obligation := f.claimObligation(t, "expiry-owner", 600*time.Millisecond, f.gen)
			result, err := f.oblig.Finalize(f.ctx, *obligation)
			if !errors.Is(err, activation.ErrLeaseLost) {
				t.Fatalf("finalize across lease expiry = %+v err=%v, want ErrLeaseLost", result, err)
			}
			if _, err := f.db.ExecContext(f.ctx, `DROP TRIGGER slow_activation_wake ON fact_work_items`); err != nil {
				t.Fatal(err)
			}
			// Only the claim (lease columns and token) may differ from before;
			// no wake and no completion survived.
			var visible int
			if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM fact_work_items
WHERE scope_id=$1 AND generation_id=$2 AND domain='deployment_mapping'
  AND status='retrying' AND visible_at <= clock_timestamp()`, f.scope, f.gen).Scan(&visible); err != nil {
				t.Fatal(err)
			}
			if visible != 0 {
				t.Fatalf("%d woken rows survived a lease lost mid-finalize", visible)
			}
			if got := f.obligationState(t, f.gen); got != "leased" {
				t.Fatalf("obligation state after lost lease = %q, want leased (unchanged)", got)
			}
		})
	}
}
