// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"fmt"
	"testing"
	"time"

	projectorruntime "github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/activation"
)

// TestActivationObligationConsumerRestartAndDuplicatesLive: duplicate
// enqueue and Ack create nothing new; an expired owner, and the same owner
// name with a stale token, both lose; a completed obligation is neither
// reclaimed nor re-finalized; and an empty claim poll writes nothing.
func TestActivationObligationConsumerRestartAndDuplicatesLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_restart", true)
	wake := f.notReady(t, f.scope, f.gen, "wake")
	if n, err := f.reducerQ.Enqueue(f.ctx, []projectorruntime.ReducerIntent{{
		ScopeID: f.scope, GenerationID: f.gen, Domain: reducer.DomainDeploymentMapping,
		EntityKey: "wake", SourceSystem: "git",
	}}); err != nil || n.Count != 0 {
		t.Fatalf("duplicate enqueue count=%d err=%v", n.Count, err)
	}
	var obligations int
	if err := f.db.QueryRowContext(f.ctx,
		"SELECT count(*) FROM activation_obligations WHERE scope_id=$1 AND generation_id=$2",
		f.scope, f.gen).Scan(&obligations); err != nil || obligations != 1 {
		t.Fatalf("obligation count=%d err=%v", obligations, err)
	}
	stale := f.claimObligation(t, "restart-a", 400*time.Millisecond, f.gen)
	f.maintenance(t) // readiness commits before the owner "restarts".
	assertActivationBackwardPhase(t, f.ctx, f.db, f.target, true)
	f.awaitLeaseExpiry(t, f.gen)
	state := f.digest(t)
	if done, err := f.finalize(stale); err != nil || done {
		t.Fatalf("expired unreclaimed owner done=%v err=%v", done, err)
	}
	if f.digest(t) != state {
		t.Fatal("expired owner changed durable state")
	}
	fresh := f.claimObligation(t, "restart-a", time.Minute, f.gen)
	if fresh.LeaseToken <= stale.LeaseToken {
		t.Fatal("restart claim did not advance the token")
	}
	state = f.digest(t)
	if done, err := f.finalize(stale); err != nil || done {
		t.Fatalf("stale-token same owner done=%v err=%v", done, err)
	}
	if f.digest(t) != state {
		t.Fatal("stale token changed durable state")
	}
	if done, err := f.finalize(fresh); err != nil || !done {
		t.Fatalf("restarted owner done=%v err=%v", done, err)
	}
	f.mustClaimable(t, wake)
	state = f.digest(t)
	if done, err := f.finalize(fresh); err != nil || done {
		t.Fatalf("replayed completion done=%v err=%v", done, err)
	}
	if f.digest(t) != state {
		t.Fatal("replayed completion changed state")
	}
	for n := 0; n < 8; n++ {
		work, err := f.oblig.Claim(f.ctx, "drain", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if work == nil {
			break
		}
		if work.ScopeID == f.scope && work.GenerationID == f.gen {
			t.Fatal("completed obligation was claimed again")
		}
	}
	drained := f.digest(t)
	for n := 0; n < 3; n++ {
		if work, err := f.oblig.Claim(f.ctx, "drain", time.Minute); err != nil || work != nil {
			t.Fatalf("drained poll work=%v err=%v", work, err)
		}
	}
	if f.digest(t) != drained {
		t.Fatal("empty polling wrote durable state")
	}
}

// TestActivationObligationConsumerWakeBatchCapLive (N6): with more waiting
// rows than one wake batch, Finalize wakes exactly WakeBatchLimit rows,
// commits them, and keeps the obligation open; the next Finalize under the
// same lease wakes the rest and completes.
func TestActivationObligationConsumerWakeBatchCapLive(t *testing.T) {
	f := newActivationMatrix(t, "activation_wakecap", true)
	const waiting = activation.WakeBatchLimit + 3
	for i := 0; i < waiting; i++ {
		f.notReady(t, f.scope, f.gen, fmt.Sprintf("wake-%02d", i))
	}
	f.maintenance(t)
	obligation := f.claimObligation(t, "wakecap-owner", time.Minute, f.gen)
	countVisible := func() int {
		t.Helper()
		var visible int
		if err := f.db.QueryRowContext(f.ctx, `SELECT count(*) FROM fact_work_items
WHERE scope_id=$1 AND generation_id=$2 AND domain='deployment_mapping'
  AND status='retrying' AND visible_at <= clock_timestamp()`, f.scope, f.gen).Scan(&visible); err != nil {
			t.Fatal(err)
		}
		return visible
	}
	first, err := f.oblig.Finalize(f.ctx, *obligation)
	if err != nil || first.Outcome != activation.OutcomeWorkPending || first.Woken != activation.WakeBatchLimit {
		t.Fatalf("first finalize = %+v err=%v, want work_pending with %d woken",
			first, err, activation.WakeBatchLimit)
	}
	if got := countVisible(); got != activation.WakeBatchLimit {
		t.Fatalf("visible after first batch = %d, want %d", got, activation.WakeBatchLimit)
	}
	if state := f.obligationState(t, f.gen); state != "leased" {
		t.Fatalf("obligation state after partial wake = %q, want leased", state)
	}
	second, err := f.oblig.Finalize(f.ctx, *obligation)
	if err != nil || second.Outcome != activation.OutcomeCompleted || second.Woken != waiting-activation.WakeBatchLimit {
		t.Fatalf("second finalize = %+v err=%v, want completed with %d woken",
			second, err, waiting-activation.WakeBatchLimit)
	}
	if got := countVisible(); got != waiting {
		t.Fatalf("visible after second batch = %d, want %d", got, waiting)
	}
}

