// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package activation_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11104 overlap of the deferred-backfill phase publish wake with the activation obligation Finalize wake on the full bootstrap schema in an isolated disposable PostgreSQL schema: a backward-evidence row parked through the waiting set-aside is reopened by the publish (wait row deleted, attempt budget frozen), Finalize then completes with nothing left to wake, and the row is natively claimable exactly once
//eshu:readiness:env ESHU_DEFERRED_PARTITION_PROOF_DSN

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// TestActivationObligationPublishWakeOverlapsFinalizeLive pins the #11104
// overlap. The waiting set-aside parks the backward-evidence row with its
// exact-phase wait, the native maintenance publish reopens it by that key,
// and the obligation's Finalize, which wakes only rows still deferred into the
// future, finds nothing to wake and completes. Neither wake burns the attempt
// budget, and the row is claimable once, not twice.
func TestActivationObligationPublishWakeOverlapsFinalizeLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_publish_overlap", true)
	intent := f.enqueueClaim(t, reducer.DomainDeploymentMapping, f.scope, f.gen, "overlap")
	if err := f.reducerQ.Fail(f.ctx, intent,
		crossrepo.BackwardEvidenceNotReadyError{ScopeID: f.scope, GenerationID: f.gen}); err != nil {
		t.Fatal(err)
	}
	var waits int
	if err := f.db.QueryRowContext(f.ctx,
		`SELECT count(*) FROM reducer_waiting_items WHERE work_item_id = $1`, intent.IntentID).Scan(&waits); err != nil {
		t.Fatal(err)
	}
	if waits != 1 {
		t.Fatalf("parked wait rows = %d, want 1 (the set-aside must own the park)", waits)
	}

	f.maintenance(t)
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, true)

	var status string
	var attempts int
	var visibleNow bool
	if err := f.db.QueryRowContext(f.ctx, `
SELECT status, attempt_count, visible_at <= clock_timestamp() FROM fact_work_items WHERE work_item_id = $1`,
		intent.IntentID).Scan(&status, &attempts, &visibleNow); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRowContext(f.ctx,
		`SELECT count(*) FROM reducer_waiting_items WHERE work_item_id = $1`, intent.IntentID).Scan(&waits); err != nil {
		t.Fatal(err)
	}
	if status != "retrying" || !visibleNow || waits != 0 {
		t.Fatalf("after publish: status=%s visible_now=%v wait_rows=%d, want retrying, visible, 0 (the publish wakes it)",
			status, visibleNow, waits)
	}
	if attempts != intent.AttemptCount {
		t.Fatalf("after publish: attempt_count=%d, want %d (a wake never counts)", attempts, intent.AttemptCount)
	}

	obligation := f.claimObligation(t, "11104-overlap-owner", time.Minute, f.gen)
	result, err := f.oblig.Finalize(f.ctx, *obligation)
	if err != nil || result.Outcome != activation.OutcomeCompleted || result.Woken != 0 {
		t.Fatalf("finalize after publish wake = %+v err=%v, want completed with nothing left to wake", result, err)
	}
	f.mustClaimable(t, intent.IntentID)
	if again, ok, err := f.reducerQ.Claim(f.ctx); err != nil || ok {
		t.Fatalf("second claim = %s ok=%v err=%v, want nothing claimable (no double wake)", again.IntentID, ok, err)
	}
}