// TestActivationObligationCatchUpLive: catch-up owes exactly one obligation
// to an active repository generation that has neither an obligation nor its
// phase, skips generations that already have either and scopes without a
// repository fact, pages by scope count, and is idempotent.
func TestActivationObligationCatchUpLive(t *testing.T) {
	ctx, database := openActivationObligationProofDB(t, "activation_catchup")
	store := NewIngestionStore(SQLDB{DB: database})
	store.SkipRelationshipBackfill = true
	queue := NewProjectorQueue(SQLDB{DB: database}, "7584-catchup-projector", time.Minute)
	activate := func(scopeID, generationID, repoID string) {
		t.Helper()
		commitActivationRepository(t, ctx, store, activationRepositoryFact("fact-"+generationID,
			scopeID, generationID, repoID, "https://github.com/acme/"+repoID+".git"), repoID)
		work := claimActivationProjectorWork(t, ctx, queue, scopeID, generationID)
		if err := queue.Ack(ctx, work, projectorruntime.Result{}); err != nil {
			t.Fatal(err)
		}
	}
	activate("git:catchup-a", "gen-catchup-a", "repo-catchup-a")
	activate("git:catchup-b", "gen-catchup-b", "repo-catchup-b")
	activate("git:catchup-c", "gen-catchup-c", "repo-catchup-c")
	commitFluxSourceGeneration(t, ctx, store, "git:catchup-file-only", "repo-file-only", "gen-catchup-file-only")
	fileOnly := claimActivationProjectorWork(t, ctx, queue, "git:catchup-file-only", "gen-catchup-file-only")
	if err := queue.Ack(ctx, fileOnly, projectorruntime.Result{}); err != nil {
		t.Fatal(err)
	}
	// Simulate generations activated before the Ack insert existed: a and c
	// lose their obligation; b's generation already has its phase.
	if _, err := database.ExecContext(ctx, `DELETE FROM activation_obligations`); err != nil {
		t.Fatal(err)
	}
	maintenance := NewIngestionStore(SQLDB{DB: database})
	if err := maintenance.RunDeferredRelationshipMaintenance(ctx, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `DELETE FROM graph_projection_phase_state
WHERE scope_id IN ('git:catchup-a', 'git:catchup-c')`); err != nil {
		t.Fatal(err)
	}
	obligations := activation.NewStore(SQLDB{DB: database})
	var pages []activation.CatchUpPage
	cursor := ""
	for n := 0; n < 10; n++ {
		page, err := obligations.CatchUp(ctx, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		pages = append(pages, page)
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	// Five active scopes (the bootstrap seeds eshu:global, which has no
	// repository fact) in pages of two; the short third page ends the walk.
	if len(pages) != 3 || pages[0].Scanned != 2 || pages[1].Scanned != 2 || pages[2].Scanned != 1 ||
		pages[0].Inserted != 1 || pages[1].Inserted != 1 || pages[2].Inserted != 0 {
		t.Fatalf("catch-up pages = %+v, want 2+2+1 scopes inserting 1+1+0 obligations", pages)
	}
	rows, err := database.QueryContext(ctx, `SELECT scope_id, generation_id, work_item_id, state
FROM activation_obligations ORDER BY scope_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var got []string
	for rows.Next() {
		var scopeID, generationID, workItemID, state string
		if err := rows.Scan(&scopeID, &generationID, &workItemID, &state); err != nil {
			t.Fatal(err)
		}
		if workItemID != projectorWorkItemID(scopeID, generationID) || state != "pending" {
			t.Fatalf("catch-up row %s/%s work=%q state=%q", scopeID, generationID, workItemID, state)
		}
		got = append(got, scopeID+"/"+generationID)
	}
	if want := []string{"git:catchup-a/gen-catchup-a", "git:catchup-c/gen-catchup-c"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("catch-up obligations = %v, want %v", got, want)
	}
	again, err := obligations.CatchUp(ctx, "", 10)
	if err != nil || again.Inserted != 0 || again.Scanned != 5 || again.NextCursor != "" {
		t.Fatalf("repeated catch-up = %+v err=%v, want 5 scanned, 0 inserted, end cursor", again, err)
	}
}
